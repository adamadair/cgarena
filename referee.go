package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Seat is one player's place in a game: the player's index in Arena.Players
// (which results are attributed to), plus the display name and command.
// Workers get their own copy of the seat order, so they never touch the
// shared Players slice.
type Seat struct {
	Index int
	Name  string
	Cmd   string
}

// seatsOf returns the seats for players in their current order.
func seatsOf(players []Player) []Seat {
	seats := make([]Seat, len(players))
	for i, p := range players {
		seats[i] = Seat{Index: i, Name: p.Name, Cmd: p.Cmd}
	}
	return seats
}

type Referee struct {
	Command   string
	Seats     []Seat // in seat order: Seats[0] is -p1
	Seed      int    // seed sent as -seed for this game; 0 or less sends none
	LogOutput string
	// Timeout is how long a game may run before the referee's process group
	// is killed and the game is invalid (ReasonTimeout); 0 means no limit.
	Timeout time.Duration
	// JSONCheck, with logging on, makes the game invalid (ReasonJSONLog) when
	// the referee's JSON log is missing or unparseable, or its scores differ
	// from stdout. Without it a bad JSON log only loses EndReason and faults.
	JSONCheck bool
	// FaultPattern, if set, marks a seat as faulted when a line of a JSON log
	// tooltip or summary matches it and names the seat as $<seat>.
	FaultPattern *regexp.Regexp
}

// waitDelay is how long Run waits, after the referee was killed or exited,
// for its output pipes to close (they can be held open by processes the
// referee started) before closing them itself.
const waitDelay = time.Second

// Per-player outcomes of a game.
const (
	OutcomeWin           = "win"
	OutcomeLoss          = "loss"
	OutcomeDraw          = "draw"
	OutcomeDoubleForfeit = "double_forfeit"
	OutcomeInvalid       = "invalid"
)

// Reasons a game is invalid.
const (
	ReasonStartFailed  = "start_failed"
	ReasonExitStatus   = "exit_status_" // followed by the exit code
	ReasonTimeout      = "timeout"
	ReasonScoreCount   = "score_count"
	ReasonSeedMismatch = "seed_mismatch"
	ReasonJSONLog      = "json_log"
)

type RefereeResult struct {
	// Seats is the seat order the game was played in.
	Seats []Seat
	// Players holds this game's W/L/T per seat, in the same order as Seats.
	Players []Player
	Scores  []int
	Seed    int // seed sent to the referee, else the one it reported (0 if neither)
	// Outcomes holds one outcome per seat, in the same order as Seats.
	Outcomes []string
	// InvalidReason is non-empty when the game is invalid.
	InvalidReason string
	// Interrupted is set when the run was interrupted before the game
	// finished; the game has no outcome. Killed is also set if the referee
	// had been started (and so was killed).
	Interrupted bool
	Killed      bool
	// LogFile is the referee's JSON log path ("" with logging off).
	LogFile string
	// EndReason is the text of the last tooltip in the JSON log ("" if
	// there is none or the log couldn't be read).
	EndReason string
	// Faults holds, per seat, whether the seat matched FaultPattern in the
	// JSON log. It is all false for invalid games.
	Faults []bool
	// Duration is how long the referee ran.
	Duration time.Duration
}

// InvalidGameError reports why a game is invalid.
type InvalidGameError struct {
	Reason string
	Detail string
}

func (e *InvalidGameError) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// ErrInterrupted is returned by RunContext when the run's context was
// cancelled (cgarena was interrupted) before the game finished. Such a game
// is neither a result nor invalid.
var ErrInterrupted = errors.New("interrupted")

// Run plays one game; see RunContext.
func (r *Referee) Run(matchId int) (*RefereeResult, error) {
	return r.RunContext(context.Background(), matchId)
}

