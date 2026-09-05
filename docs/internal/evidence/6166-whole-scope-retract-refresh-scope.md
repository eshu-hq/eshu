# 6166 — narrowing the whole-scope retract to rows that asked for one

## What changed

Four `RetractEdges` branches in
`go/internal/storage/cypher/edge_writer_retract.go` used to bind the batch-wide
`collectRepoIDs(rows)` when no row in the batch carried a delta scope:
inheritance, rationale `EXPLAINS`, SQL relationships, and shell exec. They now
bind `collectWholeScopeRefreshRepoIDs(rows)` instead, and return early when
that list comes back empty.

`collectWholeScopeRefreshRepoIDs` keeps only rows stamped
`reducer.RepoRefreshIntentType` and not delta-scoped. The difference matters
because `planRepoWideRetractWork`
(`go/internal/reducer/shared_projection_worker_refresh_fence.go`) routes two
kinds of row into the retract for these four domains: per-repository refresh
rows, which are asking for a whole-repository `DELETE`, and unmarked legacy
per-edge rows, which are asking for a write. Under the old collector one
unmarked row handed its repository a whole-repository `DELETE` that erased that
repository's edges in every file, while only the rows in that one batch were
rewritten afterwards.

The shared `collectRepoIDs` is deliberately untouched. Filtering it was tried
and rejected: five other domains (code calls, repo dependency, submodule pin,
codeowners ownership, workload dependency) synthesise their retract rows in the
caller with no `intent_type` at all, so the filter empties `repo_ids` for all
five and their retracts stop firing — a silent lost retract, which is worse
graph truth than the over-delete it would have prevented. That reasoning lives
in the `collectRepoIDs` doc comment in
`go/internal/storage/cypher/edge_writer_retract_scope.go`.

## No-Regression Evidence:

Two costs could move: the Go-side collector, and the Cypher `DELETE` whose
`$repo_ids` the collector binds.

### Go-side collector

Same batch, both collectors, so the two figures are directly comparable.
Input shape is the batch the non-delta branch actually sees: 100 rows, one
whole-scope refresh row per repository, 100 distinct repository ids. 100 is
`defaultBatchLimit` in `go/internal/reducer/shared_projection_runner.go:24`,
the shared-projection batch size these retracts drain at.

VERIFIED — run on this branch after rebasing onto `origin/main` (`9b05dcf116`),
Go 1.26.6, `darwin/arm64`, Apple M4 Pro, `-12`:

```
cd go && go test ./internal/storage/cypher -run '^$' \
  -bench 'BenchmarkCollect(RepoIDs|WholeScopeRefreshRepoIDs)RationaleNonDeltaBatch' \
  -benchmem -count=5
```

| collector | ns/op (5 runs) | B/op | allocs/op |
| --- | --- | --- | --- |
| `collectRepoIDs` (before) | 2494 / 2529 / 2769 / 2804 / 2755 | 7912 | 9 |
| `collectWholeScopeRefreshRepoIDs` (after) | 3920 / 3907 / 3788 / 3857 / 3769 | 7912 | 9 |

About **+1.1 µs per 100-row batch**, and byte-for-byte identical allocation
behaviour — the added work is one map lookup and one string compare per row,
and the result slice is built the same way. Both benchmarks live in
`go/internal/storage/cypher/edge_writer_rationale_nondelta_scope_test.go`.

For scale: a whole-scope retract batch is followed by a `DELETE` against the
graph backend measured in **seconds** (below). A microsecond on the Go side is
not a cost this path can feel.

### Cypher side

The statement text does not change. The same builders run, producing the same
`MATCH`/`WHERE`/`DELETE`; the only thing that changes is the cardinality of the
bound `$repo_ids` list, and it can only shrink.

REPORTED — `docs/internal/evidence/5998-rationale-retract-probe-guard.md`,
lines 120-141, measured this exact whole-repository retract `DELETE` on
NornicDB `eshu-nornicdb-pr290:3722b483c02c` (still the `docker-compose.yaml`
pin on this branch, line 10):

- 18.603s / 17.653s / 18.071s on a 1,675,949-relationship store, while
  deleting **zero** rows.
- 0.021s / 0.022s / 0.023s for the identical statement on an empty store.
- The same `MATCH`/`WHERE` run as a bounded read (`RETURN rel LIMIT 1`) costs
  0.021s on the large store.

That set isolates the cause, and #5998 states it directly: *"Not the
predicates. Row 2 drops both property predicates and is marginally faster."*
The cost tracks **store size**, not bound cardinality. A `DELETE` binding
fewer repository ids therefore cannot plan worse than one binding more — there
is no cardinality-driven plan choice here to degrade.

