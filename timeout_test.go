package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const shortTimeout = 300 * time.Millisecond

func TestReferee_TimeoutIsInvalid(t *testing.T) {
	ref := Referee{Command: fakeReferee(t, "sleep 10; echo 5; echo 3"), Seats: seatsOf(twoPlayers()),
		LogOutput: "off", Timeout: shortTimeout}
	start := time.Now()
	result, err := ref.Run(0)
	elapsed := time.Since(start)
	checkInvalid(t, result, err, ReasonTimeout)
	if want := "timeout: referee killed after 300ms"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if elapsed > shortTimeout+700*time.Millisecond {
		t.Errorf("Run took %v with a %v timeout", elapsed, shortTimeout)
	}
}

func TestReferee_FastGameUnderTimeout(t *testing.T) {
	ref := Referee{Command: fakeReferee(t, "echo 5; echo 3"), Seats: seatsOf(twoPlayers()),
		LogOutput: "off", Timeout: 5 * time.Second}
	result, err := ref.Run(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcomes[0] != OutcomeWin || result.Outcomes[1] != OutcomeLoss {
		t.Errorf("outcomes = %v, want [win loss]", result.Outcomes)
	}
}

func TestReferee_ZeroTimeoutDisables(t *testing.T) {
	ref := Referee{Command: fakeReferee(t, "sleep 0.3; echo 5; echo 3"), Seats: seatsOf(twoPlayers()),
		LogOutput: "off", Timeout: 0}
	if _, err := ref.Run(0); err != nil {
		t.Fatalf("unexpected error with timeout disabled: %v", err)
	}
}

func TestReferee_LingeringChildDoesNotHang(t *testing.T) {
	// The referee exits cleanly but leaves a child holding its stdout open.
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ref := Referee{Command: fakeReferee(t, "sleep 5 & echo $! > "+pidFile+"; echo 5; echo 3"),
		Seats: seatsOf(twoPlayers()), LogOutput: "off"}
	t.Cleanup(func() { killPidFile(pidFile) })
	start := time.Now()
	result, err := ref.Run(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > waitDelay+time.Second {
		t.Errorf("Run took %v; it should give up on the pipe after %v", elapsed, waitDelay)
	}
	if result.Outcomes[0] != OutcomeWin {
		t.Errorf("outcomes = %v, want [win loss]", result.Outcomes)
	}
}

func TestRun_TimeoutExitCodeAndLogLine(t *testing.T) {
	var logs strings.Builder
	captureLog(t, &logs)
	ref := fakeReferee(t, "sleep 10; echo 5; echo 3")
	start := time.Now()
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-t", "2", "-i", "7",
		"-l", "off", "-timeout", "300ms")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("run took %v with a 300ms timeout", elapsed)
	}
	if code != exitInvalid {
		t.Errorf("exit code = %d, want %d", code, exitInvalid)
	}
	if !strings.Contains(out, "Invalid: 2 (timeout: 2)") {
		t.Errorf("summary missing timeout totals:\n%s", out)
	}
	if want := "invalid: timeout: referee killed after 300ms"; !strings.Contains(logs.String(), want) {
		t.Errorf("log missing %q:\n%s", want, logs.String())
	}
}

func TestRun_TimeoutFlag(t *testing.T) {
	slow := fakeReferee(t, "sleep 0.3; echo 5; echo 3")
	base := []string{"-r", slow, "-p1", "a", "-p2", "b", "-l", "off"}
	config := func(body string) string {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name  string
		extra []string
		want  int
	}{
		{"default 30s is enough", nil, exitOK},
		{"zero disables", []string{"-timeout", "0"}, exitOK},
		{"short timeout", []string{"-timeout", "100ms"}, exitInvalid},
		{"config timeout", []string{"-config", config(`{"timeout": "100ms"}`)}, exitInvalid},
		{"negative", []string{"-timeout", "-1s"}, exitUsage},
		{"unparseable", []string{"-timeout", "soon"}, exitUsage},
		{"missing unit", []string{"-timeout", "30"}, exitUsage},
		{"config negative", []string{"-config", config(`{"timeout": "-1s"}`)}, exitUsage},
		{"config not a string", []string{"-config", config(`{"timeout": 30}`)}, exitUsage},
		{"config unparseable", []string{"-config", config(`{"timeout": "soon"}`)}, exitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string{}, base...), tt.extra...)
			if got := run(args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", args, got, tt.want)
			}
		})
	}
}

// captureLog sends the standard logger's output to w for the rest of the test.
func captureLog(t *testing.T, w *strings.Builder) {
	t.Helper()
	log.SetOutput(w)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// killPidFile kills the process whose PID is in path, if any.
func killPidFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
