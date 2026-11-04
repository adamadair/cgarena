# cgarena

`cgarena` is a command-line tool that plays many games between bots using a CodinGame-style referee and reports each bot's wins, losses and draws, with a win rate and its confidence interval. Games run in parallel, and player seats can be swapped between games so neither bot keeps a positional advantage.

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
  -n 10 -t 2 -i 1234 -l logs/flags-example
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
  "log_output": "logs/config-example",
  "swap": true
}
```

The config keys are `referee`, `players` (each with `name`, `cmd` and optional `commit_dir`), `games`, `threads`, `seed`, `log_output`, `swap`, `overwrite`, `timeout`, `out`, `json_check`, `fault_pattern`, `z`, `summary_json` and `runlog`; each is described with its flag below. `-md` and `-html` have no config key.

### Precedence

Each setting comes from, in order of priority:

1. the flag, if it is given explicitly on the command line (even with its default value, e.g. `-s=false` or `-timeout 30s`);
2. the config file, if it has the key (any value counts, including `false`, `0` and `""`: `"swap": false` turns swapping off, `"timeout": "0s"` disables the timeout);
3. the flag's default.

Players work the same way, one field at a time. The player list starts as the config's `players`, or, without them, as two players named `Player1` and `Player2` with no command. `-pN`, `-pnN` and `-pcN` then override the command, name and commit directory of player *N* (counting from 1) and leave its other fields alone. A `-pN` for the player right after the last one adds that player (named `PlayerN` unless `-pnN` is given); that is how `-p3` and `-p4` add players. A `-pN` that would skip a player, or a `-pnN`/`-pcN` for a player that doesn't exist, is a usage error (exit code 1). Empty flag values (`-pn1 ""`) are ignored.

For example, with the `config.json` above:

```sh
./cgarena -config config.json -n 4 -pn2 Beta2 -l logs/quick -timeout 0
```

plays 4 games instead of 10, with player 2 named `Beta2` (its command still comes from the config), logs to `logs/quick` and has no timeout. `threads`, `seed`, `swap` and player 1 still come from the config.

**Breaking (1.1.0):** before 1.1.0 the config file overrode the flags, and only its non-zero values were used.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-r` | | Referee command (required; config key `referee`) |
| `-p1`, `-p2` | | Commands for players 1 and 2 (required; config key `cmd` per player) |
| `-p3`, `-p4` | | Commands for players 3 and 4 (optional; `-p4` needs a player 3) |
| `-pn1` to `-pn4` | `Player1` to `Player4` | Optional display names for players 1 to 4 (config key `name` per player). Naming a player that doesn't exist is a usage error. Names must be unique. |
| `-pc1` to `-pc4` | | Optional git directory each player's bot was built from (config key `commit_dir` per player); as for `-pnN`, the player must exist. Its commit and dirty state are recorded in `-runlog` and `-summary-json`. See [Run log](#run-log). |
| `-n` | `1` | Number of games to play, at least 1 (config key `games`) |
| `-t` | `1` | Number of games to run in parallel, at least 1 (config key `threads`) |
| `-i` | `0` | Base seed. Without `-s`, game *N* uses `seed + N`; with `-s`, see [Seeds](#seeds). If 0 or less, a random base seed is picked and printed. Config key `seed`. |
| `-l` | `off` | Directory for game logs. Logging is off unless you give a directory; `none` and `off` also turn it off. Config key `log_output`. See [Log directory](#log-directory). |
| `-overwrite` | `false` | Let `-l` point at a directory holding results from an earlier run, and delete those result files first. Also lets `-out` replace an existing non-empty file. Config key `overwrite`. |
| `-out` | `<log dir>/games.jsonl` with `-l`, else none | Per-game results file, one JSON object per line (config key `out`). See [Per-game results](#per-game-results). |
| `-json-check` | `true` | With `-l`, make a game invalid (`json_log`) when the referee's JSON log is missing, unparseable, or its `scores` differ from stdout. Use `-json-check=false` (config `"json_check": false`) for referees that don't write the CodinGame SDK JSON log. Has no effect without `-l`. |
| `-fault-pattern` | | Regular expression for bot faults (config key `fault_pattern`); needs `-l`. See [Faults](#faults). |
| `-timeout` | `30s` | Per-game time limit, as a Go duration such as `45s` or `2m` (config key `timeout`, a string like `"45s"`). A game that runs longer is killed and counted as invalid (`timeout`). `0` disables the limit; a negative or unparseable value exits with code 1. See [Timeout](#timeout). |
| `-s` | `false` | Swap player seats between games, playing each map from every seating. `-n` must then be a multiple of the number of seatings (2, 6 or 24 for 2, 3 or 4 players). Config key `swap`. |
| `-z` | `1.96` | z value of the win-rate confidence interval (config key `z`): `1.96` is 95%, `2.576` is 99%. Must be greater than 0, otherwise cgarena exits with code 1. See [Statistics](#statistics). |
| `-md` | | Write a Markdown summary to this path |
| `-html` | | Write an HTML summary to this path |
| `-summary-json` | | Write a JSON summary to this path (config key `summary_json`). See [JSON summary](#json-summary). |
| `-runlog` | | Append one JSON line per run to this file (config key `runlog`). See [Run log](#run-log). |
| `-config` | | Path to a JSON config file (see [Precedence](#precedence)) |
| `-v` | | Print the version |
| `-h` | | Print help |

`-o` (old mode) is accepted but not implemented.

## How it works

For each game, `cgarena` runs the referee command with these arguments appended:

```
-p1 <player 1 cmd> -p2 <player 2 cmd> -seed <seed> [-l <log dir>/match_<game>.json]
```

The seed of each game is described under [Seeds](#seeds).

The referee is expected to print each player's score as a bare integer on its own line, in seat order.

The referee command (`-r`) is split on runs of spaces and tabs, and run directly, without a shell. **Quoting is not supported**: `"a b"` or `'a b'` is passed as the two arguments `"a` and `b"`, so no argument can contain a space. If you need one, put the command in a small script and pass the script's path. (Player commands are passed to the referee as single arguments and are not split by cgarena.)

Each game ends in one of these outcomes:

| Outcome | Rule |
|---------|------|
| `win` / `loss` | The highest score is unique: that seat wins, the others lose. |
| `draw` | Several seats share the highest score (and it is 0 or more): they draw, the others lose. |
| `double_forfeit` | Every score is negative (e.g. `-1`/`-1`). Not counted in W/L/D or win rate; reported separately. |
| `invalid` | See below. Not counted in W/L/D or win rate; reported separately. |

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

In the config file, `"timeout": "0s"` disables the limit too.

### Interrupting a run

Pressing Ctrl-C (SIGINT) or sending SIGTERM stops the run cleanly. cgarena starts no new games, kills every referee that is still running together with its bots (the same process-group kill as for a timeout), waits for them, and then prints:

```
Interrupted: 7 of 20 games finished; killed 4 running referee(s)
```

followed by the usual summary of the games that finished, and exits with code 130. Games cut short by the interrupt count toward nothing: they are neither results nor invalid. The totals are only informational, because the run is incomplete, and the `-md`/`-html` summaries aren't written. A `-summary-json` file is still written, with `"interrupted": true`. A second Ctrl-C (or SIGTERM) while this cleanup is running exits at once with code 130, even if some processes may survive. On platforms without process groups, such as Windows, only the referee processes are killed, as for the timeout.

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

cgarena has no default pattern. Faults don't change outcomes: a bot that times out still loses. The summary shows each player's fault count (`faults 2`), and the per-game record has a `fault` flag per player. Faults are only counted in valid games (double forfeits included). An invalid pattern, or `-fault-pattern` without `-l` (there is no JSON log to read), exits with code 1.

If a player faulted in every valid game it played, cgarena prints a warning after the summary; the exit code is unchanged:

```
Warning: Hang faulted in all 4 games (check its command)
```

This catches a single broken bot command, which otherwise just looks like a bot that loses every game.

## Output

The run starts with the seed range line (see [Seeds](#seeds)). Progress is logged as each game finishes, followed by `All matches complete.` and a summary:

```
Summary: seeds 100..149 (50 maps, swap on, 100 games)
A: 61 W / 33 L / 4 D   win rate 64.3%  95% CI [54.4%, 73.1%]   faults 0
B: 33 W / 61 L / 4 D   win rate 35.7%  95% CI [26.9%, 45.6%]   faults 2
Double forfeits: 1   Invalid: 1 (timeout: 1)
```

The first line repeats the run's seed range under a `Summary:` label, so the summary can be read on its own; for an interrupted run it ends with `7 of 100 games finished` instead. Each player line shows wins, losses and draws, the win rate and its confidence interval (see [Statistics](#statistics)). The `faults` column is only shown with `-fault-pattern`. The last line counts double forfeits and invalid games, which aren't in any player's W/L/D; invalid games are broken down by reason, e.g. `Invalid: 2 (exit_status_2: 1, score_count: 1)`.

**Breaking:** this replaces the earlier `AlphaBot: 100.00% Win Rate; 10 Wins; 0 Losses; 0 Ties` lines; scripts that parse stdout need updating. For machine-readable totals use `-summary-json`.

If no game produced a result (win, loss or draw) and at least one game was a double forfeit, cgarena prints a line after the summary and exits with code 3. This usually means both bot commands are broken, for example a missing executable, so every game ends `-1`/`-1`:

```
No game produced a result: all 10 games were double forfeits (check the bot commands)
```

When the run also had invalid games, the line reads `No game produced a result: 6 games were double forfeits and 4 invalid (check the bot commands)`. A run where every game was invalid gets no such line, since each invalid game is already reported.

### Statistics

For each player, *n* = W + L + D counts its valid games that produced a result (double forfeits and invalid games are left out). The win rate is p̂ = (W + D/2) / *n*, and the interval is the Wilson score interval

```
(p̂ + z²/2n ± z·sqrt(p̂(1−p̂)/n + z²/4n²)) / (1 + z²/n)
```

with z = 1.96 (95%) by default; `-z` (config key `z`) changes it, and the label follows (`-z 2.576` prints `99% CI`). With *n* = 0 the win rate and interval are printed as `n/a`.

**Counting a draw as half a win is an approximation.** It is the usual convention, but a game with three possible outcomes isn't a binomial trial, so the interval is only approximate when there are draws (with few draws the difference is negligible).

With more than two players, each player's W/L/D, win rate and interval are against the field: a game is a win for the player with the unique highest score, a draw for players tied for the highest score and a loss for everyone else, and the same formula is used.

### Summary files

`-md <file>` and `-html <file>` write the same summary as a table: the header line, then per player W, L, D, win rate, CI and faults (`n/a` without `-fault-pattern`), then a second table with the double-forfeit and invalid counts (with reasons). Player names are HTML-escaped in the HTML summary, and `|` is escaped in the Markdown one. Neither is written for an interrupted run.

### JSON summary

`-summary-json <file>` (config key `summary_json`) writes the summary as an indented JSON object:

```json
{
  "date": "2026-11-03T21:30:00Z",
  "cgarena": "1.1.0",
  "referee": "java -jar referee.jar",
  "seed_from": 100,
  "seed_to": 149,
  "swap": true,
  "games": 100,
  "threads": 4,
  "z": 1.96,
  "players": [
    {"name": "A", "cmd": "./bots/a", "commit": "5c9cdf4…", "dirty": false,
     "w": 61, "l": 33, "d": 4, "win_rate": 0.6428571428571429,
     "ci": [0.5443, 0.7307], "faults": 0},
    {"name": "B", "...": "..."}
  ],
  "double_forfeits": 1,
  "invalid": 1,
  "invalid_reasons": {"timeout": 1},
  "interrupted": false,
  "results_file": "logs/games.jsonl"
}
```

- `date` is the time the run ended, in UTC (RFC 3339). `games` is the number of games requested (`-n`).
- `win_rate` and `ci` are fractions (not percentages) at the run's `-z`; both are `null` when the player has no win, loss or draw.
- `faults` is `null` without `-fault-pattern`.
- `z` is the z value of the confidence intervals (`-z`).
- `commit` and `dirty` are the player's git commit and whether its working tree had uncommitted changes, from `-pcN` / `commit_dir`; `null` without one or when git failed (see [Run log](#run-log)).
- `results_file` is the per-game results file (see [Per-game results](#per-game-results)), or `null` when there is none.
- Unlike `-md`/`-html`, the file is also written for an interrupted run, with `"interrupted": true`, so the totals are clearly marked as incomplete.

A failure to write any summary file exits with code 1.

### Run log

`-runlog <file>` (config key `runlog`) appends one line to the file per run: the same object as the [JSON summary](#json-summary), on a single line. It keeps a history of runs, for example:

```json
{"date":"2026-11-04T15:52:21Z","cgarena":"1.1.0","referee":"java -jar referee.jar","seed_from":100,"seed_to":101,"swap":true,"games":4,"threads":2,"z":1.96,"players":[{"name":"Boss","cmd":"mono bots/Boss.exe","commit":"1cb42e318b472bb8658ba52108fe69f13d0a4c31","dirty":true,"w":4,"l":0,"d":0,"win_rate":1,"ci":[0.5100999795960008,1],"faults":null},{"name":"L1Boss","cmd":"mono bots/L1Boss.exe","commit":"1cb42e318b472bb8658ba52108fe69f13d0a4c31","dirty":true,"w":0,"l":4,"d":0,"win_rate":0,"ci":[0,0.48990002040399916],"faults":null}],"double_forfeits":0,"invalid":0,"invalid_reasons":{},"interrupted":false,"results_file":"logs/games.jsonl"}
```

- The file is created if missing and only ever appended to, never truncated. Each run adds one complete line, written at once at the end of the run (`date` is the time the run ended, in UTC, RFC 3339).
- A line is appended for runs with invalid games (exit code 3) and for interrupted runs (`"interrupted": true`, exit code 130), so they stay visible in the history. Usage and config errors (exit code 1) happen before any game is played and add no line; neither does a run that fails with a harness error (for example the per-game results file can't be written).
- `z` records the confidence level the run's intervals used.
- Failing to append to the run log exits with code 1.

Each player's commit comes from `-pc1` … `-pc4 <dir>` (or `"commit_dir"` in that player's config entry): cgarena records `git -C <dir> rev-parse HEAD` as `commit`, and whether `git -C <dir> status --porcelain` prints anything (uncommitted or untracked files) as `dirty`. The directory is relative to the working directory. The commits are read once, at the start of the run, before any game, so rebuilding a bot during the run doesn't change what is recorded. Without a directory, `commit` and `dirty` are `null`. If git fails (not a repository, no commit yet, git not installed), they are `null` too and cgarena prints `Warning: can't read the git commit of <name> from <dir>, recording null: <git error>`; the run goes on and its exit code doesn't change.

Two bots built from the same working tree record the same commit. To compare an older version, build it from its own checkout (e.g. a `git worktree`) and point its `-pcN` there.

`-pcN` overrides the `commit_dir` of config player *N*, like `-pnN` overrides its name (see [Precedence](#precedence)).

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

Player names must be unique after the flags and config file are merged, otherwise cgarena exits with code 1. For self-play, give the two copies distinct names, e.g. `-pn1 A -pn2 B`. Results are attributed to players by their position in the player list; names are only used for display.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All games were valid, and at least one produced a result (win, loss or draw). Some double forfeits are allowed: they are a bot result, not a harness fault. |
| `1` | Usage or config error, printed as a line starting with `Error:` (or by the flag parser), such as a bad flag (including a negative or unparseable `-timeout`), an unreadable or invalid config file, missing referee or player, a `-pnN`/`-pcN` for a player that doesn't exist, duplicate player names, `-n`/`-t` below 1, a log directory holding earlier results or an existing non-empty `-out` file without `-overwrite`, an invalid `-fault-pattern` or one given without `-l`, a `-z` that isn't greater than 0. Also a failure to write the results file or a summary file. |
| `3` | The run completed, but at least one game was invalid, or no game produced a result (every valid game was a double forfeit). |
| `130` | Interrupted by SIGINT or SIGTERM (see [Interrupting a run](#interrupting-a-run)): running referees were killed and the results are incomplete. This takes precedence over `3`. |

## Tests

```sh
go test ./...
go test -race ./...
```

Most outcome and exit-code tests use small fake `sh` referees written to a temp directory. The remaining tests are integration tests: they need `java` and `python3`, and must run from the repository root so the relative paths to `test-bin/` and `test-bots/` resolve.
