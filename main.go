package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
	// CommitDir is the git directory the bot was built from (-pcN, config
	// commit_dir); "" for none. Commit and Dirty are its HEAD commit and
	// whether its working tree had changes, read once at the start of the
	// run by resolveCommits (nil without CommitDir or when git failed).
	CommitDir string  `json:"commit_dir"`
	Commit    *string `json:"-"`
	Dirty     *bool   `json:"-"`
	Wins      int
	Losses    int
	Ties      int
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

// markdownEscaper escapes characters that would break a Markdown table cell.
var markdownEscaper = strings.NewReplacer("|", "\\|", "\n", " ")

func writeMarkdown(s RunSummary, path string) error {
	var sb strings.Builder
	sb.WriteString("# Match Summary\n\n")
	sb.WriteString(s.Header() + "\n\n")
	fmt.Fprintf(&sb, "| Bot | W | L | D | Win rate | %s | Faults |\n", s.CILabel())
	sb.WriteString("|-----|---|---|---|----------|--------|--------|\n")
	for _, p := range s.Players {
		fmt.Fprintf(&sb, "| %s | %d | %d | %d | %s | %s | %s |\n", markdownEscaper.Replace(p.Name),
			p.W, p.L, p.D, p.WinRateText(), p.CIText(), p.FaultsText())
	}
	sb.WriteString("\n| Double forfeits | Invalid |\n")
	sb.WriteString("|-----------------|---------|\n")
	fmt.Fprintf(&sb, "| %d | %s |\n", s.DoubleForfeits, s.InvalidText())
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func writeHTML(s RunSummary, path string) error {
	esc := html.EscapeString
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head><title>Match Summary</title></head>\n<body>\n")
	sb.WriteString("<h1>Match Summary</h1>\n")
	sb.WriteString("<p>" + esc(s.Header()) + "</p>\n")
	sb.WriteString("<table border='1'>\n")
	fmt.Fprintf(&sb, "<tr><th>Bot</th><th>W</th><th>L</th><th>D</th><th>Win rate</th><th>%s</th><th>Faults</th></tr>\n",
		esc(s.CILabel()))
	for _, p := range s.Players {
		fmt.Fprintf(&sb, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			esc(p.Name), p.W, p.L, p.D, esc(p.WinRateText()), esc(p.CIText()), esc(p.FaultsText()))
	}
	sb.WriteString("</table>\n")
	sb.WriteString("<table border='1'>\n<tr><th>Double forfeits</th><th>Invalid</th></tr>\n")
	fmt.Fprintf(&sb, "<tr><td>%d</td><td>%s</td></tr>\n", s.DoubleForfeits, esc(s.InvalidText()))
	sb.WriteString("</table>\n</body>\n</html>\n")
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// writeSummaryJSON writes s as indented JSON to path.
func writeSummaryJSON(s RunSummary, path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
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

// applyCommitDirs sets player commit directories from the -pcN flag values,
// skipping empty values and players that don't exist.
func applyCommitDirs(players []Player, dirs ...string) {
	for i, dir := range dirs {
		if dir != "" && i < len(players) {
			players[i].CommitDir = dir
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
	pc1Flag := fs.String("pc1", "", "Git directory player 1 was built from; its commit and dirty state go into -runlog and -summary-json (optional)")
	pc2Flag := fs.String("pc2", "", "Git directory player 2 was built from (optional, see -pc1)")
	pc3Flag := fs.String("pc3", "", "Git directory player 3 was built from (optional, see -pc1)")
	pc4Flag := fs.String("pc4", "", "Git directory player 4 was built from (optional, see -pc1)")
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
	summaryJSONFlag := fs.String("summary-json", "", "Path to JSON summary output (also written for interrupted runs, with \"interrupted\": true)")
	runLogFlag := fs.String("runlog", "", "Run log: append one JSON line per run (same fields as -summary-json), also for runs with invalid games and interrupted runs")
	zFlag := fs.Float64("z", defaultZ, "z value of the win-rate confidence interval (1.96 = 95%, 2.576 = 99%); must be > 0")

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
		Z:            zFlag,
		SummaryJSON:  *summaryJSONFlag,
		RunLog:       *runLogFlag,
	}

	if *p3Flag != "" {
		arena.Players = append(arena.Players, Player{Name: "Player3", Cmd: *p3Flag})
	}

	if *p4Flag != "" {
		arena.Players = append(arena.Players, Player{Name: "Player4", Cmd: *p4Flag})
	}

	applyPlayerNames(arena.Players, *pn1Flag, *pn2Flag, *pn3Flag, *pn4Flag)
	applyCommitDirs(arena.Players, *pc1Flag, *pc2Flag, *pc3Flag, *pc4Flag)

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
		if fileCfg.Z != nil {
			arena.Z = fileCfg.Z
		}
		if fileCfg.SummaryJSON != "" {
			arena.SummaryJSON = fileCfg.SummaryJSON
		}
		if fileCfg.RunLog != "" {
			arena.RunLog = fileCfg.RunLog
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

	// Commits are read once, before any game, so a bot rebuilt during the
	// run doesn't change the record.
	for _, warning := range resolveCommits(arena.Players) {
		fmt.Println(warning)
	}

	if err := runArena(arena); err != nil {
		log.Printf("Failed to run arena: %v", err)
		return exitUsage
	}
	summary := arena.Summary(time.Now())
	// The JSON summary and the run log are written for interrupted runs and
	// runs with invalid games too: they say so in their "interrupted" and
	// "invalid" fields.
	if arena.SummaryJSON != "" {
		if err := writeSummaryJSON(summary, arena.SummaryJSON); err != nil {
			log.Printf("Failed to write JSON summary: %v", err)
			return exitUsage
		}
	}
	if arena.RunLog != "" {
		if err := appendRunLog(summary, arena.RunLog); err != nil {
			log.Printf("Failed to append to the run log: %v", err)
			return exitUsage
		}
	}
	if arena.Interrupted {
		// The run is incomplete: no Markdown/HTML summaries, no no-result check.
		fmt.Println(arena.InterruptedLine())
		fmt.Println(summary.Text())
		return exitSignal
	}

	if *markdownPath != "" {
		if err := writeMarkdown(summary, *markdownPath); err != nil {
			log.Printf("Failed to write markdown: %v", err)
			return exitUsage
		}
	}
	if *htmlPath != "" {
		if err := writeHTML(summary, *htmlPath); err != nil {
			log.Printf("Failed to write HTML: %v", err)
			return exitUsage
		}
	}

	fmt.Println("All matches complete.")
	fmt.Println(summary.Text())
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
