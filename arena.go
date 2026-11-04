package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Arena struct {
	Referee   string   `json:"referee"`
	Players   []Player `json:"players"`
	Games     int      `json:"games"`
	Threads   int      `json:"threads"`
	Seed      int      `json:"seed"`
	LogOutput string   `json:"log_output"`
	Swap      bool     `json:"swap"` // Whether to switch players after each game
	// Overwrite lets Run delete earlier results (see isResultFile) from an
	// existing log directory instead of refusing to use it.
	Overwrite bool `json:"overwrite"`
	// Timeout limits each game (see Referee.Timeout); 0 means no limit. In
	// the config file it is a Go duration string, e.g. "45s".
	Timeout Duration `json:"timeout"`

	// Run totals that aren't attributed to a player. Decided counts valid
	// games that produced a result (win, loss or draw).
	Decided        int            `json:"-"`
	DoubleForfeits int            `json:"-"`
	Invalid        int            `json:"-"`
	InvalidReasons map[string]int `json:"-"`
	// Interrupted is set when the run's context was cancelled (SIGINT or
	// SIGTERM) before every game finished. Killed counts the referees that
	// were running then and were killed; their games count toward nothing.
	Interrupted bool `json:"-"`
	Killed      int  `json:"-"`
}

// Validate checks the settings Run depends on.
func (a *Arena) Validate() error {
	if a.Games < 1 {
		return fmt.Errorf("number of games (-n) must be at least 1, got %d", a.Games)
	}
	if a.Threads < 1 {
		return fmt.Errorf("number of threads (-t) must be at least 1, got %d", a.Threads)
	}
	if k := len(a.seatOrders()); a.Swap && a.Games%k != 0 {
		return fmt.Errorf("with swap (-s) and %d players, the number of games (-n) must be a multiple of %d "+
			"so every map is played from all %d seatings, got %d (e.g. -n %d)",
			len(a.Players), k, k, a.Games, roundUp(a.Games, k))
	}
	if a.Timeout < 0 {
		return fmt.Errorf("timeout (-timeout) must not be negative, got %s", time.Duration(a.Timeout))
	}
	seen := map[string]int{}
	for i, p := range a.Players {
		if j, dup := seen[p.Name]; dup {
			return fmt.Errorf("players %d and %d have the same name %q; player names must be unique "+
				"(for self-play, give distinct names, e.g. -pn1 A -pn2 B, or distinct names in the config file)",
				j+1, i+1, p.Name)
		}
		seen[p.Name] = i
	}
	return nil
}

// Duration is a time.Duration that is written as a Go duration string
// ("45s", "1m30s") in JSON config files.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string such as \"45s\", got %s", data)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// gameResult is what a worker sends to the collector for each game.
type gameResult struct {
	matchID int
	result  *RefereeResult
	err     error
}

// Run plays all games; see RunContext.
func (a *Arena) Run() error {
	return a.RunContext(context.Background())
}

// RunContext plays all games. When ctx is cancelled, no new game is started,
// the referees still running are killed, and RunContext returns nil once the
// workers are done, with Interrupted set. Games that didn't finish count
// toward nothing.
func (a *Arena) RunContext(ctx context.Context) error {
	if err := a.Validate(); err != nil {
		return err
	}
	a.Decided = 0
	a.DoubleForfeits = 0
	a.Invalid = 0
	a.InvalidReasons = map[string]int{}
	a.Interrupted = false
	a.Killed = 0

	order := a.seatOrders()
	k := len(order)
	randomSeed := a.Seed <= 0
	if randomSeed {
		a.Seed = randomBaseSeed(a.Games / k)
	}

	if a.IsLoggingOutput() {
		if err := prepareLogDir(a.LogOutput, a.Overwrite); err != nil {
			return err
		}
	}
	fmt.Println(a.seedRangeLine(randomSeed))

	refCmd, seed, logOutput, timeout := a.Referee, a.Seed, a.LogOutput, time.Duration(a.Timeout)
	jobs := make(chan int, a.Games)
	results := make(chan gameResult)

	// Workers: play games and send each result to the collector.
	var wg sync.WaitGroup
	for i := 0; i < a.Threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for matchID := range jobs {
				if ctx.Err() != nil {
					continue // interrupted: don't start any more games
				}
				ref := Referee{
					Command:   refCmd,
					Seats:     append([]Seat(nil), order[matchID%k]...),
					Seed:      gameSeed(seed, matchID, k),
					LogOutput: logOutput,
					Timeout:   timeout,
				}
				result, err := ref.RunContext(ctx, matchID)
				results <- gameResult{matchID: matchID, result: result, err: err}
			}
		}()
	}

	// Collector: the only goroutine that touches the counters.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for gr := range results {
			a.collect(gr)
		}
	}()

	// Job dispatch loop
	for i := 0; i < a.Games && ctx.Err() == nil; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)
	<-done

	if ctx.Err() != nil && a.Finished() < a.Games {
		a.Interrupted = true
	}
	return nil
}

