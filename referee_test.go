package main

import (
	"testing"
)

const (
	CelluarenaReferee = "java -jar test-bin/cellularena-cli-1.0.jar"
	CellulareaP1      = "python3 test-bots/cellularena_p1.py"
	CellulareaP2      = "python3 test-bots/cellularena_p2.py"
)

func TestReferee_Run(t *testing.T) {
	// Setup mock players
	players := []Player{
		{Cmd: CellulareaP1},
		{Cmd: CellulareaP2},
	}

	ref := Referee{
		Command:   CelluarenaReferee,
		Players:   players,
		Seed:      1234,
		LogOutput: "",
	}

	result, err := ref.Run(1)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if result == nil {
		t.Fatalf("Run() returned nil result")
	}
	if result.Scores[0] != 8 || result.Scores[1] != 1 {
		t.Errorf("Expected scores [8, 1], got %v", result.Scores)
	}
	if result.Seed != 1235 {
		t.Errorf("Expected seed 1235, got %d", result.Seed)
	}
}
