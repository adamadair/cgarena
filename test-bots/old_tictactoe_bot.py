"""Tic-tac-toe bot for old_tictactoe_referee.py: plays the first free cell.

Each turn it reads the opponent's last move ("row col", "-1 -1" if none)
and prints its own move.
"""
import sys

board = [[False] * 3 for _ in range(3)]
for line in sys.stdin:
    r, c = (int(v) for v in line.split())
    if r >= 0:
        board[r][c] = True
    r, c = next((y, x) for y in range(3) for x in range(3) if not board[y][x])
    board[r][c] = True
    print(f"{r} {c}", flush=True)
