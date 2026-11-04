# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
