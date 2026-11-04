# cgarena

`cgarena` is a command-line tool that plays many games between bots using a CodinGame-style referee and reports each bot's wins, losses and ties. Games run in parallel, and player seats can be swapped between games so neither bot keeps a positional advantage.

## Requirements

- Go (see `go.mod` for the version)
- Whatever your referee and bots need. The bundled examples use `java` (for the referee jars in `test-bin/`) and `python3` (for the bots in `test-bots/`).

## Build

```sh
go build -o cgarena .
```

Or run directly with `go run .` followed by the flags below.

## Usage

### With flags

```sh
./cgarena \
  -r "java -jar test-bin/cellularena-cli-1.0.jar" \
  -p1 "python3 test-bots/cellularena_p1.py" -pn1 AlphaBot \
  -p2 "python3 test-bots/cellularena_p2.py" -pn2 BetaBot \
  -n 10 -t 2 -i 1234 -l results
```

### With a config file

```sh
./cgarena -config config.json -md summary.md -html summary.html
```

Example `config.json`:

```json
{
  "referee": "java -jar test-bin/cellularena-cli-1.0.jar",
  "players": [
    { "name": "AlphaBot", "cmd": "python3 test-bots/cellularena_p1.py" },
    { "name": "BetaBot",  "cmd": "python3 test-bots/cellularena_p2.py" }
  ],
  "games": 10,
  "threads": 2,
  "seed": 12345,
  "log_output": "results",
  "swap": true
}
```