The degenerate case is strictly cheaper. When `collectWholeScopeRefreshRepoIDs`
returns empty the branch returns `nil` and issues nothing at all: no probe, no
`DELETE`, no statement. Where the rationale domain would previously have gone
through its #5998 existence probe, that probe mirrors the same predicate, `IN
[]` matches nothing, and the read costs 0.021s before the `DELETE` is skipped —
so even the pre-existing path was already cheap there.

### Terminal row counts

Unchanged on today's input. Every row that reaches these four whole-scope
branches is a refresh row (see the reachability argument below), so the bound
`$repo_ids` list is the same list the old collector produced and the same
relationships are deleted. In the degenerate empty case: 0 statements issued,
0 rows deleted, 0 rows left behind that should have been retracted.

## Observability Evidence:

This section previously read `No-Observability-Change:`. That was accurate for
the original diff and is no longer, because #6233 review raised the failure mode
it left invisible and the fix adds a signal for it.

The narrowing converts one failure from an over-delete (wrong, but visible as
missing edges) into a lost retract (stale edges, no error, no dead letter). The
early return also never reaches `recordGroupedWrite`, so even the statement and
group counters stay silent -- there was no series an operator could have watched.

`EdgeWriter.logWholeScopeRetractSkipped` now warns on exactly that path:

```
WARN whole-scope retract skipped: no rows carried the refresh intent_type
     domain=... evidence_source=... retract_row_count=N repo_ids_bound=0
```

It fires only when retract rows arrived and every one was unmarked, so the
normal path pays nothing -- no call, no allocation, and the helper returns
immediately when the logger is nil or the batch is empty. No existing series,
dashboard, alert, or runbook changes; this only adds a line where previously
there was nothing at all.

Pinned by `TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete`
(`go/internal/storage/cypher/edge_writer_nil_fence_whole_scope_test.go`), which
asserts both halves. Mutation-proven: deleting the four log calls reds it with
`logs=""`, and reverting the narrowing reds it with the bound `[repo-legacy]`
and the whole-repository DELETE it would have issued.

## Why the change is safe

**Reachability.** Every per-edge intent builder for these four domains stamps
the `retract_via_refresh` marker unconditionally at emission, and
`planRepoWideRetractWork` routes a marked per-edge row to the write path, never
to the retract path. Only refresh rows — which all carry `intent_type` — reach
the whole-scope retract. So on today's input the narrowed list is identical to
the batch-wide one and the narrowing is a no-op. It is a guard for the day an
emitter changes, not a behaviour change today. Pinned by:

- `go/internal/reducer/rationale_edge_intent_retract_reachability_test.go` —
  `TestRationaleProductionIntentsNeverReachRetractAsUnmarkedRows`,
  `TestRationalePerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip`
- `go/internal/reducer/sibling_edge_intent_retract_reachability_test.go` —
  `TestSiblingProductionIntentsNeverReachRetractAsUnmarkedRows`,
  `TestSiblingPerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip`
- `go/internal/storage/cypher/edge_writer_rationale_nondelta_scope_test.go` and
  `edge_writer_sibling_nondelta_scope_test.go` — assert the bound `repo_ids`
  directly, positively (the marked repository's `DELETE` still runs) and
  negatively (an unmarked bystander is not swept into it).

**Concurrency.** A whole-scope retract is keyed by
`repoWideRetractRefreshPartitionKey`
(`go/internal/reducer/shared_projection_worker_refresh_fence.go:116-118`), and a
whole-scope key hashes to exactly one partition. One partition lease owns a
repository's repo-wide retract, so a repository's retract cannot race itself no
matter how many workers or replicas are running. This change does not touch
that keying, the lease, or the fence — it only decides which repository ids go
into a `DELETE` that was already going to run under that same lease.

The failure direction follows from that. The change can only **remove**
repository ids from a `DELETE`, never add one and never reorder anything. Its
worst case is therefore a lost retract — a repository whose stale edges survive
— which the reachability tests above rule out on today's emitters. It cannot
introduce a new write race, because it adds no write, no statement, and no
partition.

## Limits of this claim

- The Go benchmarks are single-process microbenchmarks of the two collectors.
  They measure the delta this change introduces; they are not an end-to-end
  reducer throughput measurement, and no such claim is made here.
- The Cypher figures are REPORTED from #5998's committed run, not re-measured
  for this change. That is deliberate: the statement text is byte-identical, so
  re-running it would measure #5998's finding again rather than anything this
  change does. What this note needs from those numbers is only the direction —
  cost tracks store size, not bound cardinality — and that direction is what
  #5998 isolated.
- The reachability argument is about today's emitters. If a new emitter for one
  of these four domains ships without the refresh-fence marker, its rows stop
  reaching the whole-scope retract rather than over-deleting. That is the
  intended trade, and the reachability tests are what will fail loudly.
