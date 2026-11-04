package main

import (
	"bufio"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Real JSON logs written by the TryAngle Catch referee (league 4, seed 5):
//   - tryangle_wait_vs_exit.json: seat 0 waits, seat 1 exits at once, so
//     scores 0/-1, tooltip "$1 timeout!", summary "$1: timeout\n";
//   - tryangle_exit_vs_fly.json: seat 0 exits, seat 1 prints FLY, so -1/-1,
//     tooltips "$0 timeout!" and "$1 Unknown command: FLY!", and one summary
//     "$0: timeout\n$1: Unknown command: FLY\n" naming both seats.
const (
	fixtureWaitVsExit = "testdata/tryangle_wait_vs_exit.json"
	fixtureExitVsFly  = "testdata/tryangle_exit_vs_fly.json"
)

// jsonReferee is a fake referee that sets $g to the game number (seed - 10,
// so run it with -i 10), $l to its -l path ("" without logging) and $p1 to
// the seat 1 command, writes "stderr <g>" to stderr, then runs cases (a sh
// case body over $g) to print the scores and write the JSON log.
func jsonReferee(t *testing.T, cases string) string {
	t.Helper()
	return fakeReferee(t, `l=""
while [ $# -gt 0 ]; do
  case "$1" in
    -seed) g=$(($2 - 10)); shift ;;
    -l) l=$2; shift ;;
    -p1) p1=$2; shift ;;
  esac
  shift
done
echo "stderr $g" >&2
case $g in
`+cases+`
esac`)
}

// absPath returns the absolute path of a repo-relative file, for fake
// referees, which may run in another directory.
func absPath(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// readJSONL reads a results file, returning its raw lines and the records
// sorted by game.
func readJSONL(t *testing.T, path string) ([]string, []GameRecord) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	var records []GameRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec GameRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("bad JSONL line %q: %v", sc.Text(), err)
		}
		lines = append(lines, sc.Text())
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Game < records[j].Game })
	return lines, records
}

func TestRun_JSONLRecords(t *testing.T) {
	ref := jsonReferee(t, `0) echo '{"scores":{"0":5,"1":3},"tooltips":[{"text":"$1 lost a unit"},{"text":"$1 is dominated!"}]}' > "$l"; echo 5; echo 3 ;;
1) echo '{"scores":{"0":4,"1":4}}' > "$l"; echo 4; echo 4 ;;
2) echo '{"scores":{"0":-1,"1":-1},"tooltips":[{"text":"$0 timeout!"}]}' > "$l"; echo -1; echo -1 ;;
*) exit 2 ;;`)
	dir := filepath.Join(t.TempDir(), "logs")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-pn1", "A", "-p2", "b", "-pn2", "B",
		"-n", "4", "-t", "2", "-i", "10", "-l", dir)
	if code != exitInvalid {
		t.Fatalf("exit = %d, want %d\n%s", code, exitInvalid, out)
	}
	lines, recs := readJSONL(t, filepath.Join(dir, "games.jsonl"))
	if len(recs) != 4 {
		t.Fatalf("got %d records, want 4: %v", len(lines), lines)
	}
	type want struct {
		scores  []int
		outcome map[string]string
		end     string
		reason  string
	}
	wants := []want{
		{[]int{5, 3}, map[string]string{"A": "win", "B": "loss"}, "$1 is dominated!", ""},
		{[]int{4, 4}, map[string]string{"A": "draw", "B": "draw"}, "", ""},
		{[]int{-1, -1}, map[string]string{"A": "double_forfeit", "B": "double_forfeit"}, "$0 timeout!", ""},
		{nil, map[string]string{"A": "invalid", "B": "invalid"}, "", "exit_status_2"},
	}
	for g, w := range wants {
		rec := recs[g]
		if rec.Game != g || rec.Seed != 10+g || strings.Join(rec.Seats, " ") != "A B" {
			t.Errorf("game %d: game/seed/seats = %d/%d/%v", g, rec.Game, rec.Seed, rec.Seats)
		}
		if !slices.Equal(rec.Scores, w.scores) || (w.scores == nil) != (rec.Scores == nil) {
			t.Errorf("game %d: scores = %v, want %v", g, rec.Scores, w.scores)
		}
		if !maps.Equal(rec.Outcome, w.outcome) {
			t.Errorf("game %d: outcome = %v, want %v", g, rec.Outcome, w.outcome)
		}
		if rec.EndReason != w.end || rec.InvalidReason != w.reason {
			t.Errorf("game %d: end_reason %q, invalid_reason %q; want %q, %q", g, rec.EndReason, rec.InvalidReason, w.end, w.reason)
		}
		if rec.Fault["A"] || rec.Fault["B"] || len(rec.Fault) != 2 {
			t.Errorf("game %d: fault = %v, want both false (no -fault-pattern)", g, rec.Fault)
		}
		if wantLog := filepath.Join(dir, "match_"+strconv.Itoa(g)+".json"); rec.Log != wantLog || rec.DurationMs < 0 {
			t.Errorf("game %d: log %q, duration_ms %d; want %q, >= 0", g, rec.Log, rec.DurationMs, wantLog)
		}
		data, err := os.ReadFile(filepath.Join(dir, "match_"+strconv.Itoa(g)+".err"))
		if err != nil || !strings.Contains(string(data), "stderr "+strconv.Itoa(g)) {
			t.Errorf("game %d: stderr file = %q, %v; want it to hold the referee's stderr", g, data, err)
		}
	}
	for _, line := range lines {
		if strings.Contains(line, `"game":3,`) && !strings.Contains(line, `"scores":null`) {
			t.Errorf("invalid game's scores aren't null: %s", line)
		}
	}
	if strings.Contains(out, "faults") {
		t.Errorf("summary shows faults without -fault-pattern:\n%s", out)
	}
}

