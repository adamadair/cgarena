package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
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
	// Out is the per-game results file (JSONL). Empty means
	// <LogOutput>/games.jsonl with logging on, and no file otherwise.
	Out string `json:"out"`
	// JSONCheck cross-checks each game's JSON log against stdout when
	// logging is on (see Referee.JSONCheck); nil means on.
	JSONCheck *bool `json:"json_check"`
	// FaultPattern is a regular expression for faults (see
	// Referee.FaultPattern); it needs logging on.
	FaultPattern string `json:"fault_pattern"`
	// Z is the z value of the win-rate confidence interval (see wilson); nil
	// means defaultZ (1.96, 95%). It must be > 0.
	Z *float64 `json:"z"`
	// SummaryJSON is the path of the JSON summary (RunSummary); "" for none.
	SummaryJSON string `json:"summary_json"`
	// OldMode plays games with an old-style referee (see OldReferee): it
	// talks to the bots through cgarena over stdin/stdout instead of
	// starting them itself.
	OldMode bool `json:"old_mode"`
	// RunLog is the run log: one RunSummary JSON line is appended per run
	// (see appendRunLog); "" for none.
	RunLog string `json:"runlog"`

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

	// out is the open results file during RunContext (nil if none), and
	// outErr the first error writing it. Only the collector writes them.
	out    *os.File
	outErr error
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
	if a.Z != nil && !(*a.Z > 0 && !math.IsInf(*a.Z, 1)) {
		return fmt.Errorf("confidence z value (-z) must be a positive number, got %v", *a.Z)
	}
	if _, err := a.faultRegexp(); err != nil {
		return err
	}
	if a.FaultPattern != "" && a.OldMode {
		return fmt.Errorf("fault pattern (-fault-pattern) can't be used with old mode (-o): old referees write no JSON log " +
			"(a player that crashes is counted as a fault instead)")
	}
	if a.FaultPattern != "" && !a.IsLoggingOutput() {
		return fmt.Errorf("fault pattern (-fault-pattern) needs logging on (-l <dir>): faults are read from the referee's JSON logs")
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

// faultRegexp compiles FaultPattern; it returns nil for an empty pattern.
func (a *Arena) faultRegexp() (*regexp.Regexp, error) {
	if a.FaultPattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(a.FaultPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid fault pattern (-fault-pattern) %q: %v", a.FaultPattern, err)
	}
	return re, nil
}

// z returns the z value of the confidence interval.
func (a *Arena) z() float64 {
	if a.Z == nil {
		return defaultZ
	}
	return *a.Z
}

// jsonCheck reports whether JSON logs are cross-checked (with logging on).
func (a *Arena) jsonCheck() bool {
	return a.JSONCheck == nil || *a.JSONCheck
}

// resultsPath returns the per-game results file, or "" for none.
func (a *Arena) resultsPath() string {
	if a.Out != "" {
		return a.Out
	}
	if a.IsLoggingOutput() {
		return filepath.Join(a.LogOutput, "games.jsonl")
	}
	return ""
}

// checkResultsFile refuses an existing non-empty results file unless
// Overwrite is set, so earlier results are never lost by accident.
func checkResultsFile(path string, overwrite bool) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("results file %q: %w", path, err)
	}
	if info.IsDir() {
		return usageErrorf("results file (-out) %q is a directory", path)
	}
	if info.Size() > 0 && !overwrite {
		return usageErrorf("results file (-out) %q already holds results from an earlier run; "+
			"use -overwrite to replace it, or choose another -out file", path)
	}
	return nil
}

// UsageError is an error RunContext returns for a setting that can't be used
// (an existing log directory or -out file holding earlier results): a usage
// error, not a failure during the run.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usageErrorf(format string, args ...any) error {
	return &UsageError{msg: fmt.Sprintf(format, args...)}
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

	a.out = nil
	a.outErr = nil
	faultPattern, err := a.faultRegexp()
	if err != nil {
		return err
	}

	order := a.seatOrders()
	k := len(order)
	randomSeed := a.Seed <= 0
	if randomSeed {
		a.Seed = randomBaseSeed(a.Games / k)
	}

	outPath := a.resultsPath()
	if outPath != "" && a.Out != "" {
		// The default (games.jsonl in the log dir) is checked by prepareLogDir.
		if err := checkResultsFile(outPath, a.Overwrite); err != nil {
			return err
		}
	}
	if a.IsLoggingOutput() {
		if err := prepareLogDir(a.LogOutput, a.Overwrite); err != nil {
			return err
		}
	}
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("results file: %w", err)
		}
		defer f.Close()
		a.out = f
	}
	fmt.Println(a.seedRangeLine(randomSeed))

	refCmd, seed, logOutput, timeout := a.Referee, a.Seed, a.LogOutput, time.Duration(a.Timeout)
	jsonCheck := a.jsonCheck()
	oldMode := a.OldMode
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
				seats := append([]Seat(nil), order[matchID%k]...)
				mapSeed := gameSeed(seed, matchID, k)
				var result *RefereeResult
				var err error
				if oldMode {
					ref := OldReferee{
						Command:   refCmd,
						Seats:     seats,
						Seed:      mapSeed,
						LogOutput: logOutput,
						Timeout:   timeout,
					}
					result, err = ref.RunContext(ctx, matchID)
				} else {
					ref := Referee{
						Command:   refCmd,
						Seats:     seats,
						Seed:      mapSeed,
						LogOutput: logOutput,
						Timeout:   timeout,
						JSONCheck: jsonCheck,
						// A compiled Regexp is safe for concurrent use.
						FaultPattern: faultPattern,
					}
					result, err = ref.RunContext(ctx, matchID)
				}
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
	if a.outErr != nil {
		return fmt.Errorf("writing results file: %w", a.outErr)
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
	a.writeRecord(gr)
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
		p.Played++
		if result.Faults[j] {
			p.Faults++
		}
	}
	log.Printf("Match %d completed: %v", gr.matchID, result.String())
}

