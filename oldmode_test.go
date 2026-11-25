package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ackBot answers every input line with "ack <line>" until its stdin closes.
const ackBot = `while read l; do echo "ack $l"; done`

// oldRefereeScript is a fake old referee for two players: it checks the
// ###Seed/###Start lines, sends each player one line and reads one answer,
// then ends with ranking (or "1 0" if the handshake or answers were wrong,
// and with "BAD" if the seed line was wrong).
func oldRefereeScript(ranking string) string {
	return `read seed; read start
[ "$start" = "###Start 2" ] || { echo "###End 1 0"; exit 0; }
case "$seed" in "###Seed "*) ;; *) echo "###End 1 0"; exit 0 ;; esac
echo "ref debug" >&2
echo "###Input 0"; echo "hello 0"
echo "###Output 0 1"; read a
echo "###Input 1"; echo "hello 1"
echo "###Output 1 1"; read b
[ "$a" = "ack hello 0" ] && [ "$b" = "ack hello 1" ] || { echo "###End 1 0"; exit 0; }
echo "###End ` + ranking + `"`
}

func oldRef(t *testing.T, script string, bots ...string) *OldReferee {
	t.Helper()
	seats := make([]Seat, len(bots))
	for i, body := range bots {
		seats[i] = Seat{Index: i, Name: string(rune('A' + i)), Cmd: fakeReferee(t, body)}
	}
	return &OldReferee{Command: fakeReferee(t, script), Seats: seats, Seed: 1234}
}

