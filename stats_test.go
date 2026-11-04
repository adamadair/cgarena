package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWilson(t *testing.T) {
	tests := []struct {
		name      string
		successes float64
		n         int
		z         float64
		lo, hi    float64
	}{
		{"50 of 100", 50, 100, 1.96, 0.4038, 0.5962},
		{"0 of 10", 0, 10, 1.96, 0, 0.2775},
		{"10 of 10", 10, 10, 1.96, 0.7225, 1},
		{"1 of 1", 1, 1, 1.96, 0.2065, 1},
		{"20 of 20", 20, 20, 1.96, 0.8389, 1},
		// 61 W, 33 L, 4 D: p̂ = 63/98.
		{"draws count half", 61 + 4.0/2, 98, 1.96, 0.5443, 0.7307},
		{"50 of 100 at 99%", 50, 100, 2.576, 0.3753, 0.6247},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi, ok := wilson(tt.successes, tt.n, tt.z)
			if !ok {
				t.Fatal("ok = false")
			}
			if math.Abs(lo-tt.lo) > 5e-5 || math.Abs(hi-tt.hi) > 5e-5 {
				t.Errorf("wilson(%v, %d, %v) = [%.4f, %.4f], want [%.4f, %.4f]", tt.successes, tt.n, tt.z, lo, hi, tt.lo, tt.hi)
			}
			if lo < 0 || hi > 1 || lo > hi {
				t.Errorf("bounds [%v, %v] outside [0, 1]", lo, hi)
			}
			if (tt.successes == 0 && lo != 0) || (tt.successes == float64(tt.n) && hi != 1) {
				t.Errorf("bounds [%v, %v], want exactly 0 for 0/n and 1 for n/n", lo, hi)
			}
		})
	}
	if _, _, ok := wilson(0, 0, 1.96); ok {
		t.Error("wilson with n = 0: ok = true, want false")
	}
	// The spec's example: 50/100 → [40.4%, 59.6%].
	lo, hi, _ := wilson(50, 100, 1.96)
	if got := percent(lo) + " " + percent(hi); got != "40.4% 59.6%" {
		t.Errorf("50/100 = %s, want 40.4%% 59.6%%", got)
	}
}

func TestConfidenceLabel(t *testing.T) {
	for z, want := range map[float64]string{1.96: "95", 2.576: "99", 1: "68.3", 1.645: "90"} {
		if got := confidenceLabel(z); got != want {
			t.Errorf("confidenceLabel(%v) = %q, want %q", z, got, want)
		}
	}
}