// GameRecord is one line of the per-game results file (-out).
type GameRecord struct {
	Game          int               `json:"game"`
	Seed          int               `json:"seed"`
	Seats         []string          `json:"seats"`  // player name per seat
	Scores        []int             `json:"scores"` // per seat; null for invalid games
	Outcome       map[string]string `json:"outcome"`
	EndReason     string            `json:"end_reason"`
	Fault         map[string]bool   `json:"fault"`
	InvalidReason string            `json:"invalid_reason"`
	DurationMs    int64             `json:"duration_ms"`
	Log           string            `json:"log"`
	// Ranking is the old-mode ###End ranking (seat indices best first,
	// tied seats written together); left out otherwise.
	Ranking string `json:"ranking,omitempty"`
}

// newGameRecord builds the results-file record of a finished game.
func newGameRecord(gr gameResult) GameRecord {
	result := gr.result
	rec := GameRecord{
		Game:          gr.matchID,
		Seed:          result.Seed,
		Seats:         make([]string, len(result.Seats)),
		Outcome:       map[string]string{},
		EndReason:     result.EndReason,
		Fault:         map[string]bool{},
		InvalidReason: result.InvalidReason,
		DurationMs:    result.Duration.Milliseconds(),
		Log:           result.LogFile,
		Ranking:       result.Ranking,
	}
	if result.InvalidReason == "" {
		rec.Scores = result.Scores
	}
	for j, seat := range result.Seats {
		rec.Seats[j] = seat.Name
		outcome := OutcomeInvalid
		if result.InvalidReason == "" && j < len(result.Outcomes) {
			outcome = result.Outcomes[j]
		}
		rec.Outcome[seat.Name] = outcome
		rec.Fault[seat.Name] = j < len(result.Faults) && result.Faults[j]
	}
	return rec
}

// writeRecord appends gr's record to the results file, if there is one. It
// must only be called from the collector goroutine. After the first write
// error it stops writing; RunContext returns that error.
func (a *Arena) writeRecord(gr gameResult) {
	if a.out == nil || a.outErr != nil {
		return
	}
	data, err := json.Marshal(newGameRecord(gr))
	if err == nil {
		_, err = a.out.Write(append(data, '\n'))
	}
	if err != nil {
		a.outErr = err
		log.Printf("Failed to write results file: %v", err)
	}
}

// FaultWarnings returns one warning per player that faulted in every valid
// game it played (none if no game was valid for it).
func (a *Arena) FaultWarnings() []string {
	var warnings []string
	for _, p := range a.Players {
		if p.Played > 0 && p.Faults == p.Played {
			warnings = append(warnings, fmt.Sprintf("Warning: %s faulted in all %d games (check its command)", p.Name, p.Played))
		}
	}
	return warnings
}

// isResultFile reports whether a file name is one cgarena writes into the log
// directory: match_*.json, match_*.err, match_*.log (old mode) or games.jsonl. Only these files are
// considered earlier results, and only these are deleted by -overwrite.
func isResultFile(name string) bool {
	if name == "games.jsonl" {
		return true
	}
	for _, pattern := range []string{"match_*.json", "match_*.err", "match_*.log"} {
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
		return usageErrorf("log directory %q already contains %d result file(s) from an earlier run (%s); "+
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
	return isLogging(a.LogOutput)
}

// isLogging reports whether the log directory setting dir turns logging on:
// it is not empty, "none" or "off".
func isLogging(dir string) bool {
	return dir != "" && dir != "none" && dir != "off"
}

// PrintResults returns the run's summary text (see RunSummary.Text).
func (a *Arena) PrintResults() string {
	return a.Summary(time.Now()).Text()
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
