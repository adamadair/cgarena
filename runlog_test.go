package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// gitRepo creates a git repository in a temp dir with one committed file and
// returns its directory and HEAD commit. It skips the test without git.
func gitRepo(t *testing.T) (dir, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=cgarena", "-c", "user.email=cgarena@example.com",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "bot.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "bot.txt")
	git("commit", "-q", "-m", "bot")
	return dir, git("rev-parse", "HEAD")
}

// readRunLog returns the run log's lines, each parsed as a RunSummary.
func readRunLog(t *testing.T, path string) ([]string, []RunSummary) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("run log doesn't end with a newline: %q", data)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	entries := make([]RunSummary, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &entries[i]); err != nil {
			t.Fatalf("run log line %d is not valid JSON: %v\n%s", i+1, err, line)
		}
	}
	return lines, entries
}

func TestRun_RunLogCommits(t *testing.T) {
	clean, cleanHead := gitRepo(t)
	dirty, dirtyHead := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dirty, "bot.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notRepo := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(notRepo))

	ref := fakeReferee(t, "echo 5; echo 3; echo 1; echo 0")
	runLog := filepath.Join(t.TempDir(), "runs.jsonl")
	js := filepath.Join(t.TempDir(), "s.json")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-p3", "c", "-p4", "d", "-i", "1", "-l", "off",
		"-pc1", clean, "-pc2", dirty, "-pc3", notRepo, "-runlog", runLog, "-summary-json", js)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, out)
	}
	if !strings.Contains(out, "Warning: can't read the git commit of Player3 from "+notRepo+", recording null: ") {
		t.Errorf("output missing the git warning for Player3:\n%s", out)
	}
	if strings.Count(out, "Warning: can't read the git commit") != 1 {
		t.Errorf("want exactly one git warning:\n%s", out)
	}
	lines, entries := readRunLog(t, runLog)
	if len(entries) != 1 {
		t.Fatalf("run log has %d lines, want 1:\n%s", len(entries), strings.Join(lines, "\n"))
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	var fromSummary RunSummary
	if err := json.Unmarshal(data, &fromSummary); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]RunSummary{"run log": entries[0], "summary JSON": fromSummary} {
		p := s.Players
		if len(p) != 4 {
			t.Fatalf("%s players = %+v", name, p)
		}
		if p[0].Commit == nil || *p[0].Commit != cleanHead || p[0].Dirty == nil || *p[0].Dirty {
			t.Errorf("%s: clean repo player = %+v, want commit %s, dirty false", name, p[0], cleanHead)
		}
		if p[1].Commit == nil || *p[1].Commit != dirtyHead || p[1].Dirty == nil || !*p[1].Dirty {
			t.Errorf("%s: dirty repo player = %+v, want commit %s, dirty true", name, p[1], dirtyHead)
		}
		for _, i := range []int{2, 3} { // not a repo; no -pc4
			if p[i].Commit != nil || p[i].Dirty != nil {
				t.Errorf("%s: player %d = %+v, want null commit and dirty", name, i+1, p[i])
			}
		}
	}
	if !strings.Contains(lines[0], `"commit":null,"dirty":null`) {
		t.Errorf("run log line has no null commit/dirty:\n%s", lines[0])
	}
}

