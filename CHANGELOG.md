# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Every game now ends in exactly one outcome: `win`, `loss`, `draw`, `double_forfeit` (every score negative, e.g. `-1`/`-1`) or `invalid`. Double forfeits and invalid games don't count toward any player's W/L/T or win rate.
- A game is `invalid` when the referee can't be started (`start_failed`), exits non-zero (`exit_status_<n>`), prints a number of integer score lines other than the number of players, including `null` scores (`score_count`), or reports a `seed=` different from the `-seed` it was sent (`seed_mismatch`).
- Each invalid game logs one line with its game number, seed and reason as soon as it finishes, and the final summary shows totals for double forfeits and invalid games, broken down by reason.
- Exit codes: `0` when every game was valid and at least one produced a result (some double forfeits allowed), `1` for usage or config errors, `3` when the run completed but at least one game was invalid or no game produced a result.
- No-result runs: if no game produced a win, loss or draw and at least one game was a double forfeit, cgarena prints `No game produced a result: all <n> games were double forfeits (check the bot commands)` and exits with code 3. When some of the games were invalid instead, the line reads `<d> games were double forfeits and <i> invalid`. This catches broken bot commands (for example a missing executable on both sides), which previously looked like a successful run. A run with only some double forfeits still exits 0.
- Each run prints its seed range at the start, for example `Seeds 100..149 (50 maps, swap on, 100 games)`.
- Without `-i` (or with `-i` 0 or less), cgarena picks a random base seed in `[1, 2^31-1]` (low enough that every seed of the run stays at most `2^31-1`), prints it on the seed line (`random base seed, rerun with -i <seed>`) and uses it exactly as if it had been passed. Every run is now reproducible from its output, and invalid-game lines always show the seed. Previously no seed meant the referee picked its own, unrecorded one.
- `-timeout <duration>` flag (config key `timeout`, a Go duration string such as `"45s"`; default `30s`, `0` disables it): a game that runs longer is killed and counted as invalid with reason `timeout`, and its invalid-game line shows the limit (`timeout: referee killed after 30s`). A negative or unparseable value is a usage error (exit code 1). Previously a hung referee or bot hung the whole run.
- Each referee now runs in its own process group, and a timeout kills the whole group, so bots started by the referee are killed too. This applies to Linux, macOS and other Unix-like systems; on other platforms (e.g. Windows) only the referee process is killed.
- Interrupt cleanup: on SIGINT (Ctrl-C) or SIGTERM, cgarena starts no new games, kills every running referee and the bots it started (the same process-group kill as the timeout; on Windows only the referee), waits for them, prints `Interrupted: <done> of <n> games finished; killed <k> running referee(s)` and the summary of the finished games, and exits with code 130. Games cut short by the interrupt count neither as results nor as invalid, and the `-md`/`-html` summaries aren't written. A second signal during the cleanup exits at once with code 130. Previously, since referees run in their own process group, Ctrl-C left running referees and their bots alive.
- Exit code `130`: the run was interrupted by SIGINT or SIGTERM and its results are incomplete. It takes precedence over exit code 3.
- `-overwrite` flag (config key `overwrite`): lets `-l` point at a directory that holds results from an earlier run, deleting only its `match_*.json`, `match_*.err` and `games.jsonl` files first.
- Per-game results file: `-out <file>` (config key `out`) writes one JSON object per finished game (JSONL, in completion order) with `game`, `seed`, `seats`, `scores` (`null` for invalid games), `outcome` per player, `end_reason`, `fault` per player, `invalid_reason`, `duration_ms` and `log`. It defaults to `<log dir>/games.jsonl` when logging is on, and to no file otherwise. Games cut short by an interrupt are not written. An existing non-empty `-out` file is refused (exit code 1) unless `-overwrite` is given, which replaces it. A failure to write the file exits with code 1.
- With logging on, each game's referee stderr is saved to `<log dir>/match_<game>.err`. Previously it was discarded.
- With logging on, cgarena reads each game's CodinGame SDK JSON log: `end_reason` is the text of its last `tooltips` entry. `-json-check` (default on; config key `json_check`) makes a game invalid with the new reason `json_log` when the JSON log is missing or unparseable, or its `scores` differ from the referee's stdout. Use `-json-check=false` for referees that don't write the SDK JSON log.
- `-fault-pattern <regex>` (config key `fault_pattern`): a player is faulted in a game when a line of a JSON log `tooltips` text or `summaries` entry matches the pattern and names that player's seat as `$<seat>` (e.g. `$0 timeout!`). Faults don't change outcomes; the summary shows each player's fault count and the per-game record a `fault` flag per player. If a player faulted in every valid game it played, cgarena prints `Warning: <name> faulted in all <n> games (check its command)` (exit code unchanged). An invalid pattern, or a pattern without logging on, is a usage error (exit code 1). There is no default pattern; for TryAngle Catch use `timeout|Unknown command|not available`.
- Win rate confidence intervals: each player's summary line shows its win rate and a Wilson score interval, with *n* = W + L + D (valid games with a result) and draws counted as half a win (an approximation, documented in the README). `-z <float>` (config key `z`, default `1.96`, i.e. 95%) sets the interval's z value; the label follows it (`-z 2.576` prints `99% CI`). A `-z` that isn't greater than 0 is a usage error (exit code 1). With no results the win rate and interval print as `n/a`. With more than two players each player's rate and interval are against the field.
- `-summary-json <file>` (config key `summary_json`): writes a JSON summary with `date` (UTC, RFC 3339), `cgarena`, `referee`, `seed_from`, `seed_to`, `swap`, `games`, `threads`, `players` (each with `name`, `cmd`, `commit`, `dirty`, `w`, `l`, `d`, `win_rate`, `ci`, `faults`), `double_forfeits`, `invalid`, `invalid_reasons`, `interrupted` and `results_file`. `commit` and `dirty` are `null` for now; `win_rate`/`ci` are `null` when a player has no result, and `faults` is `null` without `-fault-pattern`. Unlike `-md`/`-html`, it is also written for interrupted runs, with `"interrupted": true`. Failing to write it exits with code 1.