// RunContext plays one game. It always returns a non-nil result. When the
// game is invalid, the result's InvalidReason is set, every outcome is
// "invalid", and the error is an *InvalidGameError. When parent is cancelled
// before the game finishes, the referee's process group is killed (as for a
// timeout), the result's Interrupted is set and the error is ErrInterrupted.
func (r *Referee) RunContext(parent context.Context, matchId int) (*RefereeResult, error) {
	commandSplit := strings.Split(r.Command, " ")
	runCommand := commandSplit[0]
	args := append([]string{}, commandSplit[1:]...)

	for i, seat := range r.Seats {
		args = append(args, "-p"+strconv.Itoa(i+1), seat.Cmd)
	}

	sentSeed := 0
	if r.Seed > 0 {
		sentSeed = r.Seed
		args = append(args, "-seed", strconv.Itoa(sentSeed))
	}

	logging := r.LogOutput != "" && r.LogOutput != "none" && r.LogOutput != "off"
	logFile := ""
	if logging {
		logFile = filepath.Join(r.LogOutput, "match_"+strconv.Itoa(matchId)+".json")
		args = append(args, "-l", logFile)
	}

	seats := append([]Seat(nil), r.Seats...)
	resultPlayers := make([]Player, len(seats))
	for i, seat := range seats {
		resultPlayers[i] = Player{Name: seat.Name, Cmd: seat.Cmd}
	}
	results := &RefereeResult{
		Seats:   seats,
		Players: resultPlayers,
		Seed:    sentSeed,
		LogFile: logFile,
		Faults:  make([]bool, len(seats)),
	}
	if parent.Err() != nil {
		// Interrupted before the game started: don't create its files.
		results.Interrupted = true
		return results, ErrInterrupted
	}

	ctx := parent
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, runCommand, args...)
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = waitDelay
	var out strings.Builder
	cmd.Stdout = &out
	if logging {
		errFile, err := os.Create(filepath.Join(r.LogOutput, "match_"+strconv.Itoa(matchId)+".err"))
		if err != nil {
			return results.invalid(ReasonStartFailed, "creating stderr file: "+err.Error())
		}
		defer errFile.Close()
		cmd.Stderr = errFile
	}
	start := time.Now()
	err := cmd.Run()
	results.Duration = time.Since(start)
	if err != nil && parent.Err() != nil {
		// Checked before the timeout: a kill caused by the interrupt is
		// not a timeout or an exit status.
		results.Interrupted = true
		results.Killed = cmd.Process != nil
		return results, ErrInterrupted
	}
	if err != nil && ctx.Err() != nil {
		return results.invalid(ReasonTimeout, "referee killed after "+r.Timeout.String())
	}
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		// The referee exited cleanly, but a process it started kept stdout
		// open past waitDelay; use the output read so far.
		err = nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return results.invalid(ReasonExitStatus+strconv.Itoa(exitErr.ExitCode()), err.Error())
		}
		return results.invalid(ReasonStartFailed, err.Error())
	}

	resultLines := strings.Split(out.String(), "\n")
	results.Scores = getScores(resultLines)
	if len(results.Scores) != len(seats) {
		return results.invalid(ReasonScoreCount,
			fmt.Sprintf("got %d score lines for %d players", len(results.Scores), len(seats)))
	}
	if gotSeed, ok := getSeed(resultLines); ok {
		if sentSeed > 0 && gotSeed != sentSeed {
			return results.invalid(ReasonSeedMismatch,
				fmt.Sprintf("sent seed %d, referee reported seed=%d", sentSeed, gotSeed))
		}
		results.Seed = gotSeed
	}
	if logging {
		gameLog, err := readGameLog(logFile)
		if err == nil {
			results.EndReason = gameLog.endReason()
		}
		if r.JSONCheck {
			if err == nil {
				err = gameLog.checkScores(results.Scores)
			}
			if err != nil {
				return results.invalid(ReasonJSONLog, err.Error())
			}
		}
		if err == nil && r.FaultPattern != nil {
			for j := range seats {
				results.Faults[j] = gameLog.faulted(r.FaultPattern, j)
			}
		}
	}
	results.ProcessResults()
	return results, nil
}

// gameLog holds the parts of a CodinGame SDK JSON game log that cgarena reads.
type gameLog struct {
	// Scores maps seat index ("0", "1", ...) to score.
	Scores   map[string]json.RawMessage `json:"scores"`
	Tooltips []struct {
		Text string `json:"text"`
	} `json:"tooltips"`
	// Summaries has one entry per frame; entries that aren't strings (e.g.
	// null) are ignored.
	Summaries []json.RawMessage `json:"summaries"`
}

// readGameLog reads and parses the JSON log at path.
func readGameLog(path string) (*gameLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading JSON log: %w", err)
	}
	var gl gameLog
	if err := json.Unmarshal(data, &gl); err != nil {
		return nil, fmt.Errorf("parsing JSON log %s: %w", path, err)
	}
	return &gl, nil
}

// endReason returns the text of the last tooltip, or "" if there is none.
func (gl *gameLog) endReason() string {
	if len(gl.Tooltips) == 0 {
		return ""
	}
	return gl.Tooltips[len(gl.Tooltips)-1].Text
}

// checkScores returns an error unless the log has exactly one score per seat
// and each equals the stdout score of that seat.
func (gl *gameLog) checkScores(stdout []int) error {
	mismatch := func(detail string) error {
		return fmt.Errorf("JSON log scores %s don't match stdout scores %v: %s", gl.scoresString(), stdout, detail)
	}
	if gl.Scores == nil {
		return fmt.Errorf("JSON log has no scores")
	}
	if len(gl.Scores) != len(stdout) {
		return mismatch(fmt.Sprintf("%d JSON scores for %d players", len(gl.Scores), len(stdout)))
	}
	for j, want := range stdout {
		raw, ok := gl.Scores[strconv.Itoa(j)]
		if !ok {
			return mismatch(fmt.Sprintf("no JSON score for seat %d", j))
		}
		got, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || got != float64(want) {
			return mismatch(fmt.Sprintf("seat %d", j))
		}
	}
	return nil
}

