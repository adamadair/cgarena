package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	VERSION = "1.0.2"
)

// Exit codes.
const (
	exitOK      = 0   // all games valid (double forfeits allowed)
	exitUsage   = 1   // usage or config error
	exitInvalid = 3   // run completed, but a game was invalid or no game produced a result
	exitSignal  = 130 // interrupted by SIGINT/SIGTERM; results are incomplete
)

// Hooks for the interrupt handling in runArena, replaced by tests.
var (
	// notifySignals registers c for the signals that interrupt a run.
	notifySignals = func(c chan<- os.Signal) { signal.Notify(c, os.Interrupt, syscall.SIGTERM) }
	// stopSignals undoes notifySignals.
	stopSignals = func(c chan<- os.Signal) { signal.Stop(c) }
	// exitNow exits the process at once (a second signal during cleanup).
	exitNow = os.Exit
)

// runArena plays the arena's games, interrupting the run on SIGINT or SIGTERM:
// the first signal cancels the run (no new games, running referees are
// killed, see Arena.RunContext); a second one before the run returns calls
// exitNow(exitSignal) without waiting for the cleanup.
func runArena(arena *Arena) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 2)
	notifySignals(sigs)
	defer stopSignals(sigs)
	runDone := make(chan struct{})
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		select {
		case sig := <-sigs:
			log.Printf("Received %v: killing running referees (send it again to exit at once)", sig)
			cancel()
		case <-runDone:
			return
		}
		// Prefer a second signal that is already pending over runDone.
		select {
		case <-sigs:
		default:
			select {
			case <-sigs:
			case <-runDone:
				return
			}
		}
		log.Printf("Received a second signal: exiting without waiting for cleanup")
		exitNow(exitSignal)
	}()
	err := arena.RunContext(ctx)
	close(runDone)
	<-handlerDone
	return err
}

type Player struct {
	Name   string `json:"name"`
	Cmd    string `json:"cmd"`
	Wins   int
	Losses int
	Ties   int
	// Played counts the valid games (double forfeits included) the player
	// played, and Faults those in which it matched the fault pattern.
	Played int `json:"-"`
	Faults int `json:"-"`
}

func (p *Player) WinPct() float64 {
	total := p.Wins + p.Losses + p.Ties
	if total == 0 {
		return 0.0
	}
	return (float64(p.Wins*2) + float64(p.Ties)) / float64(total*2)
}

func loadConfig(path string) (*Arena, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Arena
	err = json.Unmarshal(data, &cfg)
	return &cfg, err
}

func buildRefereeCmd(cfg *Arena, matchID int) *exec.Cmd {
	args := []string{}
	for i, player := range cfg.Players {
		args = append(args, fmt.Sprintf("-p%d", i+1), player.Cmd)
	}
	args = append(args, "-d", fmt.Sprintf("seed=%d", cfg.Seed+matchID))
	logFile := filepath.Join(cfg.LogOutput, fmt.Sprintf("match_%03d.json", matchID))
	args = append(args, "-l", logFile)

	return exec.Command("sh", "-c", fmt.Sprintf("%s %s", cfg.Referee, shellJoin(args)))
}

func shellJoin(args []string) string {
	joined := ""
	for _, a := range args {
		joined += fmt.Sprintf("'%s' ", a)
	}
	return joined
}

func writeMarkdown(players []Player, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, _ = fmt.Fprintln(f, "# Match Summary")
	_, _ = fmt.Fprintln(f, "| Bot | Wins | Losses | Ties |")
	_, _ = fmt.Fprintln(f, "|-----|------|--------|------|")
	for _, r := range players {
		_, _ = fmt.Fprintf(f, "| %s | %d | %d | %d |\n", r.Name, r.Wins, r.Losses, r.Ties)
	}
	return nil
}

