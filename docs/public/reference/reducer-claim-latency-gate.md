# Reducer Claim-Latency Gate

The reducer claim-latency gate is the contract for changing the reducer queue
claim path as reducer domains grow. It exists because the resolution engine
must keep graph readiness ordering and conflict-domain safety while avoiding a
hot polling query whose cost grows with every new edge domain.

This page is a design and benchmark gate. It does not rewrite reducer claim SQL,
change worker defaults, alter queue DDL, move readiness state, or change
production runtime knobs.

## Current Hot Path

The resolution engine polls `fact_work_items` for `stage='reducer'` rows that
are visible, unexpired, not superseded, and not blocked by an active row with
the same durable conflict domain and key. Claimed work then runs through the
domain handler and is acked, retried, or dead-lettered.

Readiness gates in the current claim query keep edge and property domains
pending until their required canonical node phases are visible in
`graph_projection_phase_state`. Those gates preserve correctness: an edge
domain must not claim work, resolve missing endpoints, and then record a retry
or incomplete write when the owning node materialization has not yet committed.

The scaling risk is the claim predicate shape, not the readiness rule. Each new
edge family that embeds another `EXISTS` readiness check makes every reducer
poll and every reducer replica evaluate more domain-specific predicates before
one row can be claimed.

## Workflow Boundaries

Reducer queue changes must separate these scopes before implementation:

| Boundary | Owner |
| --- | --- |
| Claim transaction | Selects one or more eligible `fact_work_items` rows, updates lease owner, attempt count, claim deadline, and timestamps, then returns work to the reducer loop. |
| Handler execution | Runs outside the claim transaction and may load facts, read readiness state, write graph/read-model truth, and publish phase state. |
| Ack or fail transaction | Marks the work item succeeded, retrying, or dead-lettered after handler execution. |
| Retry scope | Replays the claimed reducer intent and must tolerate duplicate delivery, expired leases, stale readiness rows, and partial prior writes. |
| Conflict scope | Uses durable `conflict_domain` and `conflict_key` values, not worker count, to prevent unsafe overlap. |

Follow-up designs must state which boundary changes and which boundary stays
unchanged. Moving readiness into a lookup or compact status row still keeps
handler execution, ack/fail, retry, and dead-letter ownership in the reducer
queue.

## Correctness Invariants

Future claim-path changes must preserve all of these invariants:

| Invariant | Requirement |
| --- | --- |
| Conflict fence | Rows sharing an active `conflict_domain` and `conflict_key` cannot run concurrently. |
| Readiness ordering | Edge and property domains can run only after their required canonical node phase rows are committed. |
| Retry boundary | Transient absence of readiness state stays pending or retryable; it must not become success, silent skip, or a broader fallback. |
| Idempotency | Reclaiming an expired claim or replaying a dead-letter row must converge on the same graph/read-model truth. |
| Dead-letter visibility | Terminal failures remain visible through queue state, failure class, logs, and status. |
| Domain isolation | Optimizing one domain must not serialize unrelated domains that have independent conflict keys. |

Serialization is not an accepted permanent fix. Lowering
`ESHU_REDUCER_WORKERS`, shrinking `ESHU_REDUCER_BATCH_CLAIM_SIZE`, disabling
batch claims, or forcing batch size `1` is diagnostic-only unless a separate
tracked benchmark proves the serial path satisfies the repo-scale performance
contract.

## Benchmark Matrix

Any change that moves, rewrites, caches, or materializes reducer readiness in
the claim path must capture the same-shape benchmark before and after the
change.

| Dimension | Required values |
| --- | --- |
| Queued rows | At least `100000` and `1000000` reducer rows, plus the production default for the target proof. |
| Domain count | Current domain catalog count and at least one expanded synthetic count that represents the next projected edge-domain tranche. |
| Readiness state | Empty, fully ready, and partially ready `graph_projection_phase_state` rows. |
| Queue shape | Pending rows, retrying rows, expired claims, and active conflicting claims. |
| Conflict domains | `code_graph`, `platform_graph`, and default scope conflict keys. |
| Workers and replicas | Configured `ESHU_REDUCER_WORKERS`, batch claim size, and reducer replica count for the run. |
| Backend profile | Postgres version, graph backend profile, Eshu commit, schema/bootstrap state, and whether pprof was enabled. |

The current baseline benchmark is
`BenchmarkReducerQueueClaimDeepQueue` in `go/internal/storage/postgres`. It
uses the real `ReducerQueue.Claim` SQL against live Postgres and is gated by
`ESHU_REDUCER_CLAIM_BENCH_DSN` or `ESHU_POSTGRES_DSN` so normal unit gates do
not require Postgres.

## Claim-Latency Budget

The first implementation PR after this contract must record the current
same-shape baseline and then enforce the follow-up budget against that baseline:

- p95 claim latency must not exceed 1.10x the same-shape baseline for the same
  queued-row count, readiness shape, worker setting, and Postgres profile;
- max claim latency must not increase by more than 60 seconds on the largest
  measured depth;
- the expanded-domain synthetic run must stay flat or sub-linear relative to
  domain count;
- retrying, expired, and conflicting rows must not be hidden from `/admin/status`
  or queue metrics while the benchmark is running;
- a claimed row must still respect readiness ordering and conflict fencing.

If the current baseline already violates the target budget, the implementation
issue must classify that as the measured problem and use the baseline as the
before number. It must not widen the budget to make a regression look healthy.