// scoresString formats Scores compactly for error messages.
func (gl *gameLog) scoresString() string {
	data, _ := json.Marshal(gl.Scores)
	return string(data)
}

// texts returns every line of the tooltips and string summaries.
func (gl *gameLog) texts() []string {
	var lines []string
	for _, tt := range gl.Tooltips {
		lines = append(lines, strings.Split(tt.Text, "\n")...)
	}
	for _, raw := range gl.Summaries {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			lines = append(lines, strings.Split(s, "\n")...)
		}
	}
	return lines
}

// faulted reports whether a tooltip or summary line matches pattern and
// names seat as $<seat>. Lines are checked one by one because a frame's
// summary can hold lines about several players.
func (gl *gameLog) faulted(pattern *regexp.Regexp, seat int) bool {
	for _, line := range gl.texts() {
		if namesSeat(line, seat) && pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// namesSeat reports whether line contains the token $<seat> (not followed by
// another digit, so $1 doesn't match $12).
func namesSeat(line string, seat int) bool {
	token := "$" + strconv.Itoa(seat)
	for {
		i := strings.Index(line, token)
		if i < 0 {
			return false
		}
		line = line[i+len(token):]
		if line == "" || line[0] < '0' || line[0] > '9' {
			return true
		}
	}
}

// invalid marks the result invalid and returns it with an *InvalidGameError.
func (rr *RefereeResult) invalid(reason, detail string) (*RefereeResult, error) {
	rr.InvalidReason = reason
	rr.Outcomes = make([]string, len(rr.Players))
	for i := range rr.Outcomes {
		rr.Outcomes[i] = OutcomeInvalid
	}
	return rr, &InvalidGameError{Reason: reason, Detail: detail}
}

// IsDoubleForfeit reports whether the game was valid but every score was negative.
func (rr *RefereeResult) IsDoubleForfeit() bool {
	return rr.InvalidReason == "" && len(rr.Outcomes) > 0 && rr.Outcomes[0] == OutcomeDoubleForfeit
}

func getScores(lines []string) []int {
	var scores []int
	for _, line := range lines {
		i, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil {
			scores = append(scores, i)
		}
	}
	return scores
}

// getSeed returns the value of the first parseable "seed=" line, and whether
// there was one.
func getSeed(lines []string) (int, bool) {
	for _, line := range lines {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "seed=")
		if !found {
			continue
		}
		if seed, err := strconv.Atoi(value); err == nil {
			return seed, true
		}
	}
	return 0, false
}

// ProcessResults sets each seat's outcome and W/L/T counters from Scores,
// which must hold one score per player:
//   - every score negative: double forfeit for everyone (no W/L/T);
//   - a unique highest score: win for that seat, loss for the rest;
//   - otherwise the seats tied for the highest score draw and the rest lose.
func (rr *RefereeResult) ProcessResults() {
	n := len(rr.Players)
	rr.Outcomes = make([]string, n)
	if len(rr.Scores) != n || n == 0 {
		for i := range rr.Outcomes {
			rr.Outcomes[i] = OutcomeInvalid
		}
		if rr.InvalidReason == "" {
			rr.InvalidReason = ReasonScoreCount
		}
		return
	}
	maxScore := maxValue(rr.Scores)
	if maxScore < 0 {
		for i := range rr.Outcomes {
			rr.Outcomes[i] = OutcomeDoubleForfeit
		}
		return
	}
	top := 0
	for _, score := range rr.Scores {
		if score == maxScore {
			top++
		}
	}
	for i, score := range rr.Scores {
		switch {
		case score != maxScore:
			rr.Outcomes[i] = OutcomeLoss
			rr.Players[i].Losses++
		case top == 1:
			rr.Outcomes[i] = OutcomeWin
			rr.Players[i].Wins++
		default:
			rr.Outcomes[i] = OutcomeDraw
			rr.Players[i].Ties++
		}
	}
}

func maxValue(arr []int) int {
	if len(arr) == 0 {
		return 0
	}
	mx := arr[0]
	for _, v := range arr[1:] {
		if v > mx {
			mx = v
		}
	}
	return mx
}

func (rr *RefereeResult) String() string {
	var sb strings.Builder
	sb.WriteString("Referee Result:\n")
	for i, player := range rr.Players {
		score := "-"
		if i < len(rr.Scores) {
			score = strconv.Itoa(rr.Scores[i])
		}
		sb.WriteString(player.Name + ": " + score + "; ")
	}
	sb.WriteString("Seed: " + strconv.Itoa(rr.Seed))
	if rr.InvalidReason != "" {
		sb.WriteString("; invalid: " + rr.InvalidReason)
	} else if rr.IsDoubleForfeit() {
		sb.WriteString("; double forfeit")
	}
	return sb.String()
}
