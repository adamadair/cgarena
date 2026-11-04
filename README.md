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
| `-pn1` to `-pn4` | `Player1` to `Player4` | Display names for players 1 to 4 (`-pn3`/`-pn4` only apply if that player's command is given) |
| `-n` | `1` | Number of games to play |
| `-t` | `1` | Number of games to run in parallel |
| `-i` | `0` | Initial seed. Game *N* uses `seed + N`. A seed of 0 or less is not passed to the referee. |
| `-l` | `logs` | Directory for game logs. Use `none` or `off` to disable. |
| `-s` | `false` | Swap player seats between games |
| `-md` | | Write a Markdown summary to this path |
| `-html` | | Write an HTML summary to this path |
| `-config` | | Path to a JSON config file |
| `-v` | | Print the version |
| `-h` | | Print help |

`-o` (old mode) is accepted but not implemented.

## How it works

For each game, `cgarena` runs the referee command with these arguments appended:

```
-p1 <player 1 cmd> -p2 <player 2 cmd> [-seed <seed+game>] [-l <log dir>/match_<game>.json]
```

The referee is expected to print each player's score as a bare integer on its own line, in seat order. The highest score wins, and equal scores are a tie. The referee command is split on spaces, so arguments containing spaces can't be quoted.

With swapping on, two players alternate seats every game. With more than two players, games cycle through every seat permutation.

**The log directory is deleted and recreated at the start of each run** when logging is enabled. Don't point `-l` at a directory you want to keep.

## Output

Progress is logged as each game finishes, followed by a summary:

```
AlphaBot: 100.00% Win Rate; 10 Wins; 0 Losses; 0 Ties
BetaBot: 0.00% Win Rate; 0 Wins; 10 Losses; 0 Ties
```

Win rate counts a tie as half a win. Player names must be unique, because results are matched back to players by name.

## Tests

```sh
go test ./...
```

The tests are integration tests. They need `java` and `python3`, and must run from the repository root so the relative paths to `test-bin/` and `test-bots/` resolve.
