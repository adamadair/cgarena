package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	VERSION = "1.0.2"
)

type Player struct {
	Name   string `json:"name"`
	Cmd    string `json:"cmd"`
	Wins   int
	Losses int
	Ties   int
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

func clearLogDir(path string) {
	_ = os.RemoveAll(path)
	_ = os.MkdirAll(path, 0755)
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
	// Command line flags
	hFlag := flag.Bool("h", false, "Print help message")
	vFlag := flag.Bool("v", false, "Print version information")
	nFlag := flag.Int("n", 1, "Number of games to play")
	tFlag := flag.Int("t", 1, "Number of threads to spawn for the games")
	rFlag := flag.String("r", "", "Command line for the referee (required)")
	p1Flag := flag.String("p1", "", "Command line for player 1 (required)")
	p2Flag := flag.String("p2", "", "Command line for player 2 (required)")
	p3Flag := flag.String("p3", "", "Command line for player 3 (optional)")
	p4Flag := flag.String("p4", "", "Command line for player 4 (optional)")
	pn1Flag := flag.String("pn1", "", "Display name for player 1 (required)")
	pn2Flag := flag.String("pn2", "", "Display name for player 2 (required)")
	pn3Flag := flag.String("pn3", "", "Display name for player 3 (optional)")
	pn4Flag := flag.String("pn4", "", "Display name for player 4 (optional)")
	lFlag := flag.String("l", "logs", "Directory for game logs")
	sFlag := flag.Bool("s", false, "Swap player positions")
	iFlag := flag.Int("i", 0, "Initial seed for repeatable tests")
	oFlag := flag.Bool("o", false, "Use old mode")
	configPath := flag.String("config", "", "Path to JSON config")
	markdownPath := flag.String("md", "", "Path to Markdown summary output")
	htmlPath := flag.String("html", "", "Path to HTML summary output")

	flag.Parse()

	if *vFlag {
		fmt.Printf("cgarena version %s\n", VERSION)
		os.Exit(0)
	}

	if *oFlag {
		println("Old mode is not implemented yet.")
	}
	if *sFlag {
		println("Swap player positions is not implemented yet.")
	}

	if *hFlag {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Build config from flags first
	arena := &Arena{
		Referee:   *rFlag,
		Players:   []Player{{Name: "Player1", Cmd: *p1Flag}, {Name: "Player2", Cmd: *p2Flag}},
		Games:     *nFlag,
		Threads:   *tFlag,
		Seed:      *iFlag,
		LogOutput: *lFlag,
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

	// If arena config file is provided, override values from it
	if *configPath != "" {
		fileCfg, err := loadConfig(*configPath)
		if err != nil {
			log.Fatal(err)
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
	}

	// Check required flags
	if arena.Referee == "" || len(arena.Players) < 2 || arena.Players[0].Cmd == "" || arena.Players[1].Cmd == "" {
		fmt.Println("Error: referee, player 1, and player 2 are required.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	err := arena.Run()
	if err != nil {
		log.Fatalf("Failed to run arena: %v", err)
	}

	if markdownPath != nil && *markdownPath != "" {
		// write markdown summary
		if err := writeMarkdown(arena.Players, *markdownPath); err != nil {
			log.Fatalf("Failed to write markdown: %v", err)
		}
	}
	if htmlPath != nil && *htmlPath != "" {
		// write HTML summary
		if err := writeHTML(arena.Players, *htmlPath); err != nil {
			log.Fatalf("Failed to write HTML: %v", err)
		}
	}

	fmt.Println("All matches complete.")
	fmt.Println(arena.PrintResults())
}
