# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`cgarena` is a Go CLI (single `main` package, no `go.mod` — it lives in GOPATH-style `~/go/src/cgarena`) that runs many games between bots using a CodinGame-style referee (a Java CLI jar), in parallel, and tallies wins/losses/ties. Not a git repository.

## Commands

There is no go.mod, so use GOPATH mode or create one first if modules are required (`go mod init cgarena`).

- Build: `go build -o cgarena .`
- Run with config: `go run . -config config.json -md out.md -html out.html`
- Run with flags: `go run . -r "java -jar test-bin/cellularena-cli-1.0.jar" -p1 "python3 test-bots/cellularena_p1.py" -p2 "python3 test-bots/cellularena_p2.py" -n 10 -t 2 -i 1234 -l results`
- All tests: `go test ./...`; single test: `go test -run TestReferee_Run .`
- Tests are integration tests: they need `java` and `python3` and use the jar/bots in `test-bin/` and `test-bots/` via paths relative to the repo root (constants in `referee_test.go`).

## Architecture

- `main.go` — flag parsing, config merging, summary writers. Config is built from CLI flags first, then **values from `-config` JSON override flags** (only non-zero values). Also defines `Player` (name, cmd, W/L/T counters; `WinPct` counts a tie as half a win). Writes Markdown (`-md`) and HTML (`-html`) summaries.
- `arena.go` — `Arena` (also the JSON config schema: `referee`, `players`, `games`, `threads`, `seed`, `log_output`, `swap`). `Run()` starts `Threads` worker goroutines consuming match IDs from a channel; each creates a `Referee`, runs it, and merges the result into `Arena.Players` under a mutex, matching players by **Name** (names must be unique). With `swap`, seat order per match is `order[matchID % len(order)]` (2 players: alternate; >2: all permutations via Heap's algorithm). The log dir is wiped (`clearLogDir`) at start of each run when logging is enabled (`LogOutput` not empty/"none"/"off").
- `referee.go` — `Referee.Run(matchId)` execs the referee command (split naively on spaces, so no quoted args), appending `-pN <cmd>`, `-seed <Seed+matchId>` (only if Seed > 0) and `-l <log>`. It parses the referee's **stdout**: integer-only lines are per-player scores (in seat order), a `seed=` line is the seed. `ProcessResults` gives win to max score(s), tie if all scores equal.
- `htmlout.html`, `results/`, `test-results/` — sample/generated outputs, not source.

## Gotchas

- `main.go` has a leftover unused `buildRefereeCmd`/`shellJoin` path (uses `-d seed=` and `sh -c`) that is not what `Referee.Run` actually does.
- Several flags are stubs: `-o` (old mode) only prints a message; `-s` just sets `Swap`. The `-pn3`/`-pn4` flags currently set the player's **Cmd**, not the name (likely a bug).
- Referee scoring parsing assumes the referee prints scores as bare integers on their own lines, with the final line being empty.