### Changed

- **Breaking:** new final summary format. The `Name: 64.30% Win Rate; 61 Wins; 33 Losses; 4 Ties[; 0 Faults]` lines are replaced by a `Summary: seeds 100..149 (50 maps, swap on, 100 games)` header and per-player lines such as `A: 61 W / 33 L / 4 D   win rate 64.3%  95% CI [54.4%, 73.1%]   faults 0` (the `faults` column only with `-fault-pattern`), followed by the `Double forfeits: …   Invalid: …` line. Scripts that parse stdout must be updated; `-summary-json` gives the same totals in a stable format.
- The Markdown (`-md`) and HTML (`-html`) summaries now show the run header, per player W, L, D, win rate, confidence interval and faults (`n/a` without `-fault-pattern`), and a second table with the double-forfeit and invalid counts (with reasons). Player names are HTML-escaped in the HTML summary, and `|` is escaped in the Markdown summary. A failure to write either file still exits with code 1.

- **Breaking:** with swapping on (`-s` / `swap`), the seats of a map are now paired: game *g* uses seed `i + g/k` and seating *g* mod *k*, where *k* is the number of seatings (2 for two players, *P*! for *P* players). So games 0 and 1 share seed `i`, games 2 and 3 share `i+1`, and so on, and every map is played from every seating. Previously game *g* always used seed `i + g`, so the two seatings were played on different maps. The old behaviour is not available any more. Without swapping, seeds are still `i + g`.
- **Breaking:** with swapping on, `-n` must be a multiple of the number of seatings (2 for two players, 6 for three, 24 for four); otherwise cgarena exits with code 1 and says which multiple is needed.
- **Breaking:** logging is now off by default. `-l` defaults to `off` instead of `logs`, so a run without `-l` writes no logs and creates no `./logs` directory. Pass `-l <dir>` to keep referee logs. `none` and `off` still turn logging off.
- **Breaking:** cgarena no longer deletes and recreates the log directory. A missing directory is created, an existing one is used, and other files and subdirectories in it are left alone. If it already holds `match_*.json`, `match_*.err` or `games.jsonl`, cgarena exits with code 1 unless `-overwrite` is given.
- With more than two players, seats tied for the highest score now get a draw instead of a win.
- The last line of referee stdout is now parsed for scores too, so a score without a trailing newline is no longer dropped. Score and `seed=` lines are trimmed of surrounding whitespace.
- `-n` and `-t` must be at least 1; otherwise cgarena exits with code 1. Previously `-t 0` played nothing and exited 0.
- Unknown or malformed flags now exit with code 1 instead of 2.
- Player names must be unique after flags and the config file are merged. A duplicate name (for example `-pn1 X -pn2 X`, or two config players with the same `name`) is a usage error (exit code 1) with a message suggesting distinct `-pn` values for self-play.
- Results are attributed to players by their position in the player list, not by name; names are display-only.
- Games now report their results over a channel to a single collector goroutine, which owns all counters. Workers get their own copy of the seat order and no longer read the shared player list. This replaces the mutex.
- `-pn1`…`-pn4` help text now says the flags are optional and default to `Player1`…`Player4`.

### Fixed

- Running cgarena no longer wipes `./logs` (or whatever `-l` points at), including files that cgarena didn't write.
- A referee that prints the wrong number of scores (for example `null`) no longer crashes the run with an index-out-of-range panic.
- A referee that fails to start or exits non-zero is no longer silently skipped: the game is counted as invalid and reflected in the exit code.
- A referee that exits while a process it started still holds its stdout open no longer blocks the game forever: cgarena stops waiting after one second and uses the output it has read.
- Data race between workers reading the player list and results being merged into it, which `go test -race` reports for parallel runs.
- With swapping on and four players, generating the seat permutations no longer reorders the configured player list in place.

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
