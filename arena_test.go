package main

import (
	"os"
	"testing"
)

func TestArena_Run(t *testing.T) {
	// Setup mock players
	players := []Player{
		{Cmd: CellulareaP1, Name: "Player 1"},
		{Cmd: CellulareaP2, Name: "Player 2"},
	}

	// Setup Arena
	tmpDir := os.TempDir()
	logDir := tmpDir + "/cgarenalog"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("Failed to create log directory: %v", err)
	}
	arena := Arena{
		Referee:   CelluarenaReferee,
		Players:   players,
		Games:     10,
		Threads:   2,
		Seed:      1234,
		LogOutput: logDir,
		Swap:      true,
	}

	err := arena.Run()
	if err != nil {
		t.Fatalf("Arena.Run() returned error: %v", err)
	}

	// Optionally check log output or other side effects
	// Example: check if log directory was cleared or files created
	if arena.Players[0].Wins != 10 {
		t.Errorf("Expected Player 1 to win 10 games, got %d", arena.Players[0].Wins)
	}
}
