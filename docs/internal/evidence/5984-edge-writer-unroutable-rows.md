# #5984 — a shared-edge write that routed nothing must not report success

## What was wrong

`EdgeWriter.WriteEdges` discarded every row `buildRowMap` could not route and
then returned `nil`, including when it discarded all of them. The shared
projection worker completes the latest intent row only after `WriteEdges`
succeeds, so a `nil` from an all-dropped batch marked the work done. Intent IDs
are deterministic and the durable upsert never reopens a completed row, so the
work was never retried: the edges did not exist, and nothing in Postgres or the
logs said so. The comment above the drop claimed rows were "skipped to avoid
silent failures"; the skip was the silent failure.

Root-Cause Evidence: `go/internal/storage/cypher/edge_writer.go` returned `nil`
on the `len(routedRows) == 0` branch with no counter, no log, and no error,
while `ProcessPartitionOnce`
(`go/internal/reducer/shared_projection_worker.go`) appends the batch's intent
IDs to `processedIDs` and calls `MarkIntentsCompleted` on any non-error return.
`TestProcessPartitionOnceDoesNotCompleteIntentsWhenWriteEdgesFails` exercises
that second half directly: with the old `nil`, completion happened; with an
error, it does not.

## What changed

`WriteEdges` now separates three cases that all looked like success:

| Batch | Before | After |
| --- | --- | --- |
| empty | `nil` | `nil` (unchanged) |
| only control rows (a repo refresh, which carries no edge) | `nil` | `nil` (unchanged) |
| some rows routed, some did not | `nil`, drop folded into an INFO line | `nil` + WARN + counter |
| non-empty, nothing routed | `nil` | `*UnroutableRowsError`, intent not completed |

The control-row carve-out matters as much as the error. A code-call delta whose
files were only deleted arrives as a single repo-refresh row whose job is the
repo-wide retract. Counting that as an unroutable edge would refuse to complete
a correct partition on every poll and wedge it.
`BenchmarkEdgeWriterCodeCallRetractAndWrite/delta_deleted_only_50_files_0_call_rows`
drives exactly that shape and failed on the first implementation, which is how
the case was found; `reducer.CarriesNoEdge` is the shared predicate that keeps
`filterUpsertRows` and the writer agreeing on what "no edge to write" means.

## No-Regression Evidence

No-Regression Evidence: the two edge-writer benchmarks that shape rows at repo
scale show no statistically significant change against base `b1b951045`
(geomean +0.14%, every case `~` at n=8); allocations per op are identical.

The change adds two counters and one predicate call to the row loop, on the
already-taken not-routed branch only. Measured on the two edge-writer
benchmarks that shape rows at repo scale, base `b1b951045` vs this branch, same
machine, `-benchtime=200x -count=8 -cpu=1`, compared with `benchstat`:

| Benchmark | Base sec/op | This branch sec/op | Verdict |
| --- | --- | --- | --- |
| `EdgeWriterRepoDependencyWrite` (5000 rows) | 4.957m ± 6% | 4.919m ± 9% | ~ (p=0.505, n=8) |
| `EdgeWriterCodeCallRetractAndWrite/repo_wide_full_refresh_5000_call_rows` | 4.309m ± 8% | 4.327m ± 10% | ~ (p=0.721, n=8) |
| `EdgeWriterCodeCallRetractAndWrite/delta_50_files_5000_call_rows` | 4.178m ± 7% | 4.250m ± 29% | ~ (p=0.161, n=8) |
| `EdgeWriterCodeCallRetractAndWrite/delta_deleted_only_50_files_0_call_rows` | 4.034µ ± 3% | 4.002µ ± 4% | ~ (p=0.505, n=8) |

Geomean +0.14%; no benchmark shows a statistically significant difference.
Allocations per op are unchanged (`7334442 B/op`, `85101 allocs/op` for
`EdgeWriterRepoDependencyWrite` on both sides). Backend: none — these
benchmarks use a no-op executor on purpose, so the numbers isolate Eshu-owned
row shaping and dispatch from backend round trips. The host was under
concurrent load from other agents during both runs; the runs were taken
back-to-back under the same conditions, and the claim being made is
no-regression, which noise would show as a difference rather than hide.

Commands:

```bash
cd go && go test ./internal/storage/cypher/ -run '^$' \
  -bench 'BenchmarkEdgeWriter(RepoDependencyWrite|CodeCallRetractAndWrite)' \
  -benchtime=200x -count=8 -cpu=1
benchstat before.txt after.txt
```

## Observability Evidence

Observability Evidence: a new `eshu_dp_shared_edge_unroutable_rows_total`
counter (domain + bounded reason) and a `shared edge rows unroutable` WARN log;
`scripts/verify-telemetry-coverage.sh` exits 0 with the X1 doc row in place.

New: `eshu_dp_shared_edge_unroutable_rows_total`, an Int64Counter incremented by
the dropped-row count, labelled by projection `domain` and a bounded `reason`
(`partial_batch` when routable rows still wrote, `whole_batch` when the batch
produced nothing). Registered in `go/internal/telemetry/instruments.go` and
recorded in `docs/public/observability/telemetry-coverage.md` under
"graph write (unroutable shared-edge rows)".

Alongside it, a `WARN` structured log, `shared edge rows unroutable`, carrying
`domain`, `evidence_source`, `input_rows`, `dropped_rows`, `reason`, and one
`sample_intent_id` an operator can look up. High-cardinality values stay in the
log, not in metric labels. Before this change an all-dropped batch produced no
log line at all — the INFO success line was only reached when something wrote.

Gate: `scripts/verify-telemetry-coverage.sh` → exit 0,
"docs/public/observability/telemetry-coverage.md and
go/internal/telemetry/instruments.go agree, no new untracked stages".

## Already-dropped edges

No repair migration is included, and this is a deliberate call rather than an
omission. Nothing in the current schema records that a row was dropped, so a
completed intent from before this change is indistinguishable from one that
legitimately had nothing to write — there is no set to repair from. What the
change gives instead is the signal that makes the population knowable from the
next run onward: the counter is zero when no edge is being lost, and any
non-zero value names the domain and a sample intent. A corpus whose counter
stays at zero has nothing to repair; a corpus that reports drops now has the
domain and sample needed to scope one.
