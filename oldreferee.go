package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reasons an old-mode game is invalid (besides start_failed, exit_status_<n>
// and timeout).
const (
	// ReasonProtocol: the referee sent a malformed or unknown ### command, a
	// player index out of range, or an ###End ranking that doesn't name every
	// player exactly once.
	ReasonProtocol = "protocol"
	// ReasonNoEnd: the referee's stdout ended without an ###End line.
	ReasonNoEnd = "no_end"
)

// OldReferee plays one game with an "old" CodinGame referee (games created
// before Botters of the Galaxy / Ultimate Tic Tac Toe, run by cg-brutaltester
// with -o). Unlike a new referee, it doesn't start the bots: cgarena starts
// them and relays lines between them and the referee, which drives the game
// over its stdin/stdout:
//
//	cgarena → referee: "###Seed <seed>", then "###Start <players>"
//	referee → cgarena: "###Input X" (the following non-### lines go to player
//	X's stdin), "###Output X N" (read N lines from player X's stdout and send
//	them to the referee), "###Error X ..." (logged), and finally
//	"###End <ranking>" (player indices best first, tied players written
//	together, e.g. "###End 1 03 2").
//
// Player indices count from 0 and are seat positions. A player whose stdout
// ends before it sent the lines asked for, whose stdin is closed while it is
// sent input, or that can't be started, has crashed: the game stops and that
// player loses (see crashScores).
type OldReferee struct {
	Command   string
	Seats     []Seat // in seat order: Seats[0] is player 0
	Seed      int    // sent as ###Seed; 0 or less sends none
	LogOutput string
	// Timeout is how long a game may run before the referee and the bots
	// are killed and the game is invalid (ReasonTimeout); 0 means no limit.
	Timeout time.Duration
}

// Run plays one game; see RunContext.
func (r *OldReferee) Run(matchID int) (*RefereeResult, error) {
	return r.RunContext(context.Background(), matchID)
}

// oldGame holds the processes and pipes of one old-mode game.
type oldGame struct {
	// referee and bots (by seat; nil for a bot that failed to start) are
	// only used by the goroutine running RunContext.
	referee *oldProc
	bots    []*oldProc
	// transcript is the game's traffic log (nil with logging off). Only the
	// goroutine running RunContext writes it.
	transcript *bufio.Writer

	// mu guards all (every started process, for killAll), killed and each
	// proc's waited.
	mu     sync.Mutex
	all    []*oldProc
	killed bool
}

// oldProc is one started process (referee or bot).
type oldProc struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	w      *bufio.Writer
	out    *bufio.Reader
	waited bool
}