// Finished returns the number of games that finished: valid or invalid, but
// not interrupted.
func (a *Arena) Finished() int {
	return a.Decided + a.DoubleForfeits + a.Invalid
}

// InterruptedLine is printed when the run was interrupted.
func (a *Arena) InterruptedLine() string {
	return fmt.Sprintf("Interrupted: %d of %d games finished; killed %d running referee(s)",
		a.Finished(), a.Games, a.Killed)
}

// collect merges one game's result into the run totals, attributing each
// seat's W/L/T to the player at that seat's index. It must only be called
// from the collector goroutine.
func (a *Arena) collect(gr gameResult) {
	result := gr.result
	if result.Interrupted {
		if result.Killed {
			a.Killed++
			log.Printf("Game %d (seed %d) interrupted: referee killed", gr.matchID, result.Seed)
		}
		return
	}
	if gr.err != nil {
		a.Invalid++
		a.InvalidReasons[result.InvalidReason]++
		log.Printf("Game %d (seed %d) invalid: %v", gr.matchID, result.Seed, gr.err)
		return
	}
	if result.IsDoubleForfeit() {
		a.DoubleForfeits++
	} else {
		a.Decided++
	}
	for j, seat := range result.Seats {
		p := &a.Players[seat.Index]
		p.Wins += result.Players[j].Wins
		p.Losses += result.Players[j].Losses
		p.Ties += result.Players[j].Ties
	}
	log.Printf("Match %d completed: %v", gr.matchID, result.String())
}

