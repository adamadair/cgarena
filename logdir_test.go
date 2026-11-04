package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// logWriter is a fake referee that writes {} to the file given with -l, if
// any, and reports a win for seat 1.
const logWriter = `while [ $# -gt 0 ]; do [ "$1" = "-l" ] && echo '{}' > "$2"; shift; done
echo 5; echo 3`

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func runArgs(ref, logDir string, extra ...string) []string {
	return append([]string{"-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-l", logDir}, extra...)
}

func TestIsResultFile(t *testing.T) {
	for name, want := range map[string]bool{
		"match_0.json": true, "match_12.err": true, "games.jsonl": true, "match_.json": true,
		"notes.txt": false, "match_0.json.bak": false, "games.json": false, "xmatch_0.json": false,
		"match_0.txt": false, "summary.md": false,
	} {
		if got := isResultFile(name); got != want {
			t.Errorf("isResultFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRun_LogDirMissingIsCreated(t *testing.T) {
	ref := fakeReferee(t, logWriter)
	logDir := filepath.Join(t.TempDir(), "a", "logs")
	if got := run(runArgs(ref, logDir)); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
	if got, want := strings.Join(dirNames(t, logDir), " "), "match_0.json match_1.json"; got != want {
		t.Errorf("log dir holds %q, want %q", got, want)
	}
}

func TestRun_LogDirEmptyIsUsed(t *testing.T) {
	ref := fakeReferee(t, logWriter)
	logDir := t.TempDir()
	if got := run(runArgs(ref, logDir)); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
	if got, want := strings.Join(dirNames(t, logDir), " "), "match_0.json match_1.json"; got != want {
		t.Errorf("log dir holds %q, want %q", got, want)
	}
}

func TestRun_LogDirWithUnrelatedFilesIsUsed(t *testing.T) {
	ref := fakeReferee(t, logWriter)
	logDir := t.TempDir()
	writeFiles(t, logDir, "notes.txt", "summary.md")
	if err := os.Mkdir(filepath.Join(logDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := run(runArgs(ref, logDir)); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
	want := "match_0.json match_1.json notes.txt sub summary.md"
	if got := strings.Join(dirNames(t, logDir), " "); got != want {
		t.Errorf("log dir holds %q, want %q", got, want)
	}
	if data, _ := os.ReadFile(filepath.Join(logDir, "notes.txt")); string(data) != "old" {
		t.Errorf("notes.txt was modified: %q", data)
	}
}

func TestRun_LogDirWithResultsIsRefused(t *testing.T) {
	for _, existing := range []string{"match_7.json", "match_7.err", "games.jsonl"} {
		t.Run(existing, func(t *testing.T) {
			ref := fakeReferee(t, `touch "$(dirname "$0")/ran"; echo 5; echo 3`)
			logDir := t.TempDir()
			writeFiles(t, logDir, existing)
			if got := run(runArgs(ref, logDir)); got != exitUsage {
				t.Fatalf("exit = %d, want %d", got, exitUsage)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(ref), "ran")); err == nil {
				t.Error("referee ran although the log dir was refused")
			}
			if got := strings.Join(dirNames(t, logDir), " "); got != existing {
				t.Errorf("log dir holds %q, want only %q", got, existing)
			}
		})
	}
}

func TestRun_LogDirOverwriteDeletesOnlyResults(t *testing.T) {
	// The referee writes no logs, so anything left afterwards was kept.
	ref := fakeReferee(t, "echo 5; echo 3")
	logDir := t.TempDir()
	writeFiles(t, logDir, "match_0.json", "match_99.json", "match_3.err", "games.jsonl", "notes.txt", "match_0.txt")
	sub := filepath.Join(logDir, "match_sub.json") // a directory matching the pattern is kept too
	if err := os.MkdirAll(filepath.Join(sub, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, sub, "match_1.json")
	if got := run(runArgs(ref, logDir, "-overwrite")); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
	want := "match_0.txt match_sub.json notes.txt"
	if got := strings.Join(dirNames(t, logDir), " "); got != want {
		t.Errorf("log dir holds %q, want %q", got, want)
	}
	if got, want := strings.Join(dirNames(t, sub), " "), "inner match_1.json"; got != want {
		t.Errorf("subdirectory holds %q, want %q", got, want)
	}
}

func TestRun_LogDirOverwriteFromConfig(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	logDir := t.TempDir()
	writeFiles(t, logDir, "match_0.json")
	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, []byte(`{"overwrite": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := run(runArgs(ref, logDir, "-config", config)); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
	if names := dirNames(t, logDir); len(names) != 0 {
		t.Errorf("log dir holds %v, want nothing", names)
	}
}

func TestRun_LoggingOffByDefault(t *testing.T) {
	ref := fakeReferee(t, `for a in "$@"; do [ "$a" = "-l" ] && exit 9; done; echo 5; echo 3`)
	cases := map[string][]string{
		"default": {"-r", ref, "-p1", "a", "-p2", "b"},
		"off":     {"-r", ref, "-p1", "a", "-p2", "b", "-l", "off"},
		"none":    {"-r", ref, "-p1", "a", "-p2", "b", "-l", "none"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			wd := t.TempDir()
			t.Chdir(wd)
			// Earlier results in the working directory must not matter either.
			writeFiles(t, wd, "match_0.json")
			if got := run(args); got != exitOK {
				t.Fatalf("exit = %d, want %d (exit 9 from the referee would mean -l was passed)", got, exitOK)
			}
			if got := strings.Join(dirNames(t, wd), " "); got != "match_0.json" {
				t.Errorf("working dir holds %q, want only match_0.json (no logs, off or none dir)", got)
			}
		})
	}
}
