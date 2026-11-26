#!/usr/bin/env python3
"""Deterministically assign `go list ./...` packages to shards via LPT.

Reads newline-separated Go package import paths from stdin, applies measured
`-race` weights from a TSV file (defaulting unlisted packages to 1.0s), and
assigns every package to exactly one shard in 1..shard_total using Longest
Processing Time (LPT) scheduling: sort by (-weight, package) and place each
package into the shard with the smallest accumulated weight (lowest shard
index breaks ties).

Prints the packages assigned to `--shard-index` in sorted order, one per line.
"""

from __future__ import annotations

import argparse
import math
import pathlib
import sys

DEFAULT_WEIGHT = 1.0


def load_weights(path: pathlib.Path | None) -> dict[str, float]:
    """Load `<package>\\t<seconds>` weights, ignoring comments and blank lines."""
    weights: dict[str, float] = {}
    if path is None:
        return weights
    if not path.is_file():
        raise FileNotFoundError(f"weights file does not exist or is not a file: {path}")
    for lineno, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) != 2:
            raise ValueError(f"{path}:{lineno}: expected '<package>\\t<seconds>', got {raw!r}")
        pkg, sec_str = parts[0].strip(), parts[1].strip()
        sec = float(sec_str)
        if not pkg or not math.isfinite(sec) or sec <= 0:
            raise ValueError(f"{path}:{lineno}: invalid entry {raw!r}")
        if pkg in weights:
            raise ValueError(f"{path}:{lineno}: duplicate weight entry for {pkg!r}")
        weights[pkg] = sec
    return weights


def assign_shards(
    packages: list[str],
    shard_total: int,
    weights: dict[str, float],
    default_weight: float = DEFAULT_WEIGHT,
) -> list[list[str]]:
    """Return `shard_total` disjoint package lists forming an exact partition."""
    if shard_total < 1:
        raise ValueError(f"shard_total must be >= 1, got {shard_total}")
    unique_pkgs = sorted({p.strip() for p in packages if p.strip()})
    ordered = sorted(
        unique_pkgs,
        key=lambda p: (-weights.get(p, default_weight), p),
    )
    loads = [0.0] * shard_total
    bins: list[list[str]] = [[] for _ in range(shard_total)]
    for pkg in ordered:
        best = min(range(shard_total), key=lambda idx: (loads[idx], idx))
        loads[best] += weights.get(pkg, default_weight)
        bins[best].append(pkg)
    return [sorted(bucket) for bucket in bins]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Select one LPT-balanced Go test shard.")
    parser.add_argument("--shard-index", type=int, required=True, help="1-based shard index")
    parser.add_argument("--shard-total", type=int, required=True, help="Total shard count (>= 1)")
    parser.add_argument("--weights", type=pathlib.Path, default=None, help="Optional TSV weight file")
    args = parser.parse_args(argv)

    if args.shard_total < 1:
        parser.error(f"--shard-total must be >= 1, got {args.shard_total}")
    if args.shard_index < 1 or args.shard_index > args.shard_total:
        parser.error(
            f"--shard-index must be in 1..{args.shard_total}, got {args.shard_index}"
        )

    weights = load_weights(args.weights)
    packages = sys.stdin.read().splitlines()
    shards = assign_shards(packages, args.shard_total, weights)
    for pkg in shards[args.shard_index - 1]:
        print(pkg)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
