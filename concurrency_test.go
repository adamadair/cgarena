package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArena_ParallelNonSwap runs a parallel non-swap arena so that
// `go test -race` can catch workers sharing state with the collector.
// The fake referee's result depends only on the seed: seed%4 == 0 is a draw,
// 1 or 2 a win for seat 1, 3 a win for seat 2.
func TestArena_ParallelNonSwap(t *testing.T) {
	script := `while [ $# -gt 0 ]; do [ "$1" = "-seed" ] && s=$2; shift; done
case $((s % 4)) in
0) echo 2; echo 2 ;;
3) echo 1; echo 6 ;;
*) echo 6; echo 1 ;;
esac`
	arena := Arena{Referee: fakeReferee(t, script), Players: twoPlayers(), Games: 40, Threads: 4, Seed: 100, LogOutput: "off"}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	a, b := arena.Players[0], arena.Players[1]
	if a.Wins != 20 || a.Losses != 10 || a.Ties != 10 {
		t.Errorf("A = %+v, want 20 W / 10 L / 10 T", a)
	}
	if b.Wins != 10 || b.Losses != 20 || b.Ties != 10 {
		t.Errorf("B = %+v, want 10 W / 20 L / 10 T", b)
	}
	if arena.Invalid != 0 || arena.DoubleForfeits != 0 {
		t.Errorf("Invalid = %d, DoubleForfeits = %d, want 0", arena.Invalid, arena.DoubleForfeits)
	}
}

// TestArena_SwapAttributesByIndex checks that, with seats swapping, each
// game's result goes to the player who sat in the winning seat: the fake
// referee always lets "botA" win, whichever seat it is in.
func TestArena_SwapAttributesByIndex(t *testing.T) {
	script := `while [ $# -gt 0 ]; do [ "$1" = "-p1" ] && p1=$2; shift; done
if [ "$p1" = botA ]; then echo 5; echo 3; else echo 3; echo 5; fi`
	arena := Arena{Referee: fakeReferee(t, script), Players: twoPlayers(), Games: 20, Threads: 4, LogOutput: "off", Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	a, b := arena.Players[0], arena.Players[1]
	if a.Wins != 20 || a.Losses != 0 || b.Wins != 0 || b.Losses != 20 {
		t.Errorf("A = %+v, B = %+v, want A to win all 20", a, b)
	}
}

// TestArena_FourPlayerSwapKeepsPlayerOrder checks that generating the seat
// permutations doesn't reorder Players (Heap's algorithm leaves a 4-element
// input permuted), and that every permutation is played.
func TestArena_FourPlayerSwapKeepsPlayerOrder(t *testing.T) {
	// Seat 1 always wins; each player sits there in 6 of the 24 permutations.
	ref := fakeReferee(t, "echo 9; echo 1; echo 1; echo 1")
	players := []Player{{Name: "A", Cmd: "a"}, {Name: "B", Cmd: "b"}, {Name: "C", Cmd: "c"}, {Name: "D", Cmd: "d"}}
	arena := Arena{Referee: ref, Players: players, Games: 24, Threads: 4, LogOutput: "off", Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"A", "B", "C", "D"} {
		p := arena.Players[i]
		if p.Name != want || p.Cmd != strings.ToLower(want) || p.Wins != 6 || p.Losses != 18 {
			t.Errorf("player %d = %+v, want %s with 6 W / 18 L", i, p, want)
		}
	}
}

func TestValidate_DuplicateNames(t *testing.T) {
	arena := Arena{Players: []Player{{Name: "X", Cmd: "a"}, {Name: "Y", Cmd: "b"}, {Name: "X", Cmd: "c"}}, Games: 1, Threads: 1}
	err := arena.Validate()
	if err == nil {
		t.Fatal("expected an error for duplicate names")
	}
	for _, want := range []string{"players 1 and 3", `"X"`, "-pn1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	if err := arena.Run(); err == nil {
		t.Error("Run accepted duplicate names")
	}
}

func TestRun_DuplicateNames(t *testing.T) {
	valid := fakeReferee(t, "echo 5; echo 3")
	config := filepath.Join(t.TempDir(), "config.json")
	cfg := `{"players": [{"name": "Same", "cmd": "a"}, {"name": "Same", "cmd": "b"}]}`
	if err := os.WriteFile(config, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []string{"-r", valid, "-p1", "bot", "-p2", "bot", "-l", "off"}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"self-play with default names", base, exitOK},
		{"self-play with distinct names", append(base, "-pn1", "A", "-pn2", "B"), exitOK},
		{"same -pn values", append(base, "-pn1", "X", "-pn2", "X"), exitUsage},
		{"-pn2 equal to the default Player1", append(base, "-pn2", "Player1"), exitUsage},
		{"duplicate names in config", append(base, "-config", config), exitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args); got != tt.want {
				t.Errorf("run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}
