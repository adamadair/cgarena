package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReferee writes an executable sh script with the given body into a temp
// dir and returns its path, usable as a referee command. The script receives
// the usual referee arguments (-p1 ..., -seed ...) in "$@".
func fakeReferee(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "referee.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// echoSeed is a script fragment that prints "seed=<value of -seed>" if given.
const echoSeed = `while [ $# -gt 0 ]; do [ "$1" = "-seed" ] && echo "seed=$2"; shift; done`

func twoPlayers() []Player {
	return []Player{{Name: "A", Cmd: "botA"}, {Name: "B", Cmd: "botB"}}
}

func TestReferee_Outcomes(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		seed     int
		reason   string   // expected InvalidReason ("" for a valid game)
		outcomes []string // expected per-seat outcomes
		wlt      [][3]int // expected per-seat W/L/T
	}{
		{
			name:     "win",
			script:   "echo 5; echo 3; " + echoSeed,
			seed:     100,
			outcomes: []string{OutcomeWin, OutcomeLoss},
			wlt:      [][3]int{{1, 0, 0}, {0, 1, 0}},
		},
		{
			name:     "loss for seat 1 without trailing newline",
			script:   `printf '0\n7'`,
			outcomes: []string{OutcomeLoss, OutcomeWin},
			wlt:      [][3]int{{0, 1, 0}, {1, 0, 0}},
		},
		{
			name:     "draw",
			script:   "echo 4; echo 4",
			outcomes: []string{OutcomeDraw, OutcomeDraw},
			wlt:      [][3]int{{0, 0, 1}, {0, 0, 1}},
		},
		{
			name:     "draw at zero",
			script:   "echo 0; echo 0",
			outcomes: []string{OutcomeDraw, OutcomeDraw},
			wlt:      [][3]int{{0, 0, 1}, {0, 0, 1}},
		},
		{
			name:     "double forfeit",
			script:   "echo -1; echo -1",
			outcomes: []string{OutcomeDoubleForfeit, OutcomeDoubleForfeit},
			wlt:      [][3]int{{0, 0, 0}, {0, 0, 0}},
		},
		{
			name:     "one negative score is a normal loss",
			script:   "echo -1; echo 0",
			outcomes: []string{OutcomeLoss, OutcomeWin},
			wlt:      [][3]int{{0, 1, 0}, {1, 0, 0}},
		},
		{name: "one score line", script: "echo 5", reason: ReasonScoreCount},
		{name: "three score lines", script: "echo 5; echo 3; echo 1", reason: ReasonScoreCount},
		{name: "no score lines", script: "echo hello", reason: ReasonScoreCount},
		{name: "null scores", script: "echo null; echo null", reason: ReasonScoreCount},
		{name: "exit 2", script: "echo 5; echo 3; exit 2", reason: "exit_status_2"},
		{name: "seed mismatch", script: "echo 5; echo 3; echo seed=999", seed: 100, reason: ReasonSeedMismatch},
		{name: "seed line without a sent seed is accepted", script: "echo 5; echo 3; echo seed=999",
			outcomes: []string{OutcomeWin, OutcomeLoss}, wlt: [][3]int{{1, 0, 0}, {0, 1, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := Referee{Command: fakeReferee(t, tt.script), Seats: seatsOf(twoPlayers()), Seed: tt.seed, LogOutput: "off"}
			result, err := ref.Run(0)
			if result == nil {
				t.Fatal("Run returned a nil result")
			}
			_ = result.String() // must not panic for any outcome
			if tt.reason != "" {
				checkInvalid(t, result, err, tt.reason)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for i, want := range tt.outcomes {
				if result.Outcomes[i] != want {
					t.Errorf("seat %d outcome = %q, want %q", i, result.Outcomes[i], want)
				}
				p := result.Players[i]
				if got := [3]int{p.Wins, p.Losses, p.Ties}; got != tt.wlt[i] {
					t.Errorf("seat %d W/L/T = %v, want %v", i, got, tt.wlt[i])
				}
			}
		})
	}
}

func checkInvalid(t *testing.T, result *RefereeResult, err error, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an invalid game (%s), got no error", reason)
	}
	if result.InvalidReason != reason {
		t.Errorf("InvalidReason = %q, want %q (err: %v)", result.InvalidReason, reason, err)
	}
	for i, o := range result.Outcomes {
		if o != OutcomeInvalid {
			t.Errorf("seat %d outcome = %q, want invalid", i, o)
		}
	}
	for i, p := range result.Players {
		if p.Wins+p.Losses+p.Ties != 0 {
			t.Errorf("seat %d has W/L/T counts on an invalid game: %+v", i, p)
		}
	}
}

func TestReferee_StartFailed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-referee")
	for _, cmd := range []string{missing, ""} {
		ref := Referee{Command: cmd, Seats: seatsOf(twoPlayers()), LogOutput: "off"}
		result, err := ref.Run(0)
		checkInvalid(t, result, err, ReasonStartFailed)
	}
}