Values in the config file override the corresponding command-line flags. Only non-zero or non-empty values are applied, so a config file can't turn a flag back off.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-r` | | Referee command (required) |
| `-p1`, `-p2` | | Commands for players 1 and 2 (required) |
| `-p3`, `-p4` | | Commands for players 3 and 4 (optional) |
| `-pn1` to `-pn4` | `Player1` to `Player4` | Optional display names for players 1 to 4 (`-pn3`/`-pn4` only apply if that player's command is given). Names must be unique. |
| `-n` | `1` | Number of games to play (at least 1) |
| `-t` | `1` | Number of games to run in parallel (at least 1) |
| `-i` | `0` | Base seed. Without `-s`, game *N* uses `seed + N`; with `-s`, see [Seeds](#seeds). If 0 or less, a random base seed is picked and printed. |
| `-l` | `off` | Directory for game logs. Logging is off unless you give a directory; `none` and `off` also turn it off. See [Log directory](#log-directory). |
| `-overwrite` | `false` | Let `-l` point at a directory holding results from an earlier run, and delete those result files first. Also lets `-out` replace an existing non-empty file. |
| `-out` | `<log dir>/games.jsonl` with `-l`, else none | Per-game results file, one JSON object per line (config key `out`). See [Per-game results](#per-game-results). |
| `-json-check` | `true` | With `-l`, make a game invalid (`json_log`) when the referee's JSON log is missing, unparseable, or its `scores` differ from stdout. Use `-json-check=false` (config `"json_check": false`) for referees that don't write the CodinGame SDK JSON log. Has no effect without `-l`. |
| `-fault-pattern` | | Regular expression for bot faults (config key `fault_pattern`); needs `-l`. See [Faults](#faults). |
| `-timeout` | `30s` | Per-game time limit, as a Go duration such as `45s` or `2m` (config key `timeout`, a string like `"45s"`). A game that runs longer is killed and counted as invalid (`timeout`). `0` disables the limit; a negative or unparseable value exits with code 1. See [Timeout](#timeout). |
| `-s` | `false` | Swap player seats between games, playing each map from every seating. `-n` must then be a multiple of the number of seatings (2, 6 or 24 for 2, 3 or 4 players). |
| `-md` | | Write a Markdown summary to this path |
| `-html` | | Write an HTML summary to this path |
| `-config` | | Path to a JSON config file |
| `-v` | | Print the version |
| `-h` | | Print help |

`-o` (old mode) is accepted but not implemented.

## How it works

For each game, `cgarena` runs the referee command with these arguments appended:

```
-p1 <player 1 cmd> -p2 <player 2 cmd> -seed <seed> [-l <log dir>/match_<game>.json]
```

The seed of each game is described under [Seeds](#seeds).

The referee is expected to print each player's score as a bare integer on its own line, in seat order. The referee command is split on spaces, so arguments containing spaces can't be quoted.

Each game ends in one of these outcomes:

| Outcome | Rule |
|---------|------|
| `win` / `loss` | The highest score is unique: that seat wins, the others lose. |
| `draw` | Several seats share the highest score (and it is 0 or more): they draw, the others lose. |
| `double_forfeit` | Every score is negative (e.g. `-1`/`-1`). Not counted in W/L/T or win rate; reported separately. |
| `invalid` | See below. Not counted in W/L/T or win rate; reported separately. |

A game is invalid, with one of these reasons, when:

- `start_failed`: the referee command can't be started (for example, it doesn't exist).
- `exit_status_<n>`: the referee exits with non-zero status *n*.
- `timeout`: the game ran longer than `-timeout` and was killed (see [Timeout](#timeout)).
- `score_count`: the number of integer score lines differs from the number of players. `null` scores count as no score line.
- `seed_mismatch`: a `-seed` was sent but the referee printed a `seed=` line with a different value.
- `json_log`: with `-l` and `-json-check` on (the default), the referee's JSON log is missing or unparseable, or its `scores` disagree with stdout (see [JSON log](#json-log)).

Each invalid game is logged as soon as it finishes, with its game number, seed and reason, for example `Game 3 (seed 13) invalid: score_count: got 1 score lines for 2 players`.

With swapping on, two players alternate seats every game. With more than two players, games cycle through every seat permutation.

### Seeds

Let *k* be the number of seatings: 1 without `-s`, 2 with `-s` and two players, and *P*! with `-s` and *P* players (6 for three, 24 for four). Game *g* (counting from 0) uses seed `i + g/k` (integer division) and seating *g* mod *k*. So:

- without `-s`, game *g* uses seed `i + g`;
- with `-s` and two players, games 0 and 1 share seed `i` with opposite seats, games 2 and 3 share `i+1`, and so on. Every map is played from every seating.

With `-s`, `-n` must be a multiple of *k*, otherwise cgarena exits with code 1. A run uses seeds `i` to `i + n/k - 1` and prints that range when it starts, for example:

```
Seeds 100..149 (50 maps, swap on, 100 games)
```

If `-i` is not given (or is 0 or less), cgarena picks a random base seed in `[1, 2^31-1]`, keeping every seed of the run at most `2^31-1`, and uses it exactly as if it had been passed. The seed line then ends with `; random base seed, rerun with -i <seed>`, so any run can be repeated from its output. The config key `seed` sets the base seed too.

### Timeout

Each referee is started in its own process group. When a game runs longer than `-timeout` (default `30s`), cgarena kills the whole group with `SIGKILL`, so the bots the referee started die with it, and counts the game as invalid with reason `timeout`. The invalid-game line shows the limit, for example `Game 4 (seed 14) invalid: timeout: referee killed after 30s`. `-timeout 0` turns the limit off.

The process-group kill applies to Linux and macOS (and other Unix-like systems). **On other platforms, such as Windows, only the referee process is killed**; bots it started may keep running.

As with the other config keys, a config `timeout` of `"0s"` is ignored rather than disabling the limit; use `-timeout 0`.

### Interrupting a run

Pressing Ctrl-C (SIGINT) or sending SIGTERM stops the run cleanly. cgarena starts no new games, kills every referee that is still running together with its bots (the same process-group kill as for a timeout), waits for them, and then prints:

```
Interrupted: 7 of 20 games finished; killed 4 running referee(s)
```

followed by the usual summary of the games that finished, and exits with code 130. Games cut short by the interrupt count toward nothing: they are neither results nor invalid. The totals are only informational, because the run is incomplete, and the `-md`/`-html` summaries aren't written. A second Ctrl-C (or SIGTERM) while this cleanup is running exits at once with code 130, even if some processes may survive. On platforms without process groups, such as Windows, only the referee processes are killed, as for the timeout.

If the referee exits but a process it started keeps the referee's stdout open, cgarena stops waiting for it after one second and uses the output it has read.

## Log directory

Logging is off by default. With `-l <dir>` (config key `log_output`), each game's referee log is written to `<dir>/match_<game>.json`, the referee's stderr is saved to `<dir>/match_<game>.err`, and the per-game results go to `<dir>/games.jsonl` (unless `-out` says otherwise). Without `-l`, the referee's stderr is discarded. cgarena never deletes the log directory. At the start of a run it:

- creates the directory if it doesn't exist;
- uses it as is if it exists and holds no earlier results. Other files and subdirectories are left alone;
- refuses to run (exit code 1) if it holds `match_*.json`, `match_*.err` or `games.jsonl` files, unless `-overwrite` (config key `overwrite`) is given;
- with `-overwrite`, deletes only those files directly in the directory. Nothing else is touched, including subdirectories.

So to rerun into the same directory, add `-overwrite`, or pick a fresh directory.

### JSON log

With `-l`, cgarena reads each game's JSON log after the referee exits. It expects the CodinGame SDK format and uses three keys:

- `scores`: an object keyed by seat index (`{"0": 10, "1": -1}`). With `-json-check` on (the default), it must hold exactly one score per seat, equal to the score the referee printed on stdout; otherwise, or if the log is missing or isn't valid JSON, the game is invalid with reason `json_log`.
- `tooltips`: the text of the last entry becomes the game's `end_reason` (`""` when there are none).
- `tooltips` and `summaries`: searched for faults (see [Faults](#faults)).

For a referee that doesn't write this format, pass `-json-check=false`: the games are then judged on stdout only, and a log that can't be read just leaves `end_reason` empty and no faults.

### Faults

`-fault-pattern <regex>` (config key `fault_pattern`, Go `regexp` syntax) marks a player as faulted in a game when a line of a `tooltips` text or of a `summaries` entry matches the pattern **and** names that player's seat as `$<seat>` (`$0` for seat 1, `$1` for seat 2, and so on; `$1` doesn't match `$12`). Lines are checked one at a time, because one frame's summary can hold lines about several players. Seats are mapped back to players, so with `-s` a fault is counted for the right player whichever seat it played.

For TryAngle Catch, whose referee writes lines such as `$0 timeout!` and `$1: Unknown command: FLY`, use:

```sh
-fault-pattern 'timeout|Unknown command|not available'
```

cgarena has no default pattern. Faults don't change outcomes: a bot that times out still loses. The summary shows each player's fault count (`; 2 Faults`), and the per-game record has a `fault` flag per player. Faults are only counted in valid games (double forfeits included). An invalid pattern, or `-fault-pattern` without `-l` (there is no JSON log to read), exits with code 1.

If a player faulted in every valid game it played, cgarena prints a warning after the summary; the exit code is unchanged:

```
Warning: Hang faulted in all 4 games (check its command)
```

This catches a single broken bot command, which otherwise just looks like a bot that loses every game.

## Output

The run starts with the seed range line (see [Seeds](#seeds)). Progress is logged as each game finishes, followed by a summary:

```
AlphaBot: 100.00% Win Rate; 10 Wins; 0 Losses; 0 Ties
BetaBot: 0.00% Win Rate; 0 Wins; 10 Losses; 0 Ties
Double forfeits: 0   Invalid: 0
```

With `-fault-pattern`, each player line ends with its fault count, e.g. `; 0 Faults`.

When there are invalid games, the totals line breaks them down by reason, e.g. `Invalid: 2 (exit_status_2: 1, score_count: 1)`.

If no game produced a result (win, loss or draw) and at least one game was a double forfeit, cgarena prints a line after the summary and exits with code 3. This usually means both bot commands are broken, for example a missing executable, so every game ends `-1`/`-1`:

```
No game produced a result: all 10 games were double forfeits (check the bot commands)
```

When the run also had invalid games, the line reads `No game produced a result: 6 games were double forfeits and 4 invalid (check the bot commands)`. A run where every game was invalid gets no such line, since each invalid game is already reported.

### Per-game results

Each finished game is written as one JSON object per line (JSONL), in completion order (not game order), to `-out <file>` (config key `out`), or to `<log dir>/games.jsonl` when `-l` is given without `-out`. Without either there is no results file. For example (`-s`, two players, with `-l` and `-fault-pattern`):

```json
{"game":0,"seed":100,"seats":["Wait","Hang"],"scores":[0,-1],"outcome":{"Hang":"loss","Wait":"win"},"end_reason":"$1 timeout!","fault":{"Hang":true,"Wait":false},"invalid_reason":"","duration_ms":2795,"log":"run1/match_0.json"}
{"game":1,"seed":100,"seats":["Hang","Wait"],"scores":[-1,0],"outcome":{"Hang":"loss","Wait":"win"},"end_reason":"$0 timeout!","fault":{"Hang":true,"Wait":false},"invalid_reason":"","duration_ms":2791,"log":"run1/match_1.json"}
```

| Field | Meaning |
|-------|---------|
| `game`, `seed` | Game number (from 0) and the seed sent to the referee |
| `seats` | Player name in each seat; `seats[j]` played as `-p<j+1>` |
| `scores` | Score per seat, in the same order as `seats`; `null` for invalid games |
| `outcome` | Outcome per player name: `win`, `loss`, `draw`, `double_forfeit` (for every player) or `invalid` (for every player) |
| `end_reason` | Text of the last JSON log tooltip; `""` without one, without `-l`, or for games invalid before the log was read |
| `fault` | Per player name, whether it matched `-fault-pattern`; all `false` without a pattern and for invalid games |
| `invalid_reason` | `""` for valid games, else one of the reasons listed under [How it works](#how-it-works) |
| `duration_ms` | How long the referee ran |
| `log` | Path of the game's JSON log, as given with `-l`; `""` without `-l` |

Games cut short by an interrupt (see [Interrupting a run](#interrupting-a-run)) are not written. If `-out` names an existing non-empty file, cgarena refuses to run (exit code 1) unless `-overwrite` is given, which replaces the file; an existing empty file is used. `games.jsonl` inside the log directory follows the log directory rules above. If writing the file fails during the run, cgarena logs the error, stops writing records and exits with code 1 after the games finish.

### Player names

Win rate counts a tie as half a win. Player names must be unique after the flags and config file are merged, otherwise cgarena exits with code 1. For self-play, give the two copies distinct names, e.g. `-pn1 A -pn2 B`. Results are attributed to players by their position in the player list; names are only used for display.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All games were valid, and at least one produced a result (win, loss or draw). Some double forfeits are allowed: they are a bot result, not a harness fault. |
| `1` | Usage or config error, such as a bad flag (including a negative or unparseable `-timeout`), missing referee or player, duplicate player names, `-n`/`-t` below 1, a log directory holding earlier results or an existing non-empty `-out` file without `-overwrite`, an invalid `-fault-pattern` or one given without `-l`. Also a failure to write the results file. |
| `3` | The run completed, but at least one game was invalid, or no game produced a result (every valid game was a double forfeit). |
| `130` | Interrupted by SIGINT or SIGTERM (see [Interrupting a run](#interrupting-a-run)): running referees were killed and the results are incomplete. This takes precedence over `3`. |

## Tests

```sh
go test ./...
go test -race ./...
```

Most outcome and exit-code tests use small fake `sh` referees written to a temp directory. The remaining tests are integration tests: they need `java` and `python3`, and must run from the repository root so the relative paths to `test-bin/` and `test-bots/` resolve.