func TestRunSummary_Text(t *testing.T) {
	a := &Arena{Referee: "ref", Games: 100, Threads: 4, Seed: 100, Swap: true, FaultPattern: "timeout",
		Players: []Player{
			{Name: "A", Cmd: "a", Wins: 61, Losses: 33, Ties: 4},
			{Name: "B", Cmd: "b", Wins: 33, Losses: 61, Ties: 4, Faults: 2},
		},
		DoubleForfeits: 1, Invalid: 1, InvalidReasons: map[string]int{"timeout": 1}, Decided: 98}
	want := `Summary: seeds 100..149 (50 maps, swap on, 100 games)
A: 61 W / 33 L / 4 D   win rate 64.3%  95% CI [54.4%, 73.1%]   faults 0
B: 33 W / 61 L / 4 D   win rate 35.7%  95% CI [26.9%, 45.6%]   faults 2
Double forfeits: 1   Invalid: 1 (timeout: 1)
`
	if got := a.PrintResults(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunSummary_NoResultsAndNoFaultPattern(t *testing.T) {
	z := 2.576
	a := &Arena{Referee: "ref", Games: 2, Threads: 1, Seed: 7, Z: &z,
		Players:        []Player{{Name: "A"}, {Name: "B"}},
		DoubleForfeits: 2, InvalidReasons: map[string]int{}}
	want := `Summary: seeds 7..8 (2 maps, swap off, 2 games)
A: 0 W / 0 L / 0 D   win rate n/a  99% CI n/a
B: 0 W / 0 L / 0 D   win rate n/a  99% CI n/a
Double forfeits: 2   Invalid: 0
`
	if got := a.PrintResults(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestRun_ThreePlayerSummary checks that each of three players gets a win
// rate and CI against the field: seat 0 always wins, so each player wins the
// 2 of 6 seatings in which it sits first.
func TestRun_ThreePlayerSummary(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3; echo 1")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-p3", "c", "-n", "6", "-s", "-i", "1", "-l", "off")
	if code != exitOK {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	for _, name := range []string{"Player1", "Player2", "Player3"} {
		want := name + ": 2 W / 4 L / 0 D   win rate 33.3%  95% CI [9.7%, 70.0%]\n"
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRun_SummaryFiles(t *testing.T) {
	// Game 0: A wins; 1: draw; 2: double forfeit; 3: invalid (exit 2).
	ref := fakeReferee(t, seedCase(`0) echo 5; echo 3 ;;
1) echo 4; echo 4 ;;
2) echo -1; echo -1 ;;
*) exit 2 ;;`))
	dir := t.TempDir()
	md, htmlPath, js := filepath.Join(dir, "s.md"), filepath.Join(dir, "s.html"), filepath.Join(dir, "s.json")
	before := time.Now().UTC().Truncate(time.Second)
	code, out := runCapture(t, "-r", ref, "-p1", "./a", "-pn1", "A<b>", "-p2", "./b", "-pn2", "B|x",
		"-n", "4", "-t", "2", "-i", "10", "-l", "off", "-md", md, "-html", htmlPath, "-summary-json", js)
	if code != exitInvalid {
		t.Fatalf("exit = %d, want %d\n%s", code, exitInvalid, out)
	}
	wantLines := []string{
		"Summary: seeds 10..13 (4 maps, swap off, 4 games)\n",
		"A<b>: 1 W / 0 L / 1 D   win rate 75.0%  95% CI [19.8%, 97.3%]\n",
		"B|x: 0 W / 1 L / 1 D   win rate 25.0%  95% CI [2.7%, 80.2%]\n",
		"Double forfeits: 1   Invalid: 1 (exit_status_2: 1)\n",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	data, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| Bot | W | L | D | Win rate | 95% CI | Faults |\n",
		"| A<b> | 1 | 0 | 1 | 75.0% | [19.8%, 97.3%] | n/a |\n",
		"| B\\|x | 0 | 1 | 1 | 25.0% | [2.7%, 80.2%] | n/a |\n",
		"| 1 | 1 (exit_status_2: 1) |\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("Markdown missing %q:\n%s", want, data)
		}
	}

	data, err = os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<th>D</th>", "<th>95% CI</th>",
		"<tr><td>A&lt;b&gt;</td><td>1</td><td>0</td><td>1</td><td>75.0%</td><td>[19.8%, 97.3%]</td><td>n/a</td></tr>",
		"<tr><td>1</td><td>1 (exit_status_2: 1)</td></tr>",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("HTML missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "A<b>") {
		t.Errorf("HTML doesn't escape player names:\n%s", data)
	}

	data, err = os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"date", "cgarena", "referee", "seed_from", "seed_to", "swap", "games", "threads",
		"players", "double_forfeits", "invalid", "invalid_reasons", "interrupted", "results_file"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON summary missing %q:\n%s", key, data)
		}
	}
	var s RunSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	date, err := time.Parse(time.RFC3339, s.Date)
	if err != nil || date.Before(before) || !strings.HasSuffix(s.Date, "Z") {
		t.Errorf("date = %q (%v), want UTC RFC 3339 not before %v", s.Date, err, before)
	}
	if s.Cgarena != VERSION || s.Referee != ref || s.SeedFrom != 10 || s.SeedTo != 13 || s.Swap ||
		s.Games != 4 || s.Threads != 2 || s.DoubleForfeits != 1 || s.Invalid != 1 ||
		s.InvalidReasons["exit_status_2"] != 1 || s.Interrupted || s.ResultsFile != nil {
		t.Errorf("unexpected JSON summary:\n%s", data)
	}
	if len(s.Players) != 2 {
		t.Fatalf("players = %+v", s.Players)
	}
	a := s.Players[0]
	if a.Name != "A<b>" || a.Cmd != "./a" || a.W != 1 || a.L != 0 || a.D != 1 || a.Commit != nil || a.Dirty != nil ||
		a.Faults != nil || a.WinRate == nil || *a.WinRate != 0.75 || a.CI == nil ||
		math.Abs(a.CI[0]-0.1979) > 5e-4 || math.Abs(a.CI[1]-0.9733) > 5e-4 {
		t.Errorf("player A = %+v", a)
	}
	for _, key := range []string{`"commit": null`, `"dirty": null`, `"faults": null`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("JSON summary missing %s:\n%s", key, data)
		}
	}
}

func TestRun_SummaryJSONNoResultsAndFaults(t *testing.T) {
	ref := jsonReferee(t, `*) echo '{"scores":{"0":-1,"1":-1},"tooltips":[{"text":"$0 timeout!"}]}' > "$l"; echo -1; echo -1 ;;`)
	dir := t.TempDir()
	js := filepath.Join(t.TempDir(), "s.json")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-i", "10", "-l", dir,
		"-fault-pattern", "timeout", "-summary-json", js)
	if code != exitInvalid {
		t.Fatalf("exit = %d, want %d\n%s", code, exitInvalid, out)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var s RunSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	p := s.Players[0]
	if p.WinRate != nil || p.CI != nil || p.Faults == nil || *p.Faults != 1 || *s.Players[1].Faults != 0 {
		t.Errorf("players = %s", data)
	}
	if s.ResultsFile == nil || *s.ResultsFile != filepath.Join(dir, "games.jsonl") {
		t.Errorf("results_file = %v, want %s", s.ResultsFile, filepath.Join(dir, "games.jsonl"))
	}
	if !strings.Contains(string(data), `"win_rate": null`) || !strings.Contains(string(data), `"ci": null`) {
		t.Errorf("win_rate/ci not null:\n%s", data)
	}
	if !strings.Contains(out, "win rate n/a  95% CI n/a   faults 1\n") {
		t.Errorf("output:\n%s", out)
	}
}

// TestRun_InterruptedWritesSummaryJSON checks that an interrupted run writes
// the JSON summary (with "interrupted": true) but no Markdown summary.
func TestRun_InterruptedWritesSummaryJSON(t *testing.T) {
	oldNotify, oldStop := notifySignals, stopSignals
	t.Cleanup(func() { notifySignals, stopSignals = oldNotify, oldStop })
	notifySignals = func(c chan<- os.Signal) { c <- syscall.SIGINT }
	stopSignals = func(chan<- os.Signal) {}

	dir := t.TempDir()
	js, md := filepath.Join(dir, "s.json"), filepath.Join(dir, "s.md")
	ref := fakeReferee(t, "exec sleep 60")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-i", "5", "-l", "off",
		"-summary-json", js, "-md", md)
	if code != exitSignal {
		t.Fatalf("exit = %d, want %d\n%s", code, exitSignal, out)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var s RunSummary
	if err := json.Unmarshal(data, &s); err != nil || !s.Interrupted || s.Games != 2 {
		t.Errorf("summary = %s (%v), want interrupted with games 2", data, err)
	}
	if _, err := os.Stat(md); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Markdown summary written for an interrupted run")
	}
	if !strings.Contains(out, "Summary: seeds 5..6 (2 maps, swap off, 0 of 2 games finished)\n") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRun_ZValidation(t *testing.T) {
	valid := fakeReferee(t, "echo 5; echo 3")
	config := func(body string) string {
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := []string{"-r", valid, "-p1", "a", "-p2", "b", "-i", "1", "-l", "off"}
	tests := []struct {
		name  string
		extra []string
		want  int
		label string
	}{
		{"z 0", []string{"-z", "0"}, exitUsage, ""},
		{"negative z", []string{"-z", "-1"}, exitUsage, ""},
		{"z NaN", []string{"-z", "NaN"}, exitUsage, ""},
		{"z 2.576", []string{"-z", "2.576"}, exitOK, "99% CI ["},
		{"config z", []string{"-config", config(`{"z": 2.576}`)}, exitOK, "99% CI ["},
		{"config z 0", []string{"-config", config(`{"z": 0}`)}, exitUsage, ""},
		{"default", nil, exitOK, "95% CI ["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runCapture(t, append(append([]string(nil), base...), tt.extra...)...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d\n%s", code, tt.want, out)
			}
			if tt.label != "" && !strings.Contains(out, tt.label) {
				t.Errorf("output missing %q:\n%s", tt.label, out)
			}
		})
	}
}
