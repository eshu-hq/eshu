#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Pick whole-scope samples for the #10881 rehearsal, deterministically.

Reads scope_counts.csv (scope_id,scope_kind,rows) from census_replica.sh and
writes three scope lists (one scope id per line) next to it:

  pieceA.scopes  the giant scope for the large size (kept whole)
  pieceB.scopes  the giant scope for the small size (kept whole)
  pieceC.scopes  seeded-random whole scopes (repository + many small ones)

Small size = B + C; large size = A + B + C + D, where D is a second seeded
pull. Scopes are never split. Usage:
  python3 -I pick_sample.py WORKDIR [--giant-a ID] [--giant-b ID]
"""
import argparse
import csv
import random
import sys


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("workdir")
    ap.add_argument("--seed", type=int, default=10881)
    ap.add_argument("--giant-a-rank", type=int, default=2,
                    help="rank (1 = largest) of the large-size giant scope")
    ap.add_argument("--giant-b-rank", type=int, default=12,
                    help="rank of the small-size giant scope")
    ap.add_argument("--c-rows", type=int, default=900_000)
    ap.add_argument("--d-rows", type=int, default=700_000)
    a = ap.parse_args()
    rows = [(r[0], r[1], int(r[2])) for r in csv.reader(open(f"{a.workdir}/scope_counts.csv"))]
    rows = [r for r in rows if r[2] > 0]
    by_size = sorted(rows, key=lambda r: -r[2])
    giant_a = by_size[a.giant_a_rank - 1]
    giant_b = by_size[a.giant_b_rank - 1]
    taken = {giant_a[0], giant_b[0]}
    rng = random.Random(a.seed)
    # Pool: every other scope up to 200k rows, shuffled with a fixed seed.
    pool = [r for r in rows if r[0] not in taken and r[2] <= 200_000]
    pool.sort(key=lambda r: r[0])
    rng.shuffle(pool)

    def draw(target: int):
        got, total = [], 0
        while pool and total < target:
            r = pool.pop()
            got.append(r)
            total += r[2]
        return got, total

    c, c_total = draw(a.c_rows)
    d, d_total = draw(a.d_rows)
    for name, items in (("pieceA", [giant_a]), ("pieceB", [giant_b]), ("pieceC", c), ("pieceD", d)):
        with open(f"{a.workdir}/{name}.scopes", "w") as fh:
            fh.write("".join(f"{r[0]}\n" for r in items))
    print(f"A giant rows={giant_a[2]} kind={giant_a[1]}")
    print(f"B giant rows={giant_b[2]} kind={giant_b[1]}")
    print(f"C scopes={len(c)} rows={c_total}")
    print(f"D scopes={len(d)} rows={d_total}")
    print(f"small size rows={giant_b[2] + c_total}")
    print(f"large size rows={giant_a[2] + giant_b[2] + c_total + d_total}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