func TestOldReferee_Outcomes(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		reason   string
		outcomes []string
	}{
		{name: "seat 0 wins", script: oldRefereeScript("0 1"), outcomes: []string{OutcomeWin, OutcomeLoss}},
		{name: "seat 1 wins", script: oldRefereeScript("1 0"), outcomes: []string{OutcomeLoss, OutcomeWin}},
		{name: "draw", script: oldRefereeScript("01"), outcomes: []string{OutcomeDraw, OutcomeDraw}},
		{name: "draw written 10", script: oldRefereeScript("10"), outcomes: []string{OutcomeDraw, OutcomeDraw}},
		{name: "player ranked twice", script: oldRefereeScript("0 0"), reason: ReasonProtocol},
		{name: "player missing", script: oldRefereeScript("0"), reason: ReasonProtocol},
		{name: "index out of range", script: oldRefereeScript("0 2"), reason: ReasonProtocol},
		{name: "empty ranking", script: oldRefereeScript(""), reason: ReasonProtocol},
		{name: "unknown command", script: `read s; read s; echo "###Bogus"`, reason: ReasonProtocol},
		{name: "bad input index", script: `read s; read s; echo "###Input 7"`, reason: ReasonProtocol},
		{name: "bad output count", script: `read s; read s; echo "###Output 0 x"`, reason: ReasonProtocol},
		{name: "no end", script: `read s; read s; echo "###Input 0"; echo hi`, reason: ReasonNoEnd},
		{name: "exit status", script: `read s; read s; exit 3`, reason: "exit_status_3"},
		{name: "ignored lines and errors", script: `read s; read s; echo "noise"; echo "###Error 0 oops"; echo "###End 1 0"`,
			outcomes: []string{OutcomeLoss, OutcomeWin}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := oldRef(t, tt.script, ackBot, ackBot)
			res, err := ref.Run(0)
			if tt.reason != "" {
				if err == nil || res.InvalidReason != tt.reason {
					t.Fatalf("reason = %q (err %v), want %q", res.InvalidReason, err, tt.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if strings.Join(res.Outcomes, ",") != strings.Join(tt.outcomes, ",") {
				t.Errorf("outcomes = %v, want %v", res.Outcomes, tt.outcomes)
			}
			if res.Seed != 1234 {
				t.Errorf("seed = %d, want 1234", res.Seed)
			}
		})
	}
}

func TestOldReferee_ThreePlayers(t *testing.T) {
	script := `read s; read s; [ "$s" = "###Start 3" ] || exit 9; echo "###End 1 02"`
	res, err := oldRef(t, script, ackBot, ackBot, ackBot).Run(0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{OutcomeLoss, OutcomeWin, OutcomeLoss}
	if strings.Join(res.Outcomes, ",") != strings.Join(want, ",") {
		t.Errorf("outcomes = %v, want %v", res.Outcomes, want)
	}
	if res.Ranking != "1 02" {
		t.Errorf("ranking = %q", res.Ranking)
	}
	if got := sprintInts(res.Scores); got != "1,2,1" {
		t.Errorf("scores = %s, want 1,2,1", got)
	}
}

// sprintInts joins ints with commas.
func sprintInts(v []int) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}

func TestOldReferee_NoSeedWhenZero(t *testing.T) {
	script := `read s; [ "$s" = "###Start 2" ] && echo "###End 0 1" || echo "###End 1 0"`
	ref := oldRef(t, script, ackBot, ackBot)
	ref.Seed = 0
	res, err := ref.Run(0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0] != OutcomeWin {
		t.Errorf("outcomes = %v: ###Start wasn't the first line", res.Outcomes)
	}
}

func TestOldReferee_Crashes(t *testing.T) {
	twoTurns := `read s; read s
echo "###Input 0"; echo x; echo "###Output 0 1"; read a
echo "###Input 1"; echo x; echo "###Output 1 1"; read b
echo "###End 0 1"`
	threeTurns := `read s; read s
for p in 0 1 2; do echo "###Input $p"; echo x; echo "###Output $p 1"; read a; done
echo "###End 0 1 2"`
	tests := []struct {
		name     string
		script   string
		bots     []string
		outcomes []string
		faults   []bool
		reason   string // substring of EndReason
	}{
		{name: "seat 1 exits at once", script: twoTurns, bots: []string{ackBot, "exit 0"},
			outcomes: []string{OutcomeWin, OutcomeLoss}, faults: []bool{false, true}, reason: "player 1 crashed"},
		{name: "seat 0 writes nothing", script: twoTurns, bots: []string{"read l", ackBot},
			outcomes: []string{OutcomeLoss, OutcomeWin}, faults: []bool{true, false}, reason: "stdout closed after 0 of 1"},
		{name: "three players, survivors draw", script: threeTurns, bots: []string{ackBot, "exit 1", ackBot},
			outcomes: []string{OutcomeDraw, OutcomeLoss, OutcomeDraw}, faults: []bool{false, true, false}, reason: "player 1 crashed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := oldRef(t, tt.script, tt.bots...).Run(0)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if strings.Join(res.Outcomes, ",") != strings.Join(tt.outcomes, ",") {
				t.Errorf("outcomes = %v, want %v", res.Outcomes, tt.outcomes)
			}
			for i := range tt.faults {
				if res.Faults[i] != tt.faults[i] {
					t.Errorf("faults = %v, want %v", res.Faults, tt.faults)
					break
				}
			}
			if !strings.Contains(res.EndReason, tt.reason) {
				t.Errorf("end reason = %q, want it to contain %q", res.EndReason, tt.reason)
			}
		})
	}
}

