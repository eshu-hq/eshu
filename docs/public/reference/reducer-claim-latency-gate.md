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

## Per-Handler Materialization Budget

Splitting reducer files (Epic D) does not make materialization faster — the
per-handler cost is the same whether the handlers live in one package or many.
B-9 (#3802) locks that cost with an absolute per-handler latency budget so a
materialization or correlation handler regression is caught on `main`
regardless of the file or monorepo layout. This is complementary to the
claim-latency budget above (which governs the queue claim SQL) and to the
benchstat-relative bench-regression gate (B-2 / #3795, a >10% sec/op drift vs a
committed baseline). The per-handler gate instead asserts each handler stays
under a fixed `ns/op` ceiling.

The budgeted benchmarks are the credential-free, in-process reducer extractors
in `go/internal/reducer` — they run pure handler logic over fixture facts with
no Postgres or NornicDB, so the gate is hermetic on a plain runner. The
backend-bound `BenchmarkReducerQueueClaimDeepQueue` claim benchmark is governed
by the claim-latency budget above, not this table.

The committed budgets and their measured baseline live in
`testdata/benchmarks/reducer-handler-budgets.txt`. Each ceiling is
`round(median_baseline * 1.50)`. The 1.50x headroom is wider than the
claim-latency budget's 1.10x on purpose: this is a single-run absolute ceiling
evaluated on a shared CI runner of a different class than where the baseline was
captured (darwin/arm64, Apple M4 Pro, `-benchtime=100ms -count=6`), so it must
absorb cross-class CPU difference and per-run noise without flaking.

| Handler benchmark | Baseline (ns/op, median) | Budget (ns/op) |
| --- | --- | --- |
| `BenchmarkExtractCloudResourceNodeRows` (AWS node) | 10,741,623 | 16,100,000 |
| `BenchmarkExtractGCPCloudResourceNodeRows` (GCP node) | 10,550,352 | 15,800,000 |
| `BenchmarkExtractKubernetesWorkloadNodeRows` (k8s workload node) | 5,413,844 | 8,100,000 |
| `BenchmarkExtractAWSRelationshipEdgeRows` (AWS edge) | 24,690,270 | 37,000,000 |
| `BenchmarkExtractGCPRelationshipEdgeRows` (GCP edge) | 27,680,020 | 41,500,000 |
| `BenchmarkExtractKubernetesCorrelationEdgeRows` (k8s correlation edge) | 9,174,514 | 13,800,000 |
| `BenchmarkSecretsIAMGCPGrantObservations` (secrets/IAM trust chain) | 5,546,848 | 8,300,000 |
| `BenchmarkBuildServiceCatalogCorrelationDecisionsHighCardinalityFanout` (service-catalog correlation) | 6,120,469 | 9,200,000 |
| `BenchmarkValueFlowFixpointFull` (value-flow fixpoint, cold) | 20,427,691 | 30,600,000 |
| `BenchmarkValueFlowFixpointIncrementalCached` (value-flow fixpoint, cached) | 11,982,886 | 18,000,000 |

`scripts/verify-reducer-perf-gate.sh` runs exactly these benchmarks
credential-free, takes the median `ns/op` across samples (the same statistic the
budgets were derived from), and reports any handler whose median exceeds its
ceiling. The CI job `reducer-perf-gate` in `.github/workflows/bench.yml` runs it
on every pull request and on push to `main`. The contract mirror
`scripts/test-verify-reducer-perf-gate.sh` exercises the median parser and breach
logic against synthetic results without running any benchmark.

The committed baseline was captured on darwin/arm64, not the CI `ubuntu-latest`
runner class, so an absolute single-run ceiling there is only a like-for-like
comparison once the baseline is recaptured on the enforcement runner. Until then
the gate runs **advisory** (`REDUCER_PERF_ENFORCE=false`: it reports a breach but
does not fail the job) — the same shared-runner-variance reasoning as the B-2
bench-regression gate. Recapture the budgets on `ubuntu-latest` with
`scripts/refresh-reducer-handler-budgets.sh`, review the diff, then flip the
workflow to `REDUCER_PERF_ENFORCE=true` to make a breach blocking. A breach must
be fixed at the handler or proven to be an intended cost and re-baselined with a
reviewed diff; it must not be hidden by widening the budget on an unexplained
regression.

## Projection-Tail Backlog Target

The materialization phase is the git-collector end-to-end long pole (#3624): the
emission phase front-loads tens of thousands of materialization intents and the
reducer drains them slower than they arrive, producing multi-minute intent waits
and a long projection tail before any repository reaches a fully-projected
state. The per-handler budget above keeps each handler cheap; this target keeps
the drain from silently regressing into an unbounded tail.

The projection-tail backlog target is a same-harness, same-corpus drain
contract, measured on the remote full-corpus harness (never started on the full
corpus — use the small proof ladder first per `eshu-diagnostic-rigor`):

- After collection completes (`source_local` succeeded), the materialization
  domains (`deployment_mapping`, `workload_materialization`, `workload_identity`,
  `inheritance_materialization`, `sql_relationship_materialization`,
  `shell_exec_materialization`, `deployable_unit_correlation`,
  `code_call_materialization`) must reach queue-zero pending, with zero
  dead-letter rows, within the known-normal drain band for the harness.
- The drain band is the recorded materialization wall-clock from the last
  green full-corpus run on the same harness and backend; a run more than 10% or
  60 seconds worse than that band must stop and profile before merge.
- `intent_wait_seconds` (resolution-engine cycle) is the projection-tail signal:
  it must trend toward zero as the tail drains, not plateau in the multi-minute
  range that #3624 captured (~1,526 s).
- A cycle reporting `written_rows: 0` while domains remain pending is a
  readiness/ordering signal, not progress, and must be classified (upstream
  inputs not ready vs genuinely empty work) before any worker change.

This target is a drain contract for full-corpus proof runs, not a CI micro-gate:
the per-handler budget is the hermetic CI assertion; the projection-tail target
is validated on the remote harness and recorded in the active ADR with the
fields the evidence ladder requires (collector-complete, projection-complete,
and queue-zero as separate timings, plus queue counts, retrying, dead letters,
commit id, backend, and clean-volume state).

## #3710 / #3725 Baseline Win

The deferred relationship-evidence backfill's **fact-LOAD** query was the
dominant at-scale long pole feeding materialization readiness (#3710): a single
`O(facts × catalog)` sequential scan over the whole latest-generation fact set
with a per-row self-exclusion regex over `lower(payload::text)`. On a de-nested
896-repo run (~3.5M facts, NornicDB) it ran ~20 min+ serially and blocked the
whole relationship-backfill phase. PR #3725 (#3710) partitioned the load per
`(scope_id, generation_id)` across the deferred-maintenance worker pool, added a
partial `pg_trgm` GIN index on the `$1` arm inside a `MATERIALIZED` CTE, and
replaced the `$2` self-exclusion boundary regex with an escaped, truth-equivalent
`LIKE` superset refined by the in-memory `catalogMatcher`.

This is the win B-9 locks as the before/after baseline:

Benchmark Evidence: `BenchmarkDeferredBackfillDiscovery{Full,Scoped}Fleet{1k,5k}`
in `go/internal/relationships` (in-memory `DiscoverEvidence` over the
representative fleet corpus whose payloads carry `repo_id`), Apple M-series:
fleet 1k `27,748,503 ns/op` / `399,019 allocs/op` Full -> `6,509,206 ns/op` /
`55,916 allocs/op` Scoped (**4.3x faster, 8.6x fewer allocs**); fleet 5k
`122,223,287 ns/op` / `218,460,600 B/op` Full -> `31,142,259 ns/op` /
`25,836,605 B/op` Scoped (**3.9x faster, 8.4x fewer bytes**). Full-corpus
wall-clock (896 repositories, 3,501,443 `fact_records`, 207,003 loaded
queried-kind facts, PostgreSQL 18 + NornicDB, deferred-backfill concurrency 8,
PR #3729 measurement): deferred relationship backfill **882 s (~14.7 min)**, down
from the pre-#3710 ~36 min+ single-scan long pole (**~2.4x wall-clock**), fanned
out across 896 `(scope_id, generation_id)` partitions / 8 workers. The honest
residual is the giant-repository `$2` self-exclusion tail (slowest single
per-scope query ~8 min), tracked by #3711; the per-handler budget table above
does not regress on that residual because the budgeted handlers are the
in-process extractors, not the backend fact-load.

No-Regression Evidence: this section records #3710/#3725 as the locked baseline
win; the numbers are the merged measurements from `go/internal/relationships`
(`BenchmarkDeferredBackfillDiscovery*`) and PR #3729's full-corpus run, not new
measurements. The B-9 per-handler gate's own current run on this branch
(darwin/arm64, `-benchtime=100ms -count=6`) measured every budgeted handler
within budget; see the Per-Handler Materialization Budget table.

No-Observability-Change: B-9 adds a CI gate, a budget artifact, a verifier, a
refresh script, and this documentation only. It adds no reducer claim SQL, graph
write, queue worker, lease behavior, readiness store, runtime knob, schema DDL,
metric, span, log field, or status field. Operators keep diagnosing
materialization throughput and the projection tail through the existing reducer
queue claim duration, Postgres queue query duration, reducer queue wait, batch
claim size, resolution-engine `intent_wait_seconds`, `/admin/status` backlog,
retry, and dead-letter state, and the deferred-backfill partition/load
instruments (`DeferredBackfillPartitions`, `DeferredBackfillPartitionWorkers`,
`DeferredBackfillPartitionLoadDuration`, `DeferredBackfillIndexBuildDuration`).

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

## Code-Call Refresh Fence Evidence

No-Regression Evidence: #2626 changes only code-call partition selection
fencing. The regression input shapes are a same-run earlier file partition
followed by a later whole repo refresh, and a `BatchLimit=1` domain page where a
same-run covering file refresh sorts outside the current
`ListPendingDomainIntents` page. `go test ./internal/reducer -run
'TestCodeCallProjectionRunner(LaterWholeRefreshDoesNotBlockEarlierFilePartition|ScansAcceptanceUnitForCoveringRefreshBeyondDomainPage)'
-count=1` failed before the fence fix and passed after it. `go test
./internal/reducer -count=1` proves the unchanged reducer package contract,
including partition leases, whole-scope fencing, refresh retraction, and
current-run partition history behavior.

No-Observability-Change: #2626 adds no graph write, queue SQL, worker count,
runtime knob, route, metric, span, log field, API/MCP response field, or schema
DDL. Operators keep diagnosing code-call partition progress through existing
partition lease timing, selection timing, blocked-readiness counters,
processed/retracted/upserted row counts, shared-intent backlog/status rows, and
edge writer counters.

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
