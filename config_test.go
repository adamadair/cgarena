package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes cfg as a JSON config file and returns its path.
func writeConfig(t *testing.T, cfg map[string]any) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runSummary runs cgarena with args plus -summary-json and returns the exit
// code, stdout and the summary (zero if none was written).
func runSummary(t *testing.T, args ...string) (int, string, RunSummary) {
	t.Helper()
	js := filepath.Join(t.TempDir(), "summary.json")
	code, out := runCapture(t, append(args, "-summary-json", js)...)
	var s RunSummary
	if data, err := os.ReadFile(js); err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
	}
	return code, out, s
}

func TestRun_ConfigWithoutFlags(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	cfg := writeConfig(t, map[string]any{
		"referee": ref,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
		"games":   4, "threads": 2, "seed": 7, "swap": true, "z": 2.576, "log_output": "off",
	})
	code, out, s := runSummary(t, "-config", cfg)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, out)
	}
	if s.Referee != ref || s.Games != 4 || s.Threads != 2 || s.SeedFrom != 7 || s.SeedTo != 8 || !s.Swap ||
		s.Z != 2.576 || len(s.Players) != 2 || s.Players[0].Name != "A" || s.Players[1].Cmd != "b" {
		t.Errorf("summary = %+v", s)
	}
}

func TestRun_ExplicitFlagsOverrideConfig(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	other := fakeReferee(t, "echo 1; echo 2")
	cfg := writeConfig(t, map[string]any{
		"referee": ref,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
		"games":   4, "threads": 2, "seed": 7, "swap": true, "z": 2.576, "log_output": "off",
	})
	code, out, s := runSummary(t, "-config", cfg, "-r", other, "-n", "3", "-t", "1", "-i", "20",
		"-s=false", "-z", "1.96", "-p2", "b2", "-pn2", "B2")
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, out)
	}
	if s.Referee != other || s.Games != 3 || s.Threads != 1 || s.SeedFrom != 20 || s.SeedTo != 22 || s.Swap ||
		s.Z != 1.96 {
		t.Errorf("summary = %+v", s)
	}
	if len(s.Players) != 2 || s.Players[0].Name != "A" || s.Players[0].Cmd != "a" ||
		s.Players[1].Name != "B2" || s.Players[1].Cmd != "b2" {
		t.Errorf("players = %+v", s.Players)
	}
	// The other referee makes seat 2 win, so the results follow -r.
	if s.Players[1].W != 3 {
		t.Errorf("B2 wins = %d, want 3 (games played by -r)", s.Players[1].W)
	}
}

func TestRun_ConfigCanSetFalseAndZero(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	// swap false and seed 0 (random) in the config override the flag
	// defaults; games 0 is a usage error rather than being ignored.
	cfg := writeConfig(t, map[string]any{
		"referee": ref,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
		"swap":    false, "seed": 0, "games": 3,
	})
	code, out := runCapture(t, "-config", cfg)
	if code != exitOK || !strings.Contains(out, "swap off, 3 games); random base seed") {
		t.Errorf("exit = %d\n%s", code, out)
	}
	zero := writeConfig(t, map[string]any{"referee": ref, "games": 0})
	if code, out := runCapture(t, "-config", zero, "-p1", "a", "-p2", "b"); code != exitUsage {
		t.Errorf("config games 0: exit = %d, want %d\n%s", code, exitUsage, out)
	}
	if code, out := runCapture(t, "-config", zero, "-p1", "a", "-p2", "b", "-n", "1"); code != exitOK {
		t.Errorf("config games 0 with -n 1: exit = %d, want %d\n%s", code, exitOK, out)
	}
}