func TestRun_JSONCheck(t *testing.T) {
	tests := []struct {
		name   string
		script string
		extra  []string
		want   int    // exit code
		reason string // invalid_reason of game 0
	}{
		{"matching", `echo '{"scores":{"0":5,"1":3}}' > "$l"; echo 5; echo 3`, nil, exitOK, ""},
		{"float scores match", `echo '{"scores":{"0":5.0,"1":3}}' > "$l"; echo 5; echo 3`, nil, exitOK, ""},
		{"mismatch", `echo '{"scores":{"0":3,"1":5}}' > "$l"; echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"one JSON score", `echo '{"scores":{"0":5}}' > "$l"; echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"null JSON score", `echo '{"scores":{"0":5,"1":null}}' > "$l"; echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"no scores key", `echo '{}' > "$l"; echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"missing log", `echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"unparseable log", `echo 'not json' > "$l"; echo 5; echo 3`, nil, exitInvalid, ReasonJSONLog},
		{"missing log, check off", `echo 5; echo 3`, []string{"-json-check=false"}, exitOK, ""},
		{"mismatch, check off", `echo '{"scores":{"0":3,"1":5},"tooltips":[{"text":"end"}]}' > "$l"; echo 5; echo 3`,
			[]string{"-json-check=false"}, exitOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			args := append([]string{"-r", jsonReferee(t, "*) "+tt.script+" ;;"), "-p1", "a", "-p2", "b",
				"-i", "10", "-l", dir}, tt.extra...)
			var logs strings.Builder
			captureLog(t, &logs)
			if got := run(args); got != tt.want {
				t.Errorf("exit = %d, want %d\n%s", got, tt.want, logs.String())
			}
			_, recs := readJSONL(t, filepath.Join(dir, "games.jsonl"))
			if len(recs) != 1 || recs[0].InvalidReason != tt.reason {
				t.Fatalf("records = %+v, want one with invalid_reason %q", recs, tt.reason)
			}
			if tt.reason != "" && !strings.Contains(logs.String(), "invalid: json_log") {
				t.Errorf("no json_log invalid line in log output:\n%s", logs.String())
			}
		})
	}
}

func TestRun_JSONCheckFromConfig(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, []byte(`{"json_check": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := fakeReferee(t, "echo 5; echo 3") // writes no JSON log
	if got := run([]string{"-r", ref, "-p1", "a", "-p2", "b", "-l", dir, "-config", config}); got != exitOK {
		t.Errorf("exit = %d, want %d with json_check false in the config", got, exitOK)
	}
}

// TestRun_FaultPattern runs a swap pair in which the player with command
// "broken" faults in both seats: game 0 uses the real wait-vs-exit log
// (seat 1 times out), game 1 a crafted log in which seat 0 times out.
func TestRun_FaultPattern(t *testing.T) {
	fixture := absPath(t, fixtureWaitVsExit)
	ref := jsonReferee(t, `*) if [ "$p1" = good ]; then cp `+fixture+` "$l"; echo 0; echo -1
   else printf '%s\n' '{"scores":{"0":-1,"1":0},"tooltips":[{"text":"$0 timeout!"}],"summaries":["","$0: timeout\n$1 captured 1 triangle\n"]}' > "$l"; echo -1; echo 0; fi ;;`)
	dir := t.TempDir()
	code, out := runCapture(t, "-r", ref, "-p1", "good", "-pn1", "A", "-p2", "broken", "-pn2", "B",
		"-n", "2", "-s", "-i", "10", "-l", dir, "-fault-pattern", "timeout|Unknown command|not available")
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, out)
	}
	_, recs := readJSONL(t, filepath.Join(dir, "games.jsonl"))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	for g, rec := range recs {
		if rec.Fault["A"] || !rec.Fault["B"] {
			t.Errorf("game %d (seats %v): fault = %v, want only B", g, rec.Seats, rec.Fault)
		}
		if rec.Outcome["A"] != OutcomeWin || rec.Outcome["B"] != OutcomeLoss {
			t.Errorf("game %d: outcome = %v, want A win, B loss (faults don't change outcomes)", g, rec.Outcome)
		}
	}
	if recs[0].EndReason != "$1 timeout!" || recs[1].EndReason != "$0 timeout!" {
		t.Errorf("end reasons = %q, %q", recs[0].EndReason, recs[1].EndReason)
	}
	for _, want := range []string{
		"A: 2 W / 0 L / 0 D   win rate 100.0%  95% CI [34.2%, 100.0%]   faults 0\n",
		"B: 0 W / 2 L / 0 D   win rate 0.0%  95% CI [0.0%, 65.8%]   faults 2\n",
		"Warning: B faulted in all 2 games (check its command)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Warning: A") {
		t.Errorf("unexpected warning for A:\n%s", out)
	}
}

func TestRun_FaultWarningOnlyWhenAllGamesFaulted(t *testing.T) {
	// Seat 1 (B) faults in game 0 only; game 2 is invalid, which doesn't
	// count either way. A double forfeit (game 1) is a valid game.
	ref := jsonReferee(t, `0) echo '{"scores":{"0":1,"1":0},"tooltips":[{"text":"$1 timeout!"}]}' > "$l"; echo 1; echo 0 ;;
1) echo '{"scores":{"0":-1,"1":-1},"tooltips":[{"text":"$0 timeout!"}]}' > "$l"; echo -1; echo -1 ;;
*) exit 2 ;;`)
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-pn1", "A", "-p2", "b", "-pn2", "B",
		"-n", "3", "-i", "10", "-l", t.TempDir(), "-fault-pattern", "timeout")
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	if strings.Contains(out, "Warning:") {
		t.Errorf("unexpected fault warning (each player faulted in 1 of 2 valid games):\n%s", out)
	}
	for _, want := range []string{"   faults 1\nB:", "B: 0 W / 1 L / 0 D   win rate 0.0%  95% CI [0.0%, 79.3%]   faults 1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRun_FaultPatternUsageErrors(t *testing.T) {
	ref := fakeReferee(t, `touch "$(dirname "$0")/ran"; echo 5; echo 3`)
	cases := map[string][]string{
		"bad regex":   {"-l", t.TempDir(), "-fault-pattern", "time(out"},
		"logging off": {"-l", "off", "-fault-pattern", "timeout"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			code, out := runCapture(t, append([]string{"-r", ref, "-p1", "a", "-p2", "b"}, extra...)...)
			if code != exitUsage {
				t.Errorf("exit = %d, want %d\n%s", code, exitUsage, out)
			}
			if !strings.Contains(out, "fault pattern") {
				t.Errorf("error doesn't mention the fault pattern:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(ref), "ran")); err == nil {
				t.Error("referee ran despite the usage error")
			}
		})
	}
}

func TestRun_FaultPatternFromConfig(t *testing.T) {
	ref := jsonReferee(t, `*) echo '{"scores":{"0":1,"1":0},"tooltips":[{"text":"$1 timeout!"}]}' > "$l"; echo 1; echo 0 ;;`)
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, []byte(`{"fault_pattern": "timeout"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-i", "10", "-l", t.TempDir(), "-config", config)
	if code != exitOK || !strings.Contains(out, "Warning: Player2 faulted in all 1 games") {
		t.Errorf("exit = %d, want %d with a Player2 warning:\n%s", code, exitOK, out)
	}
}

func TestGameLog_FaultsInRealLog(t *testing.T) {
	gl, err := readGameLog(fixtureExitVsFly)
	if err != nil {
		t.Fatal(err)
	}
	if err := gl.checkScores([]int{-1, -1}); err != nil {
		t.Errorf("checkScores: %v", err)
	}
	if got := gl.endReason(); got != "$1 Unknown command: FLY!" {
		t.Errorf("endReason = %q", got)
	}
	// The summary "$0: timeout\n$1: Unknown command: FLY\n" names both seats,
	// so faults are matched line by line: "timeout" faults only seat 0.
	for _, tt := range []struct {
		pattern string
		want    [2]bool
	}{
		{"timeout|Unknown command|not available", [2]bool{true, true}},
		{"timeout", [2]bool{true, false}},
		{"Unknown command", [2]bool{false, true}},
		{"not available", [2]bool{false, false}},
	} {
		re := regexp.MustCompile(tt.pattern)
		for seat := 0; seat < 2; seat++ {
			if got := gl.faulted(re, seat); got != tt.want[seat] {
				t.Errorf("pattern %q seat %d: faulted = %v, want %v", tt.pattern, seat, got, tt.want[seat])
			}
		}
	}
}

func TestNamesSeat(t *testing.T) {
	for _, tt := range []struct {
		line string
		seat int
		want bool
	}{
		{"$0 timeout!", 0, true},
		{"$0 timeout!", 1, false},
		{"$1: Unknown command: FLY", 1, true},
		{"$1: Unknown command: FLY", 0, false},
		{"$12 timeout", 1, false},
		{"$12 timeout, $1 too", 1, true},
		{"timeout $1", 1, true},
		{"0 timeout", 0, false},
	} {
		if got := namesSeat(tt.line, tt.seat); got != tt.want {
			t.Errorf("namesSeat(%q, %d) = %v, want %v", tt.line, tt.seat, got, tt.want)
		}
	}
}

// TestArena_InterruptedGamesNotInJSONL cancels a run once game 0 is in the
// results file, while game 1 is still running: only game 0 is recorded.
func TestArena_InterruptedGamesNotInJSONL(t *testing.T) {
	ref := jsonReferee(t, `0) echo '{"scores":{"0":1,"1":0}}' > "$l"; echo 1; echo 0 ;;
*) exec sleep 60 ;;`)
	dir := t.TempDir()
	out := filepath.Join(dir, "games.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if data, _ := os.ReadFile(out); strings.Count(string(data), "\n") >= 1 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond) // let game 1 start
		cancel()
	}()
	arena := &Arena{Referee: ref, Players: twoPlayers(), Games: 2, Threads: 2, Seed: 10, LogOutput: dir}
	if err := arena.RunContext(ctx); err != nil {
		t.Fatal(err)
	}
	if !arena.Interrupted {
		t.Fatal("run wasn't interrupted")
	}
	_, recs := readJSONL(t, out)
	if len(recs) != 1 || recs[0].Game != 0 {
		t.Errorf("records = %+v, want only game 0", recs)
	}
}

func TestRun_OutFile(t *testing.T) {
	ref := jsonReferee(t, `*) echo 5; echo 3 ;;`)
	base := func(out string, extra ...string) []string {
		return append([]string{"-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-i", "10", "-l", "off", "-out", out}, extra...)
	}

	t.Run("logging off", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "results.jsonl")
		if got := run(base(out)); got != exitOK {
			t.Fatalf("exit = %d, want %d", got, exitOK)
		}
		_, recs := readJSONL(t, out)
		if len(recs) != 2 || recs[0].Log != "" || recs[0].EndReason != "" || recs[1].Outcome["Player1"] != OutcomeWin {
			t.Errorf("records = %+v", recs)
		}
	})
	t.Run("existing empty file is used", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "results.jsonl")
		if err := os.WriteFile(out, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := run(base(out)); got != exitOK {
			t.Fatalf("exit = %d, want %d", got, exitOK)
		}
		if _, recs := readJSONL(t, out); len(recs) != 2 {
			t.Errorf("got %d records, want 2", len(recs))
		}
	})
	t.Run("existing non-empty file is refused", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "results.jsonl")
		if err := os.WriteFile(out, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		logDir := filepath.Join(t.TempDir(), "logs")
		if got := run(append(base(out), "-l", logDir, "-json-check=false")); got != exitUsage {
			t.Fatalf("exit = %d, want %d", got, exitUsage)
		}
		if data, _ := os.ReadFile(out); string(data) != "old\n" {
			t.Errorf("results file was modified: %q", data)
		}
		if _, err := os.Stat(logDir); err == nil {
			t.Error("log dir was created although the run was refused")
		}
	})
	t.Run("existing non-empty file with -overwrite is replaced", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "results.jsonl")
		if err := os.WriteFile(out, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := run(base(out, "-overwrite")); got != exitOK {
			t.Fatalf("exit = %d, want %d", got, exitOK)
		}
		if lines, _ := readJSONL(t, out); len(lines) != 2 {
			t.Errorf("got %d lines, want 2", len(lines))
		}
	})
	t.Run("from config", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "results.jsonl")
		config := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(config, []byte(`{"out": "`+out+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := run([]string{"-r", ref, "-p1", "a", "-p2", "b", "-i", "10", "-l", "off", "-config", config}); got != exitOK {
			t.Fatalf("exit = %d, want %d", got, exitOK)
		}
		if _, recs := readJSONL(t, out); len(recs) != 1 {
			t.Errorf("got %d records, want 1", len(recs))
		}
	})
	t.Run("no file without logging or -out", func(t *testing.T) {
		wd := t.TempDir()
		t.Chdir(wd)
		if got := run([]string{"-r", ref, "-p1", "a", "-p2", "b", "-i", "10", "-l", "off"}); got != exitOK {
			t.Fatalf("exit = %d, want %d", got, exitOK)
		}
		if names := dirNames(t, wd); len(names) != 0 {
			t.Errorf("working dir holds %v, want nothing", names)
		}
	})
}
