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

`WriteEdges` returns a `SharedProjectionWriteReport` naming every row it could
not route, and the reducer — which owns intent completion — persists those rows
durably BEFORE completing, then completes as usual.

| Batch | Before | After |
| --- | --- | --- |
| empty | `nil` | `nil` (unchanged) |
| only control rows (a repo refresh carries no edge) | `nil` | `nil` (unchanged) |
| some routed, some did not | `nil`, rows silently completed | rows reported, recorded durably, then completed |
| non-empty, nothing routed | `nil`, rows silently completed | rows reported, recorded durably, then completed |

### Why not simply fail the write

The first cut of this change returned an error for the all-unroutable case, and
review (codex, PR #6008) showed it could not converge. `buildRowMap` decides
purely from the persisted payload, so a rejected row is rejected identically on
every future attempt. This path has no attempt budget and no dead letter — the
runner's own comment states that `shared_projection_intents` carries no
`attempt_count` column. Returning a retryable error for a provably
non-retryable condition stalls the partition forever on work that can never
succeed, and skips that cycle's stale/superseded/terminal completions with it.

The owner chose quarantine-and-complete. The work genuinely cannot be done, so
completing is correct; what was missing was any record that it was not done.

### The half the error would not have fixed

A MIXED batch writes its routable rows, returns nil, and `ProcessPartitionOnce`
appends **every** row in `completedLatestRows` to `processedIDs` — including the
ones that produced no edge (`shared_projection_worker.go:415-420`). That loss
exists on main and survived the first cut of this branch, which only covered the
all-unroutable case. The report covers both shapes uniformly.

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

### Targeted branch benchmark (review follow-up)

The repo-scale benchmarks above cannot say anything about the added branch: in
`EdgeWriterRepoDependencyWrite` every row routes, so `reducer.CarriesNoEdge` and
the drop counters are never reached, and a regression in them would hide behind
the Cypher-building cost of 5,000 routable rows. `BenchmarkEdgeWriterUnroutableRowLoop`
isolates it, `-benchtime=200x -count=4 -cpu=1`:

| Case | sec/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `all_routable` (baseline path) | 2.57m | 3 761 161 | 40 052 |
| `all_unroutable_edge_rows` | 0.19m | 48 | 1 |
| `all_control_rows` | 0.32m | 80 001 | 5 000 |

The two new branches are 8-13x CHEAPER per batch than the routable path, which
is the expected shape: a row that does not route never reaches Cypher building
or batching. `all_control_rows` costs about 64ns and one small allocation per
row — the case a deleted-files-only code-call delta hits on every poll — which
is negligible against the retract the same cycle performs.

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

The existing `shared edge write completed` INFO line now also carries
`dropped_rows` next to `skipped_intents`. They are not the same number:
`skipped_intents` counts every row that produced no write statement, control
rows included, while `dropped_rows` counts only rows that were meant to become
an edge. Reporting them together stops an operator from reading one as the
other.

Alongside it, a `WARN` structured log, `shared edge rows unroutable`, carrying
`domain`, `evidence_source`, `input_rows`, `dropped_rows`, `reason`, and one
`sample_intent_id` an operator can look up. High-cardinality values stay in the
log, not in metric labels. Before this change an all-dropped batch produced no
log line at all — the INFO success line was only reached when something wrote.

The counter records each rejected row once: the reducer records the rows
durably and then completes the intent, so a batch is not re-selected and the
count does not inflate on repeat cycles. That was NOT true of the earlier
revision, which failed the intent and therefore re-counted every poll — the
observation that surfaced it (raised in review on PR #6008) is one of the
things that made the fail-the-intent approach untenable.

Gate: `scripts/verify-telemetry-coverage.sh` → exit 0,
"docs/public/observability/telemetry-coverage.md and
go/internal/telemetry/instruments.go agree, no new untracked stages".

## What keeps quarantine from becoming a new silent loss

A durable row nobody reads is a loss nobody notices, so the record alone is not
the fix. Three things carry it:

- `eshu_dp_shared_edge_unroutable_rows_total`, labelled by domain and a bounded
  `partial_batch`/`whole_batch` reason.
- A `shared edge rows unroutable` WARN with domain, evidence source, counts and
  a sample intent id.
- **A required hard-zero corpus-gate check**,
  `drains.shared_projection_unroutable_quarantined`. This is the load-bearing
  one: completing quarantined intents drains the nonterminal count, so without
  it the gate would go GREEN on a corpus that lost edges. No snapshot knob — the
  corpus is fixed input, so there is no legitimate count above zero.

The check is evaluated after the drain and deliberately **not** added to
`Drained()`. Quarantined rows are terminal and never decrease; in the poll
predicate they would mean the drain never converges, and a crisp assertion
failure would surface as a drain TIMEOUT instead — reading as "the pipeline
needed longer" and hiding the finding.
`TestDrainedIgnoresQuarantinedUnroutableIntents` pins that exclusion.

## Why the reason is split

`buildRowMap` returns `!ok` for two different things, and conflating them would
mislead in opposite directions:

- `missing_required_field` — required MATCH identifiers absent. No writer
  version can route it; the loss is real.
- `no_statement_for_type` — the row is well formed but this binary has no
  statement for its relationship type. During a rolling upgrade a newer producer
  emits types an older writer has not learned, and the same row routes fine once
  the writer catches up.

Recording both as the same thing would either bury real losses in deploy noise
or page someone about a rollout working as intended. Raised by Fable in the
design research as the sharpest failure mode of the quarantine approach.

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
