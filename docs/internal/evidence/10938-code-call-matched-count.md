# #10938: code-call probe-to-write race — matched counts close it at the write

The code-call lane gate (quiescence + drain) is evaluated once per cycle and
shared across partitions, but the probe was never atomic with the edge write:
a generation can activate mid-cycle (open at probe, uncommitted while later
partitions write). When an edge row's `MATCH` misses — first-generation
absence (nodes do not exist until canonical `Write` commits and the phase
publishes after `Write` returns) or a non-atomic re-projection clear —
`BatchCanonicalCodeCallUpsertCypher` `MATCH`-only rows miss silently and the
intent still completes.

## Change

Every code-call edge template (`CALLS`, `REFERENCES`, `USES_METACLASS`,
`INSTANTIATES`, plus the three label-scoped builders) now reports
`RETURN count(rel) AS matched` in the same `UNWIND` statement. The statement
count per `WriteEdges` call is unchanged and the count is atomic with the
`MERGE`.

The writer executes code-call batches through a new counted-write seam
(`CountedWriteExecutor`/`CountedWriteGroupExecutor` in
`go/internal/storage/cypher/counted_write.go`, forwarded by every
`GroupExecutor` wrapper and hidden by `ExecuteOnly` surfaces) and reconciles
matched-vs-submitted per relationship template before the caller completes
anything (`code_call_matched.go`):

- `CALLS` shortfall parks the batch with the retryable non-counting
  `shared_edge_target_not_ready` class, so the intent stays open until the
  callee phase publishes. Termination rides the existing staleness drains;
  no new probe, park state, or attempt column was added.
- `REFERENCES`/`USES_METACLASS`/`INSTANTIATES` shortfalls ship
  count+telemetry+WARN and complete (D6 census gate: parking enables per
  template only after a steady-state census; `codeCallShortfallEnforced` is
  the switch).
- Surplus (duplicate-uid nodes) completes with a WARN anomaly.
- An executor without the seam fails the batch closed with a retryable
  counting error, never silently.

Repo-dependency audit: immune — `BatchCanonicalRepoDependencyUpsertCypher`
`MERGE`s both `Repository` stub endpoints so the `MATCH` cannot miss, and the
evidence-artifact batches already run post-commit separated
(`unroutable.go`). No code change.

## RED/GREEN

| Test | RED before the fix | GREEN after |
| --- | --- | --- |
| `TestCodeCallTemplatesReportMatchedCount` (writer) | all 7 templates lack the `RETURN` tail | pass |
| `TestEdgeWriterWriteEdgesCodeCallShortfallParksBatch` (writer) | silent success (no seam) | retryable park error, non-counting class, report 2/1 |
| `TestEdgeWriterWriteEdgesCodeCallFullMatchCompletes` (writer) | n/a (pins statement-count + report 2/2) | pass, exactly 1 statement |
| `TestEdgeWriterWriteEdgesCodeCallSequentialShortfallParksBatch` (writer) | silent success | park error |
| `TestEdgeWriterWriteEdgesCodeCallWithoutSeamFailsClosed` (writer) | silent success | retryable counting error |
| `TestEdgeWriterWriteEdgesCodeCallCountOnlyShortfallCompletes` (writer) | n/a (D6 gate pin) | completes with report 1/0 |
| `TestEdgeWriterWriteEdgesCodeCallSurplusCompletes` (writer) | n/a (anomaly pin) | completes with report 2/3 |
| `TestCodeCallProjectionShortfallFailsVisitBeforeCompletion` (projection) | completes the intent | visit fails, nothing marked |
| `TestCodeCallProjectionFullMatchReportsCounts` (projection) | `UpsertedRows = 0` (empty report) | counts 1/1, intent completed |
| `TestNeo4jSessionRunnerCountedWriteLive` (cmd/reducer, live) | no seam | pass (2 present, 0 absent, group [1 0]) |
| `TestCodeCallMatchedCountLive` (writer, live) | silent zero-edge completion | park error, then convergence after the callee publishes |

## Benchmark Evidence: edge-write path before/after on the no-op executor

`BenchmarkEdgeWriterCodeCallRetractAndWrite`, 5000 call rows, `benchtime=100x`,
`count=3`, go1.26.11 linux/arm64. Baseline `b9267c2336` (clean base worktree)
vs this change. The executor is a no-op so the numbers isolate row shaping
and batching from backend round trips; against a real backend round trip
(milliseconds per batch) the residual is noise.

| Scenario | Before (3 runs) | After (3 runs) | Delta |
| --- | --- | --- | --- |
| repo_wide_full_refresh_5000_call_rows | 5.58/5.56/5.61 ms/op, 80198 allocs, 7.83 MB | 5.69/5.72/5.68 ms/op, 80204 allocs, 7.99 MB | +~2% time, +6 allocs, +2% bytes |
| delta_50_files_5000_call_rows | 5.51/5.50/5.51 ms/op | 5.75/5.57/5.65 ms/op | +~2.7% time |
| delta_deleted_only (no write rows) | ~5.9 µs/op | ~5.9 µs/op | unchanged |

The first implementation appended the `RETURN` tail with a post-`Sprintf`
concat in `buildLabelScoped*Cypher`, which doubled per-row string building
(+16% time, +5009 allocs, +2 MB per 5000 rows on the profile). The tail now
lives inside the format literals, restoring the table above.

## Observability Evidence: matched/submitted series and park signal

- New counters `eshu_dp_code_call_edge_submitted_rows_total` and
  `eshu_dp_code_call_edge_matched_rows_total` (bounded `domain` label only),
  recorded per partition visit in `recordCodeCallTiming` when the visit
  submitted rows; the visit log (`code call projection cycle completed`)
  carries `submitted_rows`/`matched_rows` beside `written_rows`.
- A shortfall WARNs (`code call edge rows did not all match; batch held for
  retry`, with evidence source, relationship, counts, one sample intent id)
  and an enforced shortfall counts one park on the existing
  `eshu_dp_shared_edge_target_miss_total{domain="code_calls"}`. No new
  unbounded labels: sample identity stays in the log.
- Docs updated: `metrics-reducer-storage.md` (counters folded into the
  `code_call_edge_batches_total` row to respect the 500-line cap),
  `shared-write-operations.md` (new "Code-Call Matched Counts" section),
  `go/internal/telemetry/README.md`, `go/internal/storage/cypher/edge/writer/README.md`,
  and the coverage-domains code-call row.

## Live proof and census

- Neo4j community 2026.08.1 (local Compose backend, `neo4j` database):
  `TestCodeCallMatchedCountLive` 0.22s, `TestNeo4jSessionRunnerCountedWriteLive`
  0.31s. All four base templates report matched==submitted in steady state
  with resolved endpoints (the D6 census for the generic shapes); an absent
  callee reports 0 and parks; the edge appears after the late callee
  publishes with no caller re-ingest (retry convergence).
- NornicDB: semantics proven on v1.3.3
  (`ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`,
  disposable local container, 2026-11-19, throwaway probe, not a committed
  test): all four base templates report matched==submitted with resolved
  endpoints; an absent callee reports 0 with the park error; a mixed
  2-present/1-absent UNWIND batch reports exactly one `matched` row with 2;
  an empty UNWIND batch reports one row with 0 — via both the Bolt driver
  and the HTTP tx/commit endpoint. The live tests keep the `backends: neo4j`
  pin until a committed NornicDB live test lands; the label-scoped
  outside-disjunction REFERENCES and INSTANTIATES shapes (#5116) are
  likewise unproven and stay count-only via `codeCallShortfallEnforced`
  (label-scoped CALLS is correctly enforced per the #5116 rationale).
