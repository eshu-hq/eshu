# Item 5: corpus-scaling lines at representative counts (Refs #7638)

Status: **measured — REPORT + ACCEPT, no optimization.**
No production code changed in this step. Only this file is new.

## Setup

New isolated stack (compose project `7638item5`, base `1664cf2f`,
disjoint ports, consumer flag on) over the retained item-3 corpus
fixture, per the item-3 runbook. Corpus at measure time
(coordinator-counted): **62 scopes, 63 generations, 347,856 fact rows**
(61 fact-bearing scopes). One quiet generation owed, then the real
targeted pass ran for it (`deferred_backfill_targeted_completed`,
owed=1, 16.99 s) — comfortably inside the 96 s deadline. Run host
details stay off-repo per
[run-manifest.md](../../../.agents/skills/eshu-performance-rigor/references/run-manifest.md).

## The two lines (coordinator re-ran both EXPLAINs)

Bound to the owed `(scope, generation)` exactly as the maintainer binds
them (source: `backfill/targeted/sql.go` — `ProductionQueries.Correlation`
for stmt1 (run at pass time by `write.go:187` via
`listSucceededReducerWorkItemIDsForDomainAndPartitions`),
`activeRepositoryGenerationsForPartitionsQuery` for stmt2 (run at pass
time by the `pass.go:183` ForPartitions loader), so these are the exact
pass-time shapes).

| Statement | Exec | Buffers (exec) | Shape |
|---|---|---|---|
| Reopen listing (`scope_replay_floor` MATERIALIZED + partition conjunct) | 1.8 ms (1.816 drive, 1.823 re-run) | 167 shared hits | 62-row floor, 62 lateral loops |
| `latest_generations` DISTINCT ON (ForPartitions wrap) | 8.0 ms (8.007 drive, 7.668 re-run) | 505 shared hits | DISTINCT ON over 62 scopes, 62 fact probes |

Single EXPLAIN (ANALYZE, BUFFERS) per statement plus one stability
repeat each (n=2, warm cache implied by shared-hit buffers, cache state
otherwise uncontrolled); planning costs (6.0 ms / 25.5 ms) reported
separately and not counted in exec.

## Verdict: accept the lines

Combined exec ≈ 10 ms at 62 scopes on real facts. Even a linear ×16
projection to 984 scopes lands near 160 ms — over two orders of
magnitude (~600x) below the 96 s pass deadline. The real 16.99 s pass is
dominated by other statements, not these two. The predicate-pushdown
optimization is therefore **not needed for this window**: a warm-cache,
n=2 preliminary accept — it would be a new theory requiring its own
prove-first measurement, and there is nothing to fix at these numbers.
If corpus growth ever makes these lines material, that future arm must
re-measure first — this note accepts the lines as measured, not forever.

## Caveats

1. 62 scopes / ~348k facts is representative of a large install, not the
   full 984-scope corpus; full-corpus p99 stays unmeasured (as in item 3).
2. The repo-bounded ForRepos variant (batch-writer path) was read in
   source but not EXPLAINed; only the pass-time ForPartitions shape was
   measured. The pushdown question names the floored listings, which are
   the measured ones.
