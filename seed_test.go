package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// recordingReferee is a fake referee that writes "<seed> <p1> <p2> ..." into
// the -l file it is given (so each game's arguments end up in
// match_<game>.json), then runs scores to print the scores.
func recordingReferee(t *testing.T, scores string) string {
	t.Helper()
	return fakeReferee(t, `s=none; seats=""
while [ $# -gt 0 ]; do
  case "$1" in
    -seed) s=$2; shift ;;
    -p[1-4]) seats="$seats $2"; shift ;;
    -l) l=$2; shift ;;
  esac
  shift
done
echo "$s$seats" > "$l"
`+scores)
}

// gameRecord is what recordingReferee saw for one game.
type gameRecord struct {
	seed  int
	seats []string
}

// readRecords reads the records of games 0..n-1 from dir.
func readRecords(t *testing.T, dir string, n int) []gameRecord {
	t.Helper()
	records := make([]gameRecord, n)
	for g := 0; g < n; g++ {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("match_%d.json", g)))
		if err != nil {
			t.Fatalf("game %d: %v", g, err)
		}
		fields := strings.Fields(string(data))
		seed, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("game %d: referee got no numeric -seed: %q", g, data)
		}
		records[g] = gameRecord{seed: seed, seats: fields[1:]}
	}
	return records
}

func TestArena_SwapPairsSeeds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	arena := Arena{Referee: recordingReferee(t, "echo 5; echo 3"), Players: twoPlayers(),
		Games: 10, Threads: 4, Seed: 100, LogOutput: dir, Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	records := readRecords(t, dir, arena.Games)
	for m := 0; m < arena.Games/2; m++ {
		first, second := records[2*m], records[2*m+1]
		if first.seed != 100+m || second.seed != 100+m {
			t.Errorf("games %d and %d got seeds %d and %d, want both %d", 2*m, 2*m+1, first.seed, second.seed, 100+m)
		}
		if strings.Join(first.seats, " ") != "botA botB" || strings.Join(second.seats, " ") != "botB botA" {
			t.Errorf("games %d and %d seated %v and %v, want opposite seatings", 2*m, 2*m+1, first.seats, second.seats)
		}
	}
	// Seat 1 always wins, so each player wins exactly one game per map.
	if a, b := arena.Players[0], arena.Players[1]; a.Wins != 5 || a.Losses != 5 || b.Wins != 5 || b.Losses != 5 {
		t.Errorf("A = %+v, B = %+v, want 5 W / 5 L each", a, b)
	}
}

func TestArena_NoSwapSeedsAreConsecutive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	arena := Arena{Referee: recordingReferee(t, "echo 5; echo 3"), Players: twoPlayers(),
		Games: 5, Threads: 3, Seed: 100, LogOutput: dir}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	for g, r := range readRecords(t, dir, arena.Games) {
		if r.seed != 100+g || strings.Join(r.seats, " ") != "botA botB" {
			t.Errorf("game %d: seed %d, seats %v; want seed %d, seats [botA botB]", g, r.seed, r.seats, 100+g)
		}
	}
}

func TestArena_ThreePlayerSwapBlocksShareSeed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	players := []Player{{Name: "A", Cmd: "a"}, {Name: "B", Cmd: "b"}, {Name: "C", Cmd: "c"}}
	arena := Arena{Referee: recordingReferee(t, "echo 9; echo 1; echo 1"), Players: players,
		Games: 12, Threads: 4, Seed: 50, LogOutput: dir, Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	records := readRecords(t, dir, arena.Games)
	for block := 0; block < 2; block++ {
		var seatings []string
		for g := 6 * block; g < 6*block+6; g++ {
			if records[g].seed != 50+block {
				t.Errorf("game %d seed = %d, want %d", g, records[g].seed, 50+block)
			}
			seatings = append(seatings, strings.Join(records[g].seats, ""))
		}
		sort.Strings(seatings)
		want := []string{"abc", "acb", "bac", "bca", "cab", "cba"}
		if strings.Join(seatings, " ") != strings.Join(want, " ") {
			t.Errorf("block %d seatings = %v, want all permutations %v", block, seatings, want)
		}
	}
}