Future benchmark runs must report p50, p95, max, row counts, domain count,
readiness row count, active conflict count, retry count, dead-letter count, and
claim success or no-work outcome. Claim latency should stay flat or sub-linear
as domain count increases. If the same-shape run is more than 10% slower or more
than 60 seconds worse than the known-normal band, stop and profile before
merging.

## Allowed Follow-Up Directions

Follow-up implementation issues may move domain readiness out of the hot poll
predicate only when the replacement preserves the invariants above. Acceptable
directions include:

- a durable readiness lookup row keyed by scope, generation, domain, and
  acceptance unit;
- a materialized readiness bitset or compact status row updated by the phase
  publisher and repairer;
- a claim-side join against a bounded readiness table whose row count is stable
  relative to domain count;
- precomputed per-domain claim eligibility that keeps retry and dead-letter
  ownership in the reducer queue.

Every direction still needs stale-state, duplicate-delivery, expired-claim,
partial-readiness, and replay proof. A cache alone is not sufficient unless it
has durable invalidation and safe fallback behavior.

## Operator Signals

Operators must be able to diagnose claim pressure without reading SQL. A future
runtime change needs either new signals or a tracked explanation that existing
signals are enough.

Use these signals first:

- `eshu_dp_queue_claim_duration_seconds{queue="reducer"}` for claim duration;
- `eshu_dp_postgres_query_duration_seconds{store="queue"}` for claim SQL cost;
- `eshu_dp_reducer_queue_wait_seconds` for visible queue age before execution;
- `eshu_dp_reducer_batch_claim_size` where batch claiming is active;
- `/admin/status` reducer backlog, retry, and dead-letter state;
- reducer execution logs with domain, worker, status, and failure class;
- Postgres query spans for the reducer queue store.

If those signals cannot distinguish claim-query cost, readiness blocking,
conflict-domain blocking, worker saturation, and handler duration, the
implementation must add bounded telemetry before changing the runtime path.

## Verification Gate

Use the smallest gate that proves the touched boundary.

| Change | Required gate |
| --- | --- |
| Contract or docs only | Strict MkDocs build, `git diff --check`, performance-evidence guard scripts, and sensitive-string scan. |
| Claim SQL or queue DDL | Focused storage tests, `BenchmarkReducerQueueClaimDeepQueue` before and after, performance-evidence guard scripts, and same-shape no-regression evidence. |
| Readiness materialization | Storage tests for stale, missing, partial, duplicate, expired, replayed, and dead-lettered work; reducer tests for domain ordering; status or telemetry proof. |
| Worker or batch behavior | Contention, retry, idempotency, ordering, expired-claim reclaim, and dead-letter proof under intended worker and replica counts. |

No-Regression Evidence: this contract changes documentation only. It adds no
reducer claim SQL, graph write, queue worker, lease behavior, readiness store,
runtime knob, schema DDL, metric, span, log field, or status field.

No-Observability-Change: the contract names required future signals but uses
the existing reducer queue claim duration, Postgres queue query duration,
reducer queue wait, batch claim size, `/admin/status`, and reducer logs as the
current diagnostic surface.

## Search-Document Claim Priority Evidence

Benchmark Evidence: #2627 changes only the reducer batch-claim ordering for
ready `eshu_search_document` work so derived search-document catch-up runs
after other ready reducer domains. The baseline remote proof shape was a
NornicDB full-corpus run at commit `127ec4862927981b75ef8b99b73a55cb088a02fa`
with 896 candidate roots / 896 Git roots, zero failed/dead-letter rows, and a
tail where four long `eshu_search_document` claims left visible
`workload_materialization`, `deployment_mapping`, and other graph/materializer
rows pending. After the query change, reduced local Postgres 18-alpine benchmark
cases on Darwin arm64 with
`ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES=1000:1000:1,1000:5000:4` and
`-benchtime=1x` measured `BenchmarkReducerQueueClaimReadinessGateGrowth` at
`55525874 ns/op`, `34016 B/op`, `144 allocs/op` for 1,000 queue rows / 1,000
phase rows / one gated domain and `54074667 ns/op`, `34016 B/op`,
`144 allocs/op` for 1,000 queue rows / 5,000 phase rows / four gated domains.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestClaimBatch|TestReducerQueueClaim.*Domain|TestClaimBatchCanFilterByMultipleDomains|TestReducerQueueClaim.*Readiness|TestReducerQueueBatch'
-count=1`, `go test ./internal/storage/postgres -count=1`, and
`go test ./internal/reducer -count=1` prove the batch claim keeps readiness
gates, same-conflict representative selection, domain allowlists, lease
fencing, and reducer service behavior intact while graph/materialization truth
work is ordered before derived search-document catch-up.

No-Observability-Change: #2627 adds no table, queue state, runtime knob, route,
metric, span, log field, API/MCP response field, or graph write. Operators keep
diagnosing this path through existing reducer execution logs, queue status,
claim-duration metrics, batch-claim-size metrics, Postgres query spans, and
`eshu_dp_postgres_query_duration_seconds`.

## Related Docs

- [Resolution Engine](../services/resolution-engine.md)
- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Profiling And Concurrency](local-testing/profiling-and-concurrency.md)
- [Telemetry Overview](telemetry/index.md)
- [Reducer And Storage Metrics](telemetry/metrics-reducer-storage.md)
