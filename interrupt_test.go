package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReferee_CancelIsInterruptedNotTimeout checks that a game killed because
// the run's context was cancelled is reported as interrupted, not as a
// timeout or an exit status, even with a timeout set.
func TestReferee_CancelIsInterruptedNotTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	ref := Referee{Command: fakeReferee(t, "exec sleep 60"), Seats: seatsOf(twoPlayers()),
		LogOutput: "off", Timeout: 30 * time.Second}
	start := time.Now()
	result, err := ref.RunContext(ctx, 0)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("RunContext took %v after cancel", elapsed)
	}
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v, want ErrInterrupted", err)
	}
	if !result.Interrupted || !result.Killed {
		t.Errorf("Interrupted = %v, Killed = %v, want both true", result.Interrupted, result.Killed)
	}
	if result.InvalidReason != "" {
		t.Errorf("InvalidReason = %q, want none", result.InvalidReason)
	}
}

func TestReferee_CancelledBeforeStartIsNotKilled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ref := Referee{Command: fakeReferee(t, "echo 1; echo 0"), Seats: seatsOf(twoPlayers()), LogOutput: "off"}
	result, err := ref.RunContext(ctx, 0)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v, want ErrInterrupted", err)
	}
	if !result.Interrupted || result.Killed {
		t.Errorf("Interrupted = %v, Killed = %v, want true, false", result.Interrupted, result.Killed)
	}
}

// TestArena_CancelledRunCountsNothing checks that a run cancelled before it
// starts launches no referee and counts no game.
func TestArena_CancelledRunCountsNothing(t *testing.T) {
	dir := t.TempDir()
	launched := filepath.Join(dir, "launched")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	arena := &Arena{Referee: fakeReferee(t, "echo x >> "+launched+"; echo 1; echo 0"),
		Players: twoPlayers(), Games: 4, Threads: 2, Seed: 1, LogOutput: "off"}
	if err := arena.RunContext(ctx); err != nil {
		t.Fatal(err)
	}
	if !arena.Interrupted || arena.Finished() != 0 || arena.Killed != 0 || arena.Invalid != 0 {
		t.Errorf("Interrupted=%v Finished=%d Killed=%d Invalid=%d, want true 0 0 0",
			arena.Interrupted, arena.Finished(), arena.Killed, arena.Invalid)
	}
	if _, err := os.Stat(launched); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a referee was launched after the run was cancelled")
	}
}

// TestRun_SecondSignalExitsAtOnce delivers two signals through the injected
// hooks: the second one must call exitNow(130) without waiting for cleanup.
func TestRun_SecondSignalExitsAtOnce(t *testing.T) {
	oldNotify, oldStop, oldExit := notifySignals, stopSignals, exitNow
	t.Cleanup(func() { notifySignals, stopSignals, exitNow = oldNotify, oldStop, oldExit })
	notifySignals = func(c chan<- os.Signal) { c <- syscall.SIGINT; c <- syscall.SIGTERM }
	stopSignals = func(chan<- os.Signal) {}
	exitCode := -1
	exitNow = func(code int) { exitCode = code } // read only after run returns

	dir := t.TempDir()
	pid := filepath.Join(dir, "ref.pid")
	t.Cleanup(func() { killPidFile(pid) })
	ref := fakeReferee(t, "echo $$ > "+pid+"; exec sleep 60")
	code, _ := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "2", "-l", "off")
	if exitCode != exitSignal {
		t.Errorf("exitNow called with %d, want %d", exitCode, exitSignal)
	}
	if code != exitSignal {
		t.Errorf("exit code = %d, want %d", code, exitSignal)
	}
}