// isResultFile reports whether a file name is one cgarena writes into the log
// directory: match_*.json, match_*.err or games.jsonl. Only these files are
// considered earlier results, and only these are deleted by -overwrite.
func isResultFile(name string) bool {
	if name == "games.jsonl" {
		return true
	}
	for _, pattern := range []string{"match_*.json", "match_*.err"} {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// prepareLogDir makes dir ready for a run's logs without ever deleting the
// directory itself. A missing directory is created. An existing directory is
// used as is unless it holds earlier results (see isResultFile): then it is an
// error, or with overwrite those files (and nothing else) are deleted.
func prepareLogDir(dir string, overwrite bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return fmt.Errorf("log directory %q: %w", dir, err)
	}
	var stale []string
	for _, e := range entries {
		if !e.IsDir() && isResultFile(e.Name()) {
			stale = append(stale, e.Name())
		}
	}
	if len(stale) == 0 {
		return nil
	}
	if !overwrite {
		example := stale[0]
		if len(stale) > 1 {
			example += ", ..."
		}
		return fmt.Errorf("log directory %q already contains %d result file(s) from an earlier run (%s); "+
			"use -overwrite to delete them, or choose another -l directory", dir, len(stale), example)
	}
	for _, name := range stale {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// NoResultMessage returns the message for a run in which no game produced a
// result (win, loss or draw) and at least one game was a double forfeit, or ""
// otherwise. Such a run exits with code 3 even when every game was valid,
// because it usually means the bot commands are broken. A run in which every
// game was invalid gets no message: the invalid lines already explain it.
func (a *Arena) NoResultMessage() string {
	if a.Decided > 0 || a.DoubleForfeits == 0 {
		return ""
	}
	if a.Invalid == 0 {
		return fmt.Sprintf("No game produced a result: all %d games were double forfeits (check the bot commands)",
			a.DoubleForfeits)
	}
	return fmt.Sprintf("No game produced a result: %d games were double forfeits and %d invalid (check the bot commands)",
		a.DoubleForfeits, a.Invalid)
}

func (a *Arena) IsLoggingOutput() bool {
	return a.LogOutput != "" && a.LogOutput != "none" && a.LogOutput != "off"
}

func (a *Arena) PrintResults() string {
	var sb strings.Builder
	for _, player := range a.Players {
		sb.WriteString(player.Name + ": " + fmt.Sprintf("%.2f%% Win Rate; ", player.WinPct()*100) +
			strconv.Itoa(player.Wins) + " Wins; " +
			strconv.Itoa(player.Losses) + " Losses; " +
			strconv.Itoa(player.Ties) + " Ties\n")
	}
	sb.WriteString("Double forfeits: " + strconv.Itoa(a.DoubleForfeits) +
		"   Invalid: " + strconv.Itoa(a.Invalid))
	if a.Invalid > 0 {
		reasons := make([]string, 0, len(a.InvalidReasons))
		for reason := range a.InvalidReasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		parts := make([]string, len(reasons))
		for i, reason := range reasons {
			parts[i] = reason + ": " + strconv.Itoa(a.InvalidReasons[reason])
		}
		sb.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	sb.WriteString("\n")
	return sb.String()
}

// seatOrders returns the seat orders games cycle through: game g is played
// in order[g % len(order)]. Without swap there is one order; with swap, two
// players alternate and more players use every permutation.
func (a *Arena) seatOrders() [][]Seat {
	seats := seatsOf(a.Players)
	switch {
	case !a.Swap:
		return [][]Seat{seats}
	case len(seats) == 2:
		return [][]Seat{{seats[0], seats[1]}, {seats[1], seats[0]}}
	default:
		return seatPermutations(seats)
	}
}

// gameSeed returns the seed for game g when games cycle through k seat
// orders: the k games of each block share one seed (one map), so with swap
// on every map is played from every seating.
func gameSeed(base, g, k int) int {
	return base + g/k
}

// maxSeed is the largest seed cgarena sends, so seeds fit a 32-bit int.
const maxSeed = 1<<31 - 1

// randomBaseSeed picks a base seed in [1, 2^31-1] such that all maps seeds
// base … base+maps-1 stay at most 2^31-1.
func randomBaseSeed(maps int) int {
	hi := maxSeed - maps + 1
	if hi < 1 {
		hi = 1
	}
	return 1 + rand.IntN(hi)
}

// seedRangeLine describes the seeds a run uses, printed at its start.
func (a *Arena) seedRangeLine(random bool) string {
	k := len(a.seatOrders())
	maps := a.Games / k
	swap := "off"
	if a.Swap {
		swap = "on"
	}
	line := fmt.Sprintf("Seeds %d..%d (%d maps, swap %s, %d games)", a.Seed, gameSeed(a.Seed, a.Games-1, k), maps, swap, a.Games)
	if random {
		line += fmt.Sprintf("; random base seed, rerun with -i %d", a.Seed)
	}
	return line
}

func roundUp(n, k int) int {
	if n < k {
		return k
	}
	return (n + k - 1) / k * k
}

// seatPermutations returns every ordering of seats, using Heap's algorithm
// on a copy so the input is left unchanged.
func seatPermutations(seats []Seat) [][]Seat {
	arr := append([]Seat(nil), seats...)
	var res [][]Seat
	var generate func([]Seat, int)
	generate = func(a []Seat, n int) {
		if n == 1 {
			res = append(res, append([]Seat(nil), a...))
			return
		}
		for i := 0; i < n; i++ {
			generate(a, n-1)
			if n%2 == 1 {
				a[0], a[n-1] = a[n-1], a[0]
			} else {
				a[i], a[n-1] = a[n-1], a[i]
			}
		}
	}
	generate(arr, len(arr))
	return res
}