func TestProcessResults_ThreePlayers(t *testing.T) {
	players := []Player{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	rr := &RefereeResult{Players: players, Scores: []int{5, 5, 2}}
	rr.ProcessResults()
	want := []string{OutcomeDraw, OutcomeDraw, OutcomeLoss}
	for i := range want {
		if rr.Outcomes[i] != want[i] {
			t.Errorf("seat %d outcome = %q, want %q", i, rr.Outcomes[i], want[i])
		}
	}
}

func TestProcessResults_WrongScoreCountDoesNotPanic(t *testing.T) {
	rr := &RefereeResult{Players: twoPlayers(), Scores: []int{1}}
	rr.ProcessResults()
	if rr.InvalidReason != ReasonScoreCount {
		t.Errorf("InvalidReason = %q, want %q", rr.InvalidReason, ReasonScoreCount)
	}
	_ = rr.String()
}

func TestArena_CountsInvalidAndDoubleForfeits(t *testing.T) {
	// Game 0: -1/-1 (double forfeit); 1: A wins; 2: draw; 3: one score line; 4: exit 2.
	script := `while [ $# -gt 0 ]; do [ "$1" = "-seed" ] && g=$(($2 - 10)); shift; done
case $g in
0) echo -1; echo -1 ;;
1) echo 9; echo 1 ;;
2) echo 3; echo 3 ;;
3) echo 9 ;;
*) exit 2 ;;
esac`
	arena := Arena{Referee: fakeReferee(t, script), Players: twoPlayers(), Games: 5, Threads: 4, Seed: 10, LogOutput: "off"}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	a, b := arena.Players[0], arena.Players[1]
	if a.Wins != 1 || a.Losses != 0 || a.Ties != 1 || b.Wins != 0 || b.Losses != 1 || b.Ties != 1 {
		t.Errorf("unexpected tallies: A=%+v B=%+v", a, b)
	}
	if arena.DoubleForfeits != 1 {
		t.Errorf("DoubleForfeits = %d, want 1", arena.DoubleForfeits)
	}
	if arena.Decided != 2 {
		t.Errorf("Decided = %d, want 2", arena.Decided)
	}
	if arena.Invalid != 2 || arena.InvalidReasons[ReasonScoreCount] != 1 || arena.InvalidReasons["exit_status_2"] != 1 {
		t.Errorf("Invalid = %d, reasons = %v", arena.Invalid, arena.InvalidReasons)
	}
	summary := arena.PrintResults()
	if !strings.Contains(summary, "Double forfeits: 1   Invalid: 2 (exit_status_2: 1, score_count: 1)") {
		t.Errorf("summary missing totals:\n%s", summary)
	}
}

func TestRun_ExitCodes(t *testing.T) {
	valid := fakeReferee(t, "echo 5; echo 3")
	doubleForfeit := fakeReferee(t, "echo -1; echo -1")
	broken := fakeReferee(t, "echo null; echo null")
	base := func(ref string, extra ...string) []string {
		return append([]string{"-r", ref, "-p1", "a", "-p2", "b", "-l", "off"}, extra...)
	}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"all valid", base(valid, "-n", "3"), exitOK},
		{"only double forfeits is a no-result run", base(doubleForfeit, "-n", "2"), exitInvalid},
		{"invalid games", base(broken, "-n", "2"), exitInvalid},
		{"missing referee command", base(filepath.Join(t.TempDir(), "nope")), exitInvalid},
		{"zero threads", base(valid, "-t", "0"), exitUsage},
		{"zero games", base(valid, "-n", "0"), exitUsage},
		{"negative games", base(valid, "-n", "-1"), exitUsage},
		{"unknown flag", base(valid, "-bogus"), exitUsage},
		{"missing player", []string{"-r", valid, "-p1", "a", "-l", "off"}, exitUsage},
		{"version", []string{"-v"}, exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}
