//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRun_SIGINTKillsRunningReferees sends the test process a real SIGINT
// while run() has two slow referees running, each with a sleep child.
func TestRun_SIGINTKillsRunningReferees(t *testing.T) {
	dir := t.TempDir()
	// Game 0 finishes at once; every other game records its referee and
	// child PIDs, then hangs. With -t 2, games 1 and 2 are running when the
	// signal arrives and games 3..5 must never start.
	ref := fakeReferee(t, seedCase(`0) echo 1; echo 0 ;;
*) echo $$ > `+dir+`/ref.$g
   sleep 60 &
   echo $! > `+dir+`/child.$g.tmp; mv `+dir+`/child.$g.tmp `+dir+`/child.$g
   wait ;;`))
	t.Cleanup(func() {
		for g := 0; g < 6; g++ {
			killPidFile(filepath.Join(dir, "child."+strconv.Itoa(g)))
			killPidFile(filepath.Join(dir, "ref."+strconv.Itoa(g)))
		}
	})

	go func() {
		// Wait for both hanging games (so run's signal handler is
		// installed), then interrupt; signal anyway after 10s.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			_, err1 := os.Stat(filepath.Join(dir, "child.1"))
			_, err2 := os.Stat(filepath.Join(dir, "child.2"))
			if err1 == nil && err2 == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()

	start := time.Now()
	code, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "6", "-t", "2", "-i", "10", "-l", "off")
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("run took %v", elapsed)
	}
	if code != exitSignal {
		t.Errorf("exit code = %d, want %d\n%s", code, exitSignal, out)
	}
	for _, want := range []string{
		"Interrupted: 1 of 6 games finished; killed 2 running referee(s)\n",
		"Double forfeits: 0   Invalid: 0\n",
		"Player1: 100.00% Win Rate; 1 Wins; 0 Losses; 0 Ties",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "All matches complete.") {
		t.Errorf("interrupted run printed \"All matches complete.\":\n%s", out)
	}
	for g := 1; g <= 2; g++ {
		for _, name := range []string{"ref", "child"} {
			pid := readPid(t, filepath.Join(dir, fmt.Sprintf("%s.%d", name, g)))
			if !processGone(pid, 2*time.Second) {
				t.Errorf("game %d %s process %d is still running after the interrupt", g, name, pid)
			}
		}
	}
	for g := 3; g < 6; g++ {
		if _, err := os.Stat(filepath.Join(dir, "ref."+strconv.Itoa(g))); err == nil {
			t.Errorf("game %d was launched after the interrupt", g)
		}
	}
}
