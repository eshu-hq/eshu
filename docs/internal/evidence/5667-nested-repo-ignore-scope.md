# #5667 — outer .gitignore matched across a nested-repo boundary

## What was wrong

Git never applies an outer `.gitignore` inside a nested repository: a nested
repo or submodule has its own ignore scope, and a rule in the parent says
nothing about a path in the child.

Discovery already behaved that way, because it groups by nearest repo root. The
filesystem managed-copy path (`shouldSkipFilesystemEntry`) matched every path
against the walked repo's ignore chain instead, so an untracked file inside a
nested repo that only the OUTER `.gitignore` matched was dropped from the copy
while discovery kept it. The same file was present or absent depending on which
path looked at it.

## What changed

The ignore match resolves the nearest enclosing git root, at both the file check
and the directory-prune probe. It reuses `nearestGitRoot` — which the tracked
resolver already computes for the same reason (#5658 P1a) — through a thin
`ignoreRootFor` wrapper that also handles the nil-resolver case by falling back
to the walked root, which is the pre-change behaviour.

## Performance Evidence:

The change adds a nearest-root resolution per visited entry. That walk is
memoized per directory in `rootByDir` and spawns no subprocess, but it is not
free, so it is measured rather than asserted.

`BenchmarkCopyRepositoryTree` over a synthetic tree of 1,000 files across 120
directories at depth 3, plus a nested repository of 50 files and a `.gitignore`
that matches 40 files — the shape where the nearest-root walk costs the most.
Apple M-series, `-benchtime 5x -count=3`:

| Build | ns/op (3 runs) | Median |
| --- | --- | --- |
| `origin/main` (before) | 129,400,233 / 129,917,533 / 131,287,800 | **129.9 ms** |
| this branch (after) | 133,223,450 / 132,826,983 / 140,036,783 | **133.2 ms** |

That is **+3.3 ms on 129.9 ms, about +2.6%** on a copy dominated by file I/O.
The third "after" sample (140.0 ms) is an outlier above the other two; the
spread on the after side is wider than the before side, so treat 2.6% as the
central estimate and not a tight bound.

The cost is bounded by directory count rather than file count: `nearestGitRoot`
memoizes every directory it visits, so a repository with N files in M
directories pays M walks, not N. The benchmark's 120 directories against 1,000
files is deliberately walk-heavy for that reason — a flatter repository pays
proportionally less.

The benchmark was a throwaway (`zz_bench_copy_test.go`), run against both
builds and deleted; it is not committed, because a benchmark whose baseline
lives in another commit cannot be re-run meaningfully from the tree alone.

## No-Observability-Change:

No metric, span, log or status field changes. The managed copy emits no
telemetry per skipped entry, and this alters which entries are skipped rather
than how skipping is reported.

## Correctness evidence

The regression test separates the two ignore scopes so it cannot pass by
accident — the outer repo ignores `*.tfstate`, the nested repo ignores `*.log` —
and asserts three directions: the nested untracked `*.tfstate` is kept, the
nested `*.log` is still dropped by the nested repo's own rule, and the outer
`*.tfstate` is still dropped by the outer rule. A change that simply stopped
ignoring anything inside nested repos passes the first and fails the second.

RED before the fix:

```
managed copy dropped nested scratch.tfstate: no such file or directory;
the outer repo's .gitignore must not match across a nested-repo boundary
```

`go test ./internal/collector ./internal/collector/discovery -count=1` → ok,
including the pre-existing #5658 nested-repo test unchanged.

Refs #5667