func TestOldReferee_BotFailsToStart(t *testing.T) {
	ref := oldRef(t, oldRefereeScript("0 1"), ackBot, ackBot)
	ref.Seats[1].Cmd = filepath.Join(t.TempDir(), "missing-bot")
	res, err := ref.Run(0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcomes[0] != OutcomeWin || res.Outcomes[1] != OutcomeLoss || !res.Faults[1] {
		t.Errorf("outcomes = %v, faults = %v", res.Outcomes, res.Faults)
	}
	if !strings.Contains(res.EndReason, "failed to start") {
		t.Errorf("end reason = %q", res.EndReason)
	}

	ref.Seats[0].Cmd = ref.Seats[1].Cmd
	res, err = ref.Run(0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsDoubleForfeit() {
		t.Errorf("both bots missing: outcomes = %v, want double forfeits", res.Outcomes)
	}
}

func TestOldReferee_RefereeFailsToStart(t *testing.T) {
	ref := oldRef(t, "", ackBot, ackBot)
	ref.Command = "   "
	res, err := ref.Run(0)
	if err == nil || res.InvalidReason != ReasonStartFailed {
		t.Errorf("reason = %q (err %v), want %q", res.InvalidReason, err, ReasonStartFailed)
	}
}

func TestOldReferee_TimeoutKillsHangingBot(t *testing.T) {
	script := `read s; read s; echo "###Output 0 1"; read a; echo "###End 0 1"`
	ref := oldRef(t, script, "exec sleep 30", ackBot)
	ref.Timeout = 500 * time.Millisecond
	start := time.Now()
	res, err := ref.Run(0)
	if err == nil || res.InvalidReason != ReasonTimeout {
		t.Fatalf("reason = %q (err %v), want %q", res.InvalidReason, err, ReasonTimeout)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestOldReferee_LingeringBotDoesNotHang(t *testing.T) {
	// The bots ignore stdin closing after ###End; they are killed after waitDelay.
	script := `read s; read s; echo "###End 0 1"`
	ref := oldRef(t, script, "exec sleep 30", "exec sleep 30")
	start := time.Now()
	if _, err := ref.Run(0); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestOldReferee_CancelIsInterrupted(t *testing.T) {
	script := `read s; read s; echo "###Output 0 1"; read a; echo "###End 0 1"`
	ref := oldRef(t, script, "exec sleep 30", ackBot)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	res, err := ref.RunContext(ctx, 0)
	if err != ErrInterrupted || !res.Interrupted || !res.Killed {
		t.Errorf("err = %v, interrupted = %v, killed = %v", err, res.Interrupted, res.Killed)
	}
}

func TestOldReferee_Logs(t *testing.T) {
	dir := t.TempDir()
	ref := oldRef(t, oldRefereeScript("0 1"), `echo "bot err" >&2; `+ackBot, ackBot)
	ref.LogOutput = dir
	res, err := ref.Run(7)
	if err != nil {
		t.Fatal(err)
	}
	if res.LogFile != filepath.Join(dir, "match_7.log") {
		t.Errorf("log file = %q", res.LogFile)
	}
	transcript, err := os.ReadFile(filepath.Join(dir, "match_7.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"To referee: ###Seed 1234", "To referee: ###Start 2", "Referee: ###Input 0",
		"Referee: hello 0", "Player 0: ack hello 0", "To referee: ack hello 0", "Referee: ###End 0 1"} {
		if !strings.Contains(string(transcript), want+"\n") {
			t.Errorf("transcript missing %q:\n%s", want, transcript)
		}
	}
	stderr, err := os.ReadFile(filepath.Join(dir, "match_7.err"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Referee: ref debug\n", "Player 0: bot err\n"} {
		if !strings.Contains(string(stderr), want) {
			t.Errorf("stderr file missing %q:\n%s", want, stderr)
		}
	}
}

func TestRankingScores(t *testing.T) {
	tests := []struct {
		ranking string
		n       int
		want    string // scores, or "error"
	}{
		{"0 2 3 1", 4, "4,1,3,2"},
		{"1 0", 2, "1,2"},
		{"01", 2, "1,1"},
		{"1 03 2", 4, "2,3,1,2"},
		{"02 31", 4, "2,1,2,1"},
		{"0 1 1", 2, "error"},
		{"0 x", 2, "error"},
		{"", 2, "error"},
	}
	for _, tt := range tests {
		scores, err := rankingScores(strings.Fields(tt.ranking), tt.n)
		got := "error"
		if err == nil {
			got = sprintInts(scores)
		}
		if got != tt.want {
			t.Errorf("rankingScores(%q, %d) = %s, want %s", tt.ranking, tt.n, got, tt.want)
		}
	}
}

func TestRun_OldModeSwap(t *testing.T) {
	// Seat 0 always wins, so with swap each player wins half its games.
	dir := filepath.Join(t.TempDir(), "logs")
	ref := fakeReferee(t, oldRefereeScript("0 1"))
	bot := fakeReferee(t, ackBot)
	jsonPath := filepath.Join(t.TempDir(), "summary.json")
	code, out := runCapture(t, "-o", "-r", ref, "-p1", bot, "-p2", bot, "-pn1", "A", "-pn2", "B",
		"-n", "4", "-s", "-t", "2", "-i", "50", "-l", dir, "-summary-json", jsonPath)
	if code != exitOK {
		t.Fatalf("exit code = %d:\n%s", code, out)
	}
	for _, want := range []string{"A: 2 W / 2 L / 0 D", "B: 2 W / 2 L / 0 D", "faults 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	_, recs := readJSONL(t, filepath.Join(dir, "games.jsonl"))
	if len(recs) != 4 {
		t.Fatalf("%d records, want 4", len(recs))
	}
	for _, rec := range recs {
		if rec.Ranking != "0 1" || rec.Outcome[rec.Seats[0]] != OutcomeWin || !strings.HasSuffix(rec.Log, ".log") {
			t.Errorf("record %+v", rec)
		}
	}
	summary, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), `"old_mode": true`) {
		t.Errorf("summary JSON missing old_mode:\n%s", summary)
	}

	// The log dir now holds match_*.log files: refused without -overwrite.
	code, out = runCapture(t, "-o", "-r", ref, "-p1", bot, "-p2", bot, "-pn1", "A", "-pn2", "B", "-l", dir)
	if code != exitUsage || !strings.Contains(out, "already contains") {
		t.Errorf("rerun: exit code = %d:\n%s", code, out)
	}
}

func TestRun_OldModeCrashIsFault(t *testing.T) {
	ref := fakeReferee(t, oldRefereeScript("0 1"))
	code, out := runCapture(t, "-o", "-r", ref, "-p1", fakeReferee(t, ackBot), "-p2", filepath.Join(t.TempDir(), "nope"),
		"-pn1", "A", "-pn2", "B", "-n", "2")
	if code != exitOK {
		t.Fatalf("exit code = %d:\n%s", code, out)
	}
	for _, want := range []string{"A: 2 W / 0 L / 0 D", "B: 0 W / 2 L / 0 D", "faults 2",
		"Warning: B faulted in all 2 games"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRun_OldModeFromConfig(t *testing.T) {
	ref := fakeReferee(t, oldRefereeScript("1 0"))
	bot := fakeReferee(t, ackBot)
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfg, []byte(`{"old_mode": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCapture(t, "-config", cfg, "-r", ref, "-p1", bot, "-p2", bot, "-pn1", "A", "-pn2", "B")
	if code != exitOK || !strings.Contains(out, "B: 1 W / 0 L") {
		t.Errorf("exit code = %d:\n%s", code, out)
	}
	// An explicit -o=false beats the config: the old referee prints no scores.
	code, out = runCapture(t, "-config", cfg, "-o=false", "-r", ref, "-p1", bot, "-p2", bot, "-pn1", "A", "-pn2", "B")
	if code != exitInvalid {
		t.Errorf("-o=false: exit code = %d:\n%s", code, out)
	}
}

func TestRun_OldModeFaultPatternIsUsageError(t *testing.T) {
	code, out := runCapture(t, "-o", "-r", "ref", "-p1", "a", "-p2", "b", "-l", t.TempDir(), "-fault-pattern", "x")
	if code != exitUsage || !strings.Contains(out, "Error: fault pattern (-fault-pattern) can't be used with old mode") {
		t.Errorf("exit code = %d:\n%s", code, out)
	}
}

const (
	OldTicTacToeReferee = "python3 test-bots/old_tictactoe_referee.py"
	OldTicTacToeBot     = "python3 test-bots/old_tictactoe_bot.py"
)

// TestRun_OldModeTicTacToe plays a real old-style referee written in Python.
// Both bots play the first free cell, so the first player always wins: with
// swap, each wins one game.
func TestRun_OldModeTicTacToe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	code, out := runCapture(t, "-o", "-r", OldTicTacToeReferee, "-p1", OldTicTacToeBot, "-p2", OldTicTacToeBot,
		"-pn1", "A", "-pn2", "B", "-n", "2", "-s", "-t", "2", "-l", dir)
	if code != exitOK {
		t.Fatalf("exit code = %d:\n%s", code, out)
	}
	for _, want := range []string{"A: 1 W / 1 L / 0 D", "B: 1 W / 1 L / 0 D"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	transcript, err := os.ReadFile(filepath.Join(dir, "match_0.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "Referee: ###End 0 1\n") {
		t.Errorf("transcript:\n%s", transcript)
	}
}
