package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// runCapture calls run(args) and returns its exit code and everything it
// printed to stdout. It swaps os.Stdout, so tests using it must not run in
// parallel.
func runCapture(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	code := run(args)
	os.Stdout = old
	w.Close()
	return code, <-out
}

// seedCase is a fake referee script that picks its output by game number
// (seed - 10, so run it with -i 10). Game numbers not listed fall to "*".
func seedCase(cases string) string {
	return `while [ $# -gt 0 ]; do [ "$1" = "-seed" ] && g=$(($2 - 10)); shift; done
case $g in
` + cases + `
esac`
}

func TestRun_AllDoubleForfeitsIsNoResult(t *testing.T) {
	ref := fakeReferee(t, "echo -1; echo -1")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "3", "-t", "2", "-l", "off")
	if code != exitInvalid {
		t.Errorf("exit code = %d, want %d", code, exitInvalid)
	}
	want := "No game produced a result: all 3 games were double forfeits (check the bot commands)\n"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if !strings.Contains(out, "Double forfeits: 3   Invalid: 0") {
		t.Errorf("summary missing double-forfeit count:\n%s", out)
	}
}

func TestRun_SomeDoubleForfeitsStillExitZero(t *testing.T) {
	// Game 1 is a double forfeit; the others are decided.
	ref := fakeReferee(t, seedCase(`1) echo -1; echo -1 ;;
*) echo 5; echo 3 ;;`))
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "4", "-i", "10", "-l", "off")
	if code != exitOK {
		t.Errorf("exit code = %d, want %d", code, exitOK)
	}
	if strings.Contains(out, "No game produced a result") {
		t.Errorf("unexpected no-result message:\n%s", out)
	}
	if !strings.Contains(out, "Double forfeits: 1   Invalid: 0") {
		t.Errorf("summary missing double-forfeit count:\n%s", out)
	}
}

func TestRun_InvalidAndDoubleForfeitsIsNoResult(t *testing.T) {
	// Games 0 and 2 are double forfeits, 1 and 3 invalid (score_count).
	ref := fakeReferee(t, seedCase(`0|2) echo -1; echo -1 ;;
*) echo null; echo null ;;`))
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "4", "-i", "10", "-l", "off")
	if code != exitInvalid {
		t.Errorf("exit code = %d, want %d", code, exitInvalid)
	}
	want := "No game produced a result: 2 games were double forfeits and 2 invalid (check the bot commands)\n"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if !strings.Contains(out, "Double forfeits: 2   Invalid: 2 (score_count: 2)") {
		t.Errorf("summary missing totals:\n%s", out)
	}
}

func TestRun_InvalidWithResultsHasNoNoResultMessage(t *testing.T) {
	// Game 0 is decided, game 1 a double forfeit, game 2 invalid.
	ref := fakeReferee(t, seedCase(`0) echo 5; echo 3 ;;
1) echo -1; echo -1 ;;
*) exit 2 ;;`))
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "3", "-i", "10", "-l", "off")
	if code != exitInvalid {
		t.Errorf("exit code = %d, want %d", code, exitInvalid)
	}
	if strings.Contains(out, "No game produced a result") {
		t.Errorf("unexpected no-result message:\n%s", out)
	}
}

func TestRun_AllInvalidHasNoNoResultMessage(t *testing.T) {
	ref := fakeReferee(t, "echo null; echo null")
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-l", "off")
	if code != exitInvalid {
		t.Errorf("exit code = %d, want %d", code, exitInvalid)
	}
	if strings.Contains(out, "No game produced a result") {
		t.Errorf("unexpected no-result message for an all-invalid run:\n%s", out)
	}
}
