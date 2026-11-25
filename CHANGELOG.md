# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.0] - 2026-11-24

### Added

- **Old mode (`-o`, config key `old_mode`)** plays games with old-style CodinGame referees (games created before Botters of the Galaxy and Ultimate Tic Tac Toe), as cg-brutaltester's `-o` does. cgarena starts the referee and the bots, sends `###Seed` and `###Start`, and passes lines between them for `###Input` and `###Output`. It logs `###Error`, and reads the result from the `###End` ranking. The first place wins, and a shared first place is a draw.
  - A bot that crashes (its stdout ends before it sends the lines asked for, its stdin closes, or it can't be started) loses, and the crash counts as a fault. The game stops at once: if one bot is left it wins, and if several are left they draw.
  - New invalid-game reasons: `protocol` (malformed or unknown `###` command, bad player index or ranking) and `no_end` (the referee stopped without `###End`). The `-timeout` limit kills the referee and every bot.
  - With `-l`, each game writes a transcript to `match_<game>.log` and the stderr of the referee and bots to `match_<game>.err`, with each line prefixed by its source. `match_*.log` files count as earlier results for `-overwrite`.
  - Per-game records get a `ranking` field, and the JSON summary and run log get `"old_mode": true`.
  - `-fault-pattern` with `-o` is a usage error, and `-json-check` has no effect in old mode.
- `test-bots/old_tictactoe_referee.py` and `test-bots/old_tictactoe_bot.py`, a small old-style referee and bot used by the integration tests.

### Removed

- The `Old mode is not implemented yet.` message printed with `-o`.

## [1.1.0] - 2026-11-04

This release makes cgarena a harness whose results can be trusted: no failure is silently dropped, swapped games share their map, every run is reproducible from its output, and each run can be recorded with the bots' git commits. Several defaults and formats change; see **Breaking** below.

### Changed

#### Breaking

- **Explicit flags now override the config file.** Each setting comes from the flag if it is given on the command line, else from the config file if it has the key, else from the flag's default. Previously config values overrode flags. Config keys now apply whatever their value, so `"swap": false`, `"overwrite": false`, `"seed": 0` (random seed), `"log_output": ""` (logging off) and `"timeout": "0s"` (no limit) take effect; previously zero, `false` and empty values were ignored. A config `"games": 0` or `"threads": 0` is now a usage error instead of being ignored.
- **Player flags with config players:** `-p1`…`-p4`, `-pn1`…`-pn4` and `-pc1`…`-pc4` given explicitly override the command, name and commit directory of config player *N*; previously config `players` replaced every player flag. A `-pN` for the player after the last one adds it. A `-pnN` or `-pcN` for a player that doesn't exist, or a `-p4` without a player 3, is now a usage error (exit code 1); previously such names were silently ignored, and `-p4` without `-p3` became player 3.
- **Exit codes:** `0` when every game was valid and at least one produced a result (some double forfeits allowed); `1` for usage or config errors (including unknown or malformed flags, which used to exit 2) and for failures to write an output file; `3` when the run completed but at least one game was invalid or no game produced a result; `130` when the run was interrupted by SIGINT or SIGTERM (takes precedence over 3). Previously failed games still exited 0.
- **New summary format.** The `Name: 64.30% Win Rate; 61 Wins; 33 Losses; 4 Ties` lines are replaced by a `Summary: seeds 100..149 (50 maps, swap on, 100 games)` header and per-player lines such as `A: 61 W / 33 L / 4 D   win rate 64.3%  95% CI [54.4%, 73.1%]   faults 0` (the `faults` column only with `-fault-pattern`), followed by `Double forfeits: …   Invalid: …`. Scripts that parse stdout must be updated; `-summary-json` gives the same totals in a stable format.
- **Swap pairs share a map.** With swapping on (`-s` / `swap`), game *g* uses seed `i + g/k` and seating *g* mod *k*, where *k* is the number of seatings (2 for two players, *P*! for *P* players), so every map is played from every seating. Previously game *g* used seed `i + g`, so the two seatings of a pair were played on different maps. Without swapping, seeds are still `i + g`.
- With swapping on, `-n` must be a multiple of the number of seatings (2, 6 or 24 for 2, 3 or 4 players); otherwise cgarena exits with code 1 and says which multiple is needed.
- **Logging is off by default.** `-l` defaults to `off` instead of `logs`, so a run without `-l` writes no logs and creates no `./logs` directory. `none` and `off` still turn logging off.
- **The log directory is never deleted.** A missing directory is created and an existing one is used, leaving other files and subdirectories alone. If it already holds `match_*.json`, `match_*.err` or `games.jsonl`, cgarena exits with code 1 unless `-overwrite` is given, which deletes only those files.
- Double forfeits (every score negative) and invalid games no longer count toward any player's W/L/D or win rate, and with more than two players, seats tied for the highest score now draw instead of all winning.

#### Other changes

- The referee command (`-r`) is split on runs of whitespace (`strings.Fields`), so repeated spaces or tabs no longer create empty arguments. Quoting is still not supported.
- Usage errors found when the run starts (a log directory or `-out` file holding earlier results) and config file errors are printed as `Error: …` on stdout like the other usage errors, instead of `Failed to run arena: …` or a log line.
- `-n` and `-t` must be at least 1; otherwise cgarena exits with code 1. Previously `-t 0` played nothing and exited 0.
- Player names must be unique once flags and config are merged. A duplicate name (for example `-pn1 X -pn2 X`, or two config players with the same `name`) is a usage error with a message suggesting distinct `-pn` values for self-play. Results are attributed to players by position, not by name; names are display-only.
- The Markdown (`-md`) and HTML (`-html`) summaries show the run header, per player W, L, D, win rate, confidence interval and faults (`n/a` without `-fault-pattern`), and a second table with the double-forfeit and invalid counts. Player names are HTML-escaped in the HTML summary, and `|` is escaped in the Markdown one.
- The last line of referee stdout is parsed for scores too, so a score without a trailing newline is no longer dropped. Score and `seed=` lines are trimmed of surrounding whitespace.
- Games report their results over a channel to a single collector goroutine, which owns all counters; workers get their own copy of the seat order. This replaces the mutex.
- `-pn1`…`-pn4` help text says the flags are optional and default to `Player1`…`Player4`.
- The README, `CLAUDE.md` and `config.json` examples log to `logs/…` (now gitignored) instead of `results/`, which holds sample match files and would be refused.

### Added

- Outcome model: every game ends in exactly one outcome, `win`, `loss`, `draw`, `double_forfeit` (every score negative, e.g. `-1`/`-1`) or `invalid`. A game is invalid when the referee can't be started (`start_failed`), exits non-zero (`exit_status_<n>`), runs past the timeout (`timeout`), prints a number of integer score lines other than the number of players, including `null` scores (`score_count`), reports a `seed=` different from the `-seed` it was sent (`seed_mismatch`), or, with logging and `-json-check`, has a bad JSON log (`json_log`). Each invalid game logs one line with its game number, seed and reason as soon as it finishes, and the summary shows totals by reason.
- No-result runs: if no game produced a win, loss or draw and at least one was a double forfeit, cgarena prints `No game produced a result: all <n> games were double forfeits (check the bot commands)` (or `<d> games were double forfeits and <i> invalid`) and exits with code 3. This catches broken bot commands, which previously looked like a successful run.
- Each run prints its seed range at the start, for example `Seeds 100..149 (50 maps, swap on, 100 games)`. Without `-i` (or with `-i` 0 or less), cgarena picks a random base seed in `[1, 2^31-1]` (keeping every seed of the run at most `2^31-1`), prints it (`random base seed, rerun with -i <seed>`) and uses it as if it had been passed, so every run is reproducible from its output.
- `-timeout <duration>` (config key `timeout`, a Go duration string such as `"45s"`; default `30s`, `0` disables it): a game that runs longer is killed and counted as invalid (`timeout: referee killed after 30s`). Each referee runs in its own process group and the whole group is killed, so the bots it started die too (Linux, macOS and other Unix-like systems; on Windows only the referee is killed). A referee that exits while a process it started holds its stdout open no longer blocks the game: cgarena stops waiting after one second.
- Interrupt cleanup: on SIGINT (Ctrl-C) or SIGTERM, cgarena starts no new games, kills every running referee and its bots, prints `Interrupted: <done> of <n> games finished; killed <k> running referee(s)` and the summary of the finished games, and exits with code 130. Interrupted games count toward nothing, and `-md`/`-html` aren't written. A second signal exits at once.
- `-overwrite` (config key `overwrite`): lets `-l` point at a directory with earlier results, deleting only its `match_*.json`, `match_*.err` and `games.jsonl` first, and lets `-out` replace an existing non-empty file.
- Per-game results file: `-out <file>` (config key `out`; default `<log dir>/games.jsonl` with logging on, else none) gets one JSON object per finished game, in completion order, with `game`, `seed`, `seats`, `scores` (`null` for invalid games), `outcome` and `fault` per player, `end_reason`, `invalid_reason`, `duration_ms` and `log`. An existing non-empty file is refused (exit code 1) unless `-overwrite` is given.
- With logging on, each game's referee stderr is saved to `<log dir>/match_<game>.err` (previously discarded), and cgarena reads the CodinGame SDK JSON log: `end_reason` is the text of its last tooltip, and `-json-check` (default on; config key `json_check`) makes the game invalid (`json_log`) when the log is missing or unparseable or its `scores` differ from stdout. Use `-json-check=false` for referees that don't write that log.
- `-fault-pattern <regex>` (config key `fault_pattern`, needs `-l`): a player is faulted in a game when a JSON log tooltip or summary line matches the pattern and names its seat as `$<seat>`. Faults don't change outcomes; the summary shows each player's fault count and the per-game record a `fault` flag. If a player faulted in every valid game it played, cgarena prints `Warning: <name> faulted in all <n> games (check its command)` (exit code unchanged). For TryAngle Catch use `timeout|Unknown command|not available`.
- Win-rate confidence intervals: each player's summary line shows a Wilson score interval, with *n* = W + L + D and draws counted as half a win (an approximation, documented in the README). `-z <float>` (config key `z`, default `1.96`, i.e. 95%) sets its z value, and the label follows (`-z 2.576` prints `99% CI`); it must be greater than 0. With no results the rate and interval print as `n/a`.
- `-summary-json <file>` (config key `summary_json`): a JSON summary with `date`, `cgarena`, `referee`, `seed_from`, `seed_to`, `swap`, `games`, `threads`, `z`, `players` (each with `name`, `cmd`, `commit`, `dirty`, `w`, `l`, `d`, `win_rate`, `ci`, `faults`), `double_forfeits`, `invalid`, `invalid_reasons`, `interrupted` and `results_file`. Unlike `-md`/`-html` it is also written for interrupted runs, with `"interrupted": true`.
- Run log: `-runlog <file>` (config key `runlog`) appends one JSON line per run with the same fields as `-summary-json`, also for runs with invalid games and interrupted runs. The file is only ever appended to.
- `-pc1`…`-pc4 <dir>` (config key `commit_dir` per player): the git directory each bot was built from. Its `git rev-parse HEAD` and whether `git status --porcelain` is non-empty are recorded as the player's `commit` and `dirty` in the run log and the JSON summary, read once before the first game. If git fails they are `null` and a warning is printed; the run goes on.

### Removed

- The `Swap player positions is not implemented yet.` message printed with `-s`; swapping has always worked. (`-o` still prints that old mode is not implemented.)
- The unused `buildRefereeCmd`/`shellJoin` code.

### Fixed

- Running cgarena no longer wipes `./logs` (or whatever `-l` points at), including files that cgarena didn't write.
- A referee that prints the wrong number of scores (for example `null`) no longer crashes the run with an index-out-of-range panic.
- A referee that fails to start or exits non-zero is no longer silently skipped: the game is counted as invalid and reflected in the exit code.
- A hung referee or bot no longer hangs the whole run (see `-timeout`), and Ctrl-C no longer leaves running referees and their bots alive.
- Data race between workers reading the player list and results being merged into it, which `go test -race` reported for parallel runs.
- With swapping on and four players, generating the seat permutations no longer reorders the configured player list in place.
- Repeated spaces in the referee command no longer pass empty arguments to it.

## [1.0.2] - 2026-11-03

### Fixed

- Help text for `-pn1` to `-pn4` now describes them as display names instead of command lines.

## [1.0.1] - 2026-11-03

### Fixed

- `-pn3` and `-pn4` now set the player's name instead of its command.

## [1.0.0] - 2026-11-03

Initial versioned release.

### Added

- Run many games between bots through an external CodinGame-style referee, with parallel workers (`-t`).
- Configuration through command-line flags or a JSON config file (`-config`); config values override flags.
- Support for 2 to 4 players, with optional seat swapping between games (`-s` / `swap`), including all permutations for more than two players.
- Repeatable runs via an initial seed (`-i`); game *N* uses `seed + N`.
- Per-game referee logs written to a log directory (`-l`).
- Win/loss/tie tally per bot, printed at the end of the run, with ties counted as half a win in the win rate.
- Markdown (`-md`) and HTML (`-html`) summary output.
- `README.md` and `CLAUDE.md` documentation, and a Go module (`go.mod`).

### Known issues

- `-o` (old mode) is accepted but not implemented.
- The referee command is split on spaces, so quoted arguments are not supported.