func TestValidate_GamesMustBeMultipleOfSeatings(t *testing.T) {
	three := []Player{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	tests := []struct {
		players []Player
		games   int
		want    string // "" for valid
	}{
		{twoPlayers(), 4, ""},
		{twoPlayers(), 3, "must be a multiple of 2"},
		{twoPlayers(), 1, "must be a multiple of 2"},
		{three, 12, ""},
		{three, 10, "must be a multiple of 6"},
	}
	for _, tt := range tests {
		arena := Arena{Players: tt.players, Games: tt.games, Threads: 1, Swap: true}
		err := arena.Validate()
		if tt.want == "" {
			if err != nil {
				t.Errorf("%d players, %d games: unexpected error %v", len(tt.players), tt.games, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%d players, %d games: error %v, want one containing %q", len(tt.players), tt.games, err, tt.want)
		}
	}
	// Without swap any number of games is fine.
	if err := (&Arena{Players: three, Games: 7, Threads: 1}).Validate(); err != nil {
		t.Errorf("no swap, 7 games: %v", err)
	}
}

func TestRun_SwapGamesNotMultipleIsUsageError(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	base := []string{"-r", ref, "-p1", "a", "-p2", "b", "-l", "off", "-s"}
	if got := run(append(base, "-n", "3")); got != exitUsage {
		t.Errorf("-s -n 3 with 2 players: exit %d, want %d", got, exitUsage)
	}
	if got := run(append(base, "-p3", "c", "-n", "8")); got != exitUsage {
		t.Errorf("-s -n 8 with 3 players: exit %d, want %d", got, exitUsage)
	}
	if got := run(append(base, "-n", "4")); got != exitOK {
		t.Errorf("-s -n 4 with 2 players: exit %d, want %d", got, exitOK)
	}
}

func TestRun_RandomBaseSeed(t *testing.T) {
	for _, extra := range [][]string{nil, {"-i", "0"}, {"-i", "-5"}} {
		dir := filepath.Join(t.TempDir(), "logs")
		args := append([]string{"-r", recordingReferee(t, "echo 5; echo 3"), "-p1", "a", "-p2", "b",
			"-n", "6", "-t", "3", "-s", "-l", dir}, extra...)
		code, out := runCapture(t, args...)
		if code != exitOK {
			t.Fatalf("%v: exit %d, want %d\n%s", extra, code, exitOK, out)
		}
		m := regexp.MustCompile(`(?m)^Seeds (\d+)\.\.(\d+) \(3 maps, swap on, 6 games\); random base seed, rerun with -i (\d+)$`).
			FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("%v: no random seed line in output:\n%s", extra, out)
		}
		base, _ := strconv.Atoi(m[1])
		last, _ := strconv.Atoi(m[2])
		rerun, _ := strconv.Atoi(m[3])
		if base < 1 || base > maxSeed || last != base+2 || rerun != base {
			t.Errorf("%v: seed line %q is inconsistent", extra, m[0])
		}
		for g, r := range readRecords(t, dir, 6) {
			if r.seed != base+g/2 {
				t.Errorf("%v: game %d got seed %d, want %d", extra, g, r.seed, base+g/2)
			}
		}
	}
}

func TestRun_SeedLineWithGivenSeed(t *testing.T) {
	ref := fakeReferee(t, "echo 5; echo 3")
	_, out := runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "4", "-s", "-i", "100", "-l", "off")
	if !strings.Contains(out, "Seeds 100..101 (2 maps, swap on, 4 games)\n") {
		t.Errorf("missing seed range line:\n%s", out)
	}
	_, out = runCapture(t, "-r", ref, "-p1", "a", "-p2", "b", "-n", "3", "-i", "100", "-l", "off")
	if !strings.Contains(out, "Seeds 100..102 (3 maps, swap off, 3 games)\n") {
		t.Errorf("missing seed range line:\n%s", out)
	}
}

func TestRandomBaseSeedRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if s := randomBaseSeed(10); s < 1 || s+9 > maxSeed {
			t.Fatalf("randomBaseSeed(10) = %d, out of range", s)
		}
	}
	if s := randomBaseSeed(maxSeed + 5); s != 1 {
		t.Errorf("randomBaseSeed(huge) = %d, want 1", s)
	}
}

func TestArena_SeedMismatchWithPairedSeeds(t *testing.T) {
	// A referee that reports the seed it was sent: no mismatches.
	honest := fakeReferee(t, "echo 5; echo 3; "+echoSeed)
	arena := Arena{Referee: honest, Players: twoPlayers(), Games: 6, Threads: 3, Seed: 20, LogOutput: "off", Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	if arena.Invalid != 0 {
		t.Errorf("honest referee: Invalid = %d (%v), want 0", arena.Invalid, arena.InvalidReasons)
	}
	// A referee that reports the wrong seed whenever botB is in seat 1.
	liar := fakeReferee(t, `while [ $# -gt 0 ]; do
  case "$1" in -seed) s=$2; shift ;; -p1) p1=$2; shift ;; esac; shift
done
echo 5; echo 3
if [ "$p1" = botB ]; then echo "seed=$((s + 1))"; else echo "seed=$s"; fi`)
	arena = Arena{Referee: liar, Players: twoPlayers(), Games: 6, Threads: 3, Seed: 20, LogOutput: "off", Swap: true}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	if arena.Invalid != 3 || arena.InvalidReasons[ReasonSeedMismatch] != 3 || arena.Decided != 3 {
		t.Errorf("lying referee: Invalid = %d (%v), Decided = %d; want 3 seed_mismatch and 3 decided",
			arena.Invalid, arena.InvalidReasons, arena.Decided)
	}
}

func TestArena_InvalidLineShowsRandomSeed(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	arena := Arena{Referee: fakeReferee(t, "exit 2"), Players: twoPlayers(), Games: 1, Threads: 1, LogOutput: "off"}
	if err := arena.Run(); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("Game 0 (seed %d) invalid: exit_status_2", arena.Seed)
	if arena.Seed < 1 || !strings.Contains(buf.String(), want) {
		t.Errorf("Seed = %d, log output missing %q:\n%s", arena.Seed, want, buf.String())
	}
}
