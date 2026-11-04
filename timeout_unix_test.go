//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestReferee_TimeoutKillsProcessGroup checks that on timeout both the
// referee and a child it started (standing in for a bot) are killed.
func TestReferee_TimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	refPid, childPid := filepath.Join(dir, "ref.pid"), filepath.Join(dir, "child.pid")
	script := "echo $$ > " + refPid + "\nsleep 60 &\necho $! > " + childPid + "\nwait"
	t.Cleanup(func() { killPidFile(childPid); killPidFile(refPid) })
	ref := Referee{Command: fakeReferee(t, script), Seats: seatsOf(twoPlayers()),
		LogOutput: "off", Timeout: shortTimeout}
	start := time.Now()
	result, err := ref.Run(0)
	if elapsed := time.Since(start); elapsed > shortTimeout+700*time.Millisecond {
		t.Errorf("Run took %v with a %v timeout (hung on inherited pipes?)", elapsed, shortTimeout)
	}
	checkInvalid(t, result, err, ReasonTimeout)
	for name, path := range map[string]string{"referee": refPid, "child": childPid} {
		pid := readPid(t, path)
		if !processGone(pid, 2*time.Second) {
			t.Errorf("%s process %d is still running after the timeout", name, pid)
		}
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// processGone polls until pid no longer exists (kill -0 fails with ESRCH),
// allowing time for a killed orphan to be reaped by init.
func processGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