// RunContext plays one game. It always returns a non-nil result. When the
// game is invalid, the result's InvalidReason is set, every outcome is
// "invalid", and the error is an *InvalidGameError. When parent is cancelled
// before the game finishes, every process is killed, the result's
// Interrupted is set and the error is ErrInterrupted.
func (r *OldReferee) RunContext(parent context.Context, matchID int) (*RefereeResult, error) {
	sentSeed := max(r.Seed, 0)
	logging := isLogging(r.LogOutput)
	logFile := ""
	if logging {
		logFile = filepath.Join(r.LogOutput, "match_"+strconv.Itoa(matchID)+".log")
	}
	results := newRefereeResult(r.Seats, sentSeed, logFile)
	if parent.Err() != nil {
		results.Interrupted = true
		return results, ErrInterrupted
	}

	ctx := parent
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	g := &oldGame{}
	var stderr io.Writer = io.Discard
	if logging {
		logF, err := os.Create(logFile)
		if err != nil {
			return results.invalid(ReasonStartFailed, "creating game log: "+err.Error())
		}
		defer logF.Close()
		g.transcript = bufio.NewWriter(logF)
		defer g.transcript.Flush()
		errF, err := os.Create(filepath.Join(r.LogOutput, "match_"+strconv.Itoa(matchID)+".err"))
		if err != nil {
			return results.invalid(ReasonStartFailed, "creating stderr file: "+err.Error())
		}
		defer errF.Close()
		stderr = errF
	}
	var stderrMu sync.Mutex
	var stderrWriters []*lineWriter
	newStderr := func(prefix string) io.Writer {
		lw := &lineWriter{mu: &stderrMu, w: stderr, prefix: prefix}
		stderrWriters = append(stderrWriters, lw)
		return lw
	}
	defer func() {
		for _, lw := range stderrWriters {
			lw.Flush()
		}
	}()

	start := time.Now()
	defer func() { results.Duration = time.Since(start) }()

	// Kill everything when the game's context ends (timeout or interrupt),
	// and in any case once the game is over.
	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			g.killAll()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		<-watcherDone
		g.finish()
	}()

	// interrupted checks for a cancelled run or an expired timeout (a
	// failure after one is caused by the kill). It returns a nil result if
	// neither happened.
	interrupted := func() (*RefereeResult, error) {
		if parent.Err() != nil {
			results.Interrupted = true
			results.Killed = true
			return results, ErrInterrupted
		}
		if ctx.Err() != nil {
			return results.invalid(ReasonTimeout, "referee and bots killed after "+r.Timeout.String())
		}
		return nil, nil
	}

	ref, err := g.start(r.Command, newStderr("Referee: "))
	if err != nil {
		if res, err := interrupted(); res != nil {
			return res, err
		}
		return results.invalid(ReasonStartFailed, err.Error())
	}
	g.referee = ref

	crashed := make([]bool, len(r.Seats))
	anyCrash := false
	for i, seat := range r.Seats {
		bot, err := g.start(seat.Cmd, newStderr(fmt.Sprintf("Player %d: ", i)))
		if err != nil {
			g.log("Player %d failed to start: %v", i, err)
			crashed[i] = true
			anyCrash = true
			results.EndReason = fmt.Sprintf("player %d crashed (failed to start: %v)", i, err)
		}
		g.bots = append(g.bots, bot)
	}

	crash := func(seat int, why string) (*RefereeResult, error) {
		if res, err := interrupted(); res != nil {
			return res, err
		}
		g.log("Player %d crashed: %s", seat, why)
		crashed[seat] = true
		results.EndReason = fmt.Sprintf("player %d crashed (%s)", seat, why)
		return results.crashScores(crashed), nil
	}
	if anyCrash {
		if res, err := interrupted(); res != nil {
			return res, err
		}
		return results.crashScores(crashed), nil
	}

	if sentSeed > 0 {
		g.toReferee("###Seed " + strconv.Itoa(sentSeed))
	}
	g.toReferee("###Start " + strconv.Itoa(len(r.Seats)))
	g.referee.w.Flush()

	input := -1 // player receiving the referee's lines (###Input), or -1
	for {
		line, err := readLine(g.referee.out)
		if err != nil {
			if res, err := interrupted(); res != nil {
				return res, err
			}
			werr := g.wait(g.referee)
			if res, err := interrupted(); res != nil {
				return res, err
			}
			var exitErr *exec.ExitError
			if errors.As(werr, &exitErr) && exitErr.ExitCode() != 0 {
				return results.invalid(ReasonExitStatus+strconv.Itoa(exitErr.ExitCode()),
					"referee exited without ###End: "+werr.Error())
			}
			return results.invalid(ReasonNoEnd, "referee's output ended without ###End")
		}
		g.log("Referee: %s", line)

		if !strings.HasPrefix(line, "###") {
			if input < 0 {
				continue // not inside an ###Input block: ignored (logged above)
			}
			if _, err := g.bots[input].w.WriteString(line + "\n"); err != nil {
				return crash(input, "stdin closed: "+err.Error())
			}
			continue
		}
		if input >= 0 {
			if err := g.bots[input].w.Flush(); err != nil {
				return crash(input, "stdin closed: "+err.Error())
			}
			input = -1
		}

		fields := strings.Fields(line)
		switch fields[0] {
		case "###Input":
			if len(fields) != 2 {
				return results.invalid(ReasonProtocol, fmt.Sprintf("malformed %q", line))
			}
			x, ok := r.seatIndex(fields[1])
			if !ok {
				return results.invalid(ReasonProtocol, fmt.Sprintf("bad player index in %q", line))
			}
			input = x
		case "###Output":
			if len(fields) != 3 {
				return results.invalid(ReasonProtocol, fmt.Sprintf("malformed %q", line))
			}
			x, ok := r.seatIndex(fields[1])
			n, err := strconv.Atoi(fields[2])
			if !ok || err != nil || n < 0 {
				return results.invalid(ReasonProtocol, fmt.Sprintf("bad player index or line count in %q", line))
			}
			for i := 0; i < n; i++ {
				out, err := readLine(g.bots[x].out)
				if err != nil {
					return crash(x, fmt.Sprintf("stdout closed after %d of %d lines asked by ###Output", i, n))
				}
				g.log("Player %d: %s", x, out)
				g.toReferee(out)
			}
			g.referee.w.Flush() // a write error shows up as the referee's stdout ending
		case "###Error":
			// Logged above; the referee decides what it means for the game.
		case "###End":
			scores, err := rankingScores(fields[1:], len(r.Seats))
			if err != nil {
				return results.invalid(ReasonProtocol, fmt.Sprintf("%q: %v", line, err))
			}
			results.Scores = scores
			results.Ranking = strings.Join(fields[1:], " ")
			results.ProcessResults()
			return results, nil
		default:
			return results.invalid(ReasonProtocol, fmt.Sprintf("unknown command %q", line))
		}
	}
}

// seatIndex parses a player index sent by the referee.
func (r *OldReferee) seatIndex(s string) (int, bool) {
	x, err := strconv.Atoi(s)
	return x, err == nil && x >= 0 && x < len(r.Seats)
}