func TestRun_ConfigTimeoutAndFlagOverride(t *testing.T) {
	slow := fakeReferee(t, "sleep 0.3; echo 5; echo 3")
	short := writeConfig(t, map[string]any{"referee": slow, "timeout": "100ms"})
	off := writeConfig(t, map[string]any{"referee": slow, "timeout": "0s"})
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"config 100ms", []string{"-config", short}, exitInvalid},
		{"-timeout 0 overrides config 100ms", []string{"-config", short, "-timeout", "0"}, exitOK},
		{"config 0s disables", []string{"-config", off}, exitOK},
		{"-timeout 100ms overrides config 0s", []string{"-config", off, "-timeout", "100ms"}, exitInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"-p1", "a", "-p2", "b", "-l", "off"}, tt.args...)
			if got := run(args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", args, got, tt.want)
			}
		})
	}
}

func TestRun_ConfigLogDirAndFlagOverride(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	logDir := filepath.Join(t.TempDir(), "logs")
	cfg := writeConfig(t, map[string]any{
		"referee": ref, "log_output": logDir, "json_check": false,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
	})
	if got := run([]string{"-config", cfg, "-l", "off"}); got != exitOK {
		t.Fatalf("exit = %d", got)
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("-l off didn't override the config log_output: %v", err)
	}
	if got := run([]string{"-config", cfg}); got != exitOK {
		t.Fatalf("exit = %d", got)
	}
	if _, err := os.Stat(filepath.Join(logDir, "games.jsonl")); err != nil {
		t.Errorf("config log_output not used: %v", err)
	}
	// The second run into the same directory needs -overwrite; the config
	// says false, the flag says true.
	cfgNo := writeConfig(t, map[string]any{
		"referee": ref, "log_output": logDir, "json_check": false, "overwrite": false,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
	})
	if code, out := runCapture(t, "-config", cfgNo); code != exitUsage || !strings.HasPrefix(out, "Error: log directory") {
		t.Errorf("exit = %d, want %d with an Error: line\n%s", code, exitUsage, out)
	}
	if got := run([]string{"-config", cfgNo, "-overwrite"}); got != exitOK {
		t.Errorf("-overwrite didn't override config overwrite false: exit = %d", got)
	}
}

func TestRun_PlayerFlagsWithConfigPlayers(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3; echo 1")
	cfg := writeConfig(t, map[string]any{
		"referee": ref,
		"players": []map[string]string{{"name": "A", "cmd": "a"}, {"name": "B", "cmd": "b"}},
	})
	code, out, s := runSummary(t, "-config", cfg, "-p3", "c", "-pn3", "C", "-pn1", "A1")
	if code != exitOK {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	if len(s.Players) != 3 || s.Players[0].Name != "A1" || s.Players[0].Cmd != "a" ||
		s.Players[2].Name != "C" || s.Players[2].Cmd != "c" {
		t.Errorf("players = %+v", s.Players)
	}
	for _, args := range [][]string{
		{"-pn3", "C"},
		{"-pc3", t.TempDir()},
		{"-p4", "d"},
	} {
		code, out := runCapture(t, append([]string{"-config", cfg}, args...)...)
		if code != exitUsage || !strings.Contains(out, "Error: "+args[0]+" is given but there is no player") {
			t.Errorf("%v: exit = %d, want %d with an error\n%s", args, code, exitUsage, out)
		}
	}
}

func TestRun_OutFileExistsIsUsageError(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	out := filepath.Join(t.TempDir(), "games.jsonl")
	if err := os.WriteFile(out, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-out", out)
	if code != exitUsage || !strings.HasPrefix(stdout, "Error: results file (-out)") {
		t.Errorf("exit = %d, want %d with an Error: line\n%s", code, exitUsage, stdout)
	}
}

func TestReferee_CommandSplitOnWhitespace(t *testing.T) {
	ref := fakeReferee(t, `[ "$1" = "x" ] && [ "$2" = "-p1" ] || exit 2
echo 5; echo 3`)
	r := Referee{Command: "  " + ref + " \t x  ", Seats: seatsOf(twoPlayers())}
	if _, err := r.Run(0); err != nil {
		t.Errorf("Run: %v", err)
	}
	r.Command = "   "
	result, err := r.Run(0)
	if err == nil || result.InvalidReason != ReasonStartFailed {
		t.Errorf("blank command: reason %q, err %v; want %s", result.InvalidReason, err, ReasonStartFailed)
	}
}