func writeHTML(players []Player, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, "<!DOCTYPE html>")
	_, _ = fmt.Fprintln(f, "<html>")
	_, _ = fmt.Fprintln(f, "<head><title>Match Summary</title></head>")
	_, _ = fmt.Fprintln(f, "<body>")
	_, _ = fmt.Fprintln(f, "<h1>Match Summary</h1>")
	_, _ = fmt.Fprintln(f, "<table border='1'>")
	_, _ = fmt.Fprintln(f, "<tr><th>Bot</th><th>Wins</th><th>Losses</th><th>Ties</th></tr>")
	for _, r := range players {
		_, _ = fmt.Fprintf(f, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%d</td></tr>\n", r.Name, r.Wins, r.Losses, r.Ties)
	}
	_, _ = fmt.Fprintln(f, "</table>")
	_, _ = fmt.Fprintln(f, "</body>")
	_, _ = fmt.Fprintln(f, "</html>")
	return nil
}

// applyPlayerNames sets player display names from the -pnN flag values.
// Empty names are skipped, as are names for players that don't exist.
func applyPlayerNames(players []Player, names ...string) {
	for i, name := range names {
		if name != "" && i < len(players) {
			players[i].Name = name
		}
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args, plays the games and returns the process exit code.
func run(args []string) int {
	fs := flag.NewFlagSet("cgarena", flag.ContinueOnError)
	// Command line flags
	hFlag := fs.Bool("h", false, "Print help message")
	vFlag := fs.Bool("v", false, "Print version information")
	nFlag := fs.Int("n", 1, "Number of games to play")
	tFlag := fs.Int("t", 1, "Number of threads to spawn for the games")
	rFlag := fs.String("r", "", "Command line for the referee (required)")
	p1Flag := fs.String("p1", "", "Command line for player 1 (required)")
	p2Flag := fs.String("p2", "", "Command line for player 2 (required)")
	p3Flag := fs.String("p3", "", "Command line for player 3 (optional)")
	p4Flag := fs.String("p4", "", "Command line for player 4 (optional)")
	pn1Flag := fs.String("pn1", "", "Display name for player 1 (optional, default Player1)")
	pn2Flag := fs.String("pn2", "", "Display name for player 2 (optional, default Player2)")
	pn3Flag := fs.String("pn3", "", "Display name for player 3 (optional, default Player3)")
	pn4Flag := fs.String("pn4", "", "Display name for player 4 (optional, default Player4)")
	lFlag := fs.String("l", "off", "Directory for game logs (none or off disables logging)")
	overwriteFlag := fs.Bool("overwrite", false, "Delete match_*.json, match_*.err and games.jsonl from an existing log directory (and replace an existing -out file) instead of refusing to run")
	sFlag := fs.Bool("s", false, "Swap player positions, playing each map from every seating (-n must be a multiple of the number of seatings)")
	iFlag := fs.Int("i", 0, "Base seed (0 or less picks a random one, printed at the start); with -s, each map's seatings share a seed")
	oFlag := fs.Bool("o", false, "Use old mode")
	timeoutFlag := fs.Duration("timeout", 30*time.Second, "Per-game time limit as a Go duration (e.g. 45s, 2m); the referee and the bots it started are killed and the game is invalid (0 disables)")
	outFlag := fs.String("out", "", "Per-game results file, one JSON object per line (default <log dir>/games.jsonl with -l, else none)")
	jsonCheckFlag := fs.Bool("json-check", true, "With -l, make a game invalid (json_log) if the referee's JSON log is missing, unparseable or its scores differ from stdout")
	faultPatternFlag := fs.String("fault-pattern", "", "Regular expression: a player is faulted in a game when a JSON log tooltip or summary line matches it and names its seat as $<seat> (needs -l)")
	configPath := fs.String("config", "", "Path to JSON config")
	markdownPath := fs.String("md", "", "Path to Markdown summary output")
	htmlPath := fs.String("html", "", "Path to HTML summary output")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	if *vFlag {
		fmt.Printf("cgarena version %s\n", VERSION)
		return exitOK
	}

	if *oFlag {
		println("Old mode is not implemented yet.")
	}
	if *sFlag {
		println("Swap player positions is not implemented yet.")
	}

	if *hFlag {
		fmt.Println("Usage:")
		fs.PrintDefaults()
		return exitOK
	}

	// Build config from flags first
	arena := &Arena{
		Referee:   *rFlag,
		Players:   []Player{{Name: "Player1", Cmd: *p1Flag}, {Name: "Player2", Cmd: *p2Flag}},
		Games:     *nFlag,
		Threads:   *tFlag,
		Seed:      *iFlag,
		LogOutput: *lFlag,
		Timeout:   Duration(*timeoutFlag),

		Out:          *outFlag,
		JSONCheck:    jsonCheckFlag,
		FaultPattern: *faultPatternFlag,
	}

	if *p3Flag != "" {
		arena.Players = append(arena.Players, Player{Name: "Player3", Cmd: *p3Flag})
	}

	if *p4Flag != "" {
		arena.Players = append(arena.Players, Player{Name: "Player4", Cmd: *p4Flag})
	}

	applyPlayerNames(arena.Players, *pn1Flag, *pn2Flag, *pn3Flag, *pn4Flag)

	if *sFlag {
		arena.Swap = true
	}
	if *overwriteFlag {
		arena.Overwrite = true
	}

	// If arena config file is provided, override values from it
	if *configPath != "" {
		fileCfg, err := loadConfig(*configPath)
		if err != nil {
			log.Printf("Error: %v", err)
			return exitUsage
		}
		if fileCfg.Referee != "" {
			arena.Referee = fileCfg.Referee
		}
		if len(fileCfg.Players) > 0 {
			arena.Players = fileCfg.Players
		}
		if fileCfg.Games != 0 {
			arena.Games = fileCfg.Games
		}
		if fileCfg.Threads != 0 {
			arena.Threads = fileCfg.Threads
		}
		if fileCfg.Seed != 0 {
			arena.Seed = fileCfg.Seed
		}
		if fileCfg.LogOutput != "" {
			arena.LogOutput = fileCfg.LogOutput
		}
		if fileCfg.Swap {
			arena.Swap = fileCfg.Swap
		}
		if fileCfg.Overwrite {
			arena.Overwrite = fileCfg.Overwrite
		}
		if fileCfg.Timeout != 0 {
			arena.Timeout = fileCfg.Timeout
		}
		if fileCfg.Out != "" {
			arena.Out = fileCfg.Out
		}
		if fileCfg.JSONCheck != nil {
			arena.JSONCheck = fileCfg.JSONCheck
		}
		if fileCfg.FaultPattern != "" {
			arena.FaultPattern = fileCfg.FaultPattern
		}
	}

	// Check required flags
	if arena.Referee == "" || len(arena.Players) < 2 || arena.Players[0].Cmd == "" || arena.Players[1].Cmd == "" {
		fmt.Println("Error: referee, player 1, and player 2 are required.")
		fs.PrintDefaults()
		return exitUsage
	}
	if err := arena.Validate(); err != nil {
		fmt.Println("Error: " + err.Error())
		return exitUsage
	}

	if err := runArena(arena); err != nil {
		log.Printf("Failed to run arena: %v", err)
		return exitUsage
	}
	if arena.Interrupted {
		// The run is incomplete: no summary files, no no-result check.
		fmt.Println(arena.InterruptedLine())
		fmt.Println(arena.PrintResults())
		return exitSignal
	}

	if markdownPath != nil && *markdownPath != "" {
		// write markdown summary
		if err := writeMarkdown(arena.Players, *markdownPath); err != nil {
			log.Printf("Failed to write markdown: %v", err)
			return exitUsage
		}
	}
	if htmlPath != nil && *htmlPath != "" {
		// write HTML summary
		if err := writeHTML(arena.Players, *htmlPath); err != nil {
			log.Printf("Failed to write HTML: %v", err)
			return exitUsage
		}
	}

	fmt.Println("All matches complete.")
	fmt.Println(arena.PrintResults())
	for _, warning := range arena.FaultWarnings() {
		fmt.Println(warning)
	}
	noResult := arena.NoResultMessage()
	if noResult != "" {
		fmt.Println(noResult)
	}
	if arena.Invalid > 0 || noResult != "" {
		return exitInvalid
	}
	return exitOK
}