// rankingScores turns an ###End ranking (groups of player indices, best
// first, tied players written together as in "03") into per-seat scores:
// the first group gets len(groups), the next one less, and so on, so that
// ProcessResults gives first place the win (or a draw when shared).
func rankingScores(groups []string, players int) ([]int, error) {
	if len(groups) == 0 {
		return nil, errors.New("empty ranking")
	}
	scores := make([]int, players)
	seen := make([]bool, players)
	for pos, group := range groups {
		for _, c := range group {
			x := int(c - '0')
			if c < '0' || c > '9' || x >= players {
				return nil, fmt.Errorf("bad player index %q", c)
			}
			if seen[x] {
				return nil, fmt.Errorf("player %d ranked twice", x)
			}
			seen[x] = true
			scores[x] = len(groups) - pos
		}
	}
	for x, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("player %d not ranked", x)
		}
	}
	return scores, nil
}

// crashScores ends the game after a crash: crashed players lose; if one
// player is left it wins, if several are left they draw, and if none is left
// the game is a double forfeit. Crashed players are marked as faulted.
func (rr *RefereeResult) crashScores(crashed []bool) *RefereeResult {
	rr.Scores = make([]int, len(crashed))
	for i, c := range crashed {
		if c {
			rr.Scores[i] = -1
			rr.Faults[i] = true
		}
	}
	rr.ProcessResults()
	return rr
}

// newRefereeResult returns the starting result of a game.
func newRefereeResult(seats []Seat, seed int, logFile string) *RefereeResult {
	seats = append([]Seat(nil), seats...)
	players := make([]Player, len(seats))
	for i, seat := range seats {
		players[i] = Player{Name: seat.Name, Cmd: seat.Cmd}
	}
	return &RefereeResult{
		Seats:   seats,
		Players: players,
		Seed:    seed,
		LogFile: logFile,
		Faults:  make([]bool, len(seats)),
	}
}

// start starts command (split on whitespace) in its own process group with
// piped stdin and stdout. It returns a nil proc when the game was already
// killed.
func (g *oldGame) start(command string, stderr io.Writer) (*oldProc, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("empty command")
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	setProcessGroup(cmd)
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.killed {
		return nil, errors.New("game killed before start")
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &oldProc{cmd: cmd, in: in, w: bufio.NewWriter(in), out: bufio.NewReader(out)}
	g.all = append(g.all, p)
	return p, nil
}

// wait waits for p to exit and returns cmd.Wait's error. After it, killAll
// leaves p alone (its process group may be gone, its pid reused).
func (g *oldGame) wait(p *oldProc) error {
	err := p.cmd.Wait()
	g.mu.Lock()
	p.waited = true
	g.mu.Unlock()
	return err
}

// killAll kills every started process's group. Reads from and writes to
// their pipes then fail, which ends the game loop.
func (g *oldGame) killAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.killed = true
	for _, p := range g.all {
		if !p.waited {
			_ = killProcessGroup(p.cmd)
		}
	}
}

// finish ends the game: it closes every stdin, gives the processes waitDelay
// to exit on their own, then kills what is left and waits for everything.
func (g *oldGame) finish() {
	g.mu.Lock()
	ps := append([]*oldProc(nil), g.all...)
	g.mu.Unlock()
	for _, p := range ps {
		_ = p.in.Close()
	}
	timer := time.AfterFunc(waitDelay, g.killAll)
	defer timer.Stop()
	for _, p := range ps {
		g.mu.Lock()
		waited := p.waited
		g.mu.Unlock()
		if !waited {
			_ = g.wait(p)
		}
	}
}

// toReferee queues one line for the referee's stdin. Write errors are
// ignored: a referee that stopped reading is seen when its stdout ends.
func (g *oldGame) toReferee(line string) {
	g.log("To referee: %s", line)
	_, _ = g.referee.w.WriteString(line + "\n")
}

// log writes one line to the game's transcript, if logging is on.
func (g *oldGame) log(format string, args ...any) {
	if g.transcript != nil {
		fmt.Fprintf(g.transcript, format+"\n", args...)
	}
}

// readLine reads one line without its line ending ("\n" or "\r\n"). A last
// line without a newline is returned; io.EOF comes only with nothing left.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// lineWriter writes whole lines, each prefixed, to w under mu, so several
// processes' stderr can share one file without mixing within a line.
type lineWriter struct {
	mu     *sync.Mutex
	w      io.Writer
	prefix string
	buf    []byte
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.buf = append(lw.buf, p...)
	for {
		i := strings.IndexByte(string(lw.buf), '\n')
		if i < 0 {
			break
		}
		lw.w.Write([]byte(lw.prefix + string(lw.buf[:i+1])))
		lw.buf = lw.buf[i+1:]
	}
	return len(p), nil
}

// Flush writes a last unterminated line, if any.
func (lw *lineWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if len(lw.buf) > 0 {
		lw.w.Write([]byte(lw.prefix + string(lw.buf) + "\n"))
		lw.buf = nil
	}
}
