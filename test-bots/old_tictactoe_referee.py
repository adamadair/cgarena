"""A minimal old-style (cg-brutaltester -o) tic-tac-toe referee for tests.

Protocol with the tester, on stdin/stdout:
  in:  optional "###Seed N", then "###Start 2"
  out: "###Input P" + the opponent's last move ("-1 -1" on the first turn),
       "###Output P 1" (then reads the player's move "row col"),
       "###Error P ..." on an invalid move,
       "###End <ranking>" ("0 1", "1 0", or "01" for a draw).
Player 0 moves first. An invalid move loses.
"""
import sys

LINES = [[(r, c) for c in range(3)] for r in range(3)]
LINES += [[(r, c) for r in range(3)] for c in range(3)]
LINES += [[(i, i) for i in range(3)], [(i, 2 - i) for i in range(3)]]


def send(line):
    print(line, flush=True)


def main():
    line = input()
    if line.startswith("###Seed"):
        line = input()
    if line.split() != ["###Start", "2"]:
        print("expected ###Start 2, got " + line, file=sys.stderr)
        sys.exit(2)

    board = [[-1] * 3 for _ in range(3)]
    last = (-1, -1)
    for turn in range(9):
        p = turn % 2
        send(f"###Input {p}")
        send(f"{last[0]} {last[1]}")
        send(f"###Output {p} 1")
        move = input()
        try:
            r, c = (int(v) for v in move.split())
            if not (0 <= r < 3 and 0 <= c < 3) or board[r][c] != -1:
                raise ValueError
        except ValueError:
            send(f"###Error {p} invalid move {move!r}")
            send(f"###End {1 - p} {p}")
            return
        board[r][c] = p
        last = (r, c)
        if any(all(board[y][x] == p for y, x in cells) for cells in LINES):
            send(f"###End {p} {1 - p}")
            return
    send("###End 01")


if __name__ == "__main__":
    main()