func TestRun_RunLogAppends(t *testing.T) {
	valid := fakeReferee(t, "echo 5; echo 3")
	invalid := fakeReferee(t, "exit 2")
	runLog := filepath.Join(t.TempDir(), "runs.jsonl")
	base := []string{"-p1", "a", "-p2", "b", "-i", "1", "-l", "off", "-runlog", runLog}

	if code, out := runCapture(t, append([]string{"-r", valid}, base...)...); code != exitOK {
		t.Fatalf("first run: exit = %d\n%s", code, out)
	}
	if code, out := runCapture(t, append([]string{"-r", valid, "-z", "2.576", "-n", "2"}, base...)...); code != exitOK {
		t.Fatalf("second run: exit = %d\n%s", code, out)
	}
	if code, out := runCapture(t, append([]string{"-r", invalid}, base...)...); code != exitInvalid {
		t.Fatalf("invalid run: exit = %d, want %d\n%s", code, exitInvalid, out)
	}
	// Usage errors happen before any game: no run, no entry.
	if code, out := runCapture(t, append([]string{"-r", valid, "-n", "0"}, base...)...); code != exitUsage {
		t.Fatalf("usage error: exit = %d, want %d\n%s", code, exitUsage, out)
	}

	lines, entries := readRunLog(t, runLog)
	if len(entries) != 3 {
		t.Fatalf("run log has %d lines, want 3:\n%s", len(entries), strings.Join(lines, "\n"))
	}
	if entries[0].Games != 1 || entries[0].Z != defaultZ || entries[0].Invalid != 0 || entries[0].Players[0].W != 1 {
		t.Errorf("first entry = %s", lines[0])
	}
	if !strings.Contains(lines[0], `"z":1.96,`) {
		t.Errorf("first entry has no z 1.96: %s", lines[0])
	}
	if entries[1].Games != 2 || entries[1].Z != 2.576 || entries[1].Players[0].W != 2 {
		t.Errorf("second entry = %s", lines[1])
	}
	if entries[2].Invalid != 1 || entries[2].InvalidReasons["exit_status_2"] != 1 || entries[2].Interrupted {
		t.Errorf("invalid-run entry = %s", lines[2])
	}
	for i, e := range entries {
		if e.Players[0].Commit != nil || e.Players[0].Dirty != nil {
			t.Errorf("entry %d has a commit without -pc: %s", i+1, lines[i])
		}
	}
}

func TestRun_RunLogInterrupted(t *testing.T) {
	oldNotify, oldStop := notifySignals, stopSignals
	t.Cleanup(func() { notifySignals, stopSignals = oldNotify, oldStop })
	notifySignals = func(c chan<- os.Signal) { c <- syscall.SIGINT }
	stopSignals = func(chan<- os.Signal) {}

	runLog := filepath.Join(t.TempDir(), "runs.jsonl")
	ref := fakeReferee(t, "exec sleep 60")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-i", "5", "-l", "off", "-runlog", runLog)
	if code != exitSignal {
		t.Fatalf("exit = %d, want %d\n%s", code, exitSignal, out)
	}
	lines, entries := readRunLog(t, runLog)
	if len(entries) != 1 || !entries[0].Interrupted || entries[0].Games != 2 {
		t.Errorf("run log = %s, want one interrupted entry with games 2", strings.Join(lines, "\n"))
	}
}

func TestRun_RunLogFromConfig(t *testing.T) {
	repo, head := gitRepo(t)
	dir := t.TempDir()
	runLog := filepath.Join(dir, "runs.jsonl")
	ref := fakeReferee(t, "echo 5; echo 3")
	cfg := map[string]any{
		"referee": ref,
		"players": []map[string]string{{"name": "A", "cmd": "a", "commit_dir": repo}, {"name": "B", "cmd": "b"}},
		"seed":    1,
		"runlog":  runLog,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCapture(t, "-config", cfgPath, "-l", "off")
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, out)
	}
	lines, entries := readRunLog(t, runLog)
	if len(entries) != 1 {
		t.Fatalf("run log = %s", strings.Join(lines, "\n"))
	}
	p := entries[0].Players
	if p[0].Name != "A" || p[0].Commit == nil || *p[0].Commit != head || p[0].Dirty == nil || *p[0].Dirty ||
		p[1].Commit != nil || p[1].Dirty != nil {
		t.Errorf("players = %s", lines[0])
	}
}

func TestAppendRunLog_KeepsExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	if err := os.WriteFile(path, []byte("{\"old\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendRunLog(RunSummary{Games: 3, Z: defaultZ}, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != `{"old":true}` || !strings.Contains(lines[1], `"games":3,`) {
		t.Errorf("run log = %q", data)
	}
}
