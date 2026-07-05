# NornicDB Tuning

Use this page when `ESHU_GRAPH_BACKEND=nornicdb` and a proof exposes a graph
write timeout, slow phase, or backend compatibility gate. It is the knob map,
not the install or operations runbook.

Tune from evidence: identify phase, label, row count, grouped statement count,
timeout shape, and queue state before changing the narrowest matching knob. For
local start/stop/install commands, use
[Graph Backend Installation](graph-backend-installation.md) and
[Graph Backend Operations](graph-backend-operations.md). For query-shape rules,
use [Cypher Performance Discipline](cypher-performance.md). For the proof behind
current defaults, use
[NornicDB Tuning Evidence](nornicdb-tuning-evidence.md).

## First Decision

| Symptom | First place to look | Do not start with |
| --- | --- | --- |
| `graph_write_timeout` with phase and label | Canonical or semantic write budget below | Reducer worker count |
| Queue backlog with workers busy | `/admin/status`, queue age, graph-write durations | Full-corpus rerun |
| Slow `content writer stage completed` with `upsert_entities` dominating | Content-store tuning below | NornicDB graph knobs |
| Code-call acceptance cap | Discovery advisory and code-call scan guard | Graph write timeout |
| NornicDB CPU-bound at startup | Search-index persistence and embeddings below | Restart loops |
| Tiny row count still slow | Query shape, schema/index presence, NornicDB hot-path eligibility | Lowering broad concurrency |

## Validation Ladder

Do not use the full corpus as the first debugging loop.

1. Re-run only the failing repo with fresh `ESHU_HOME`, rebuilt Eshu binaries,
   and the exact NornicDB binary under evaluation.
2. If a graph timeout is the only blocker and the statement is plausibly
   correct, raise `ESHU_CANONICAL_WRITE_TIMEOUT` only for that
   correctness-validation lane so later graph/query failures can surface.
3. Confirm the run drains with `pending=0`, `in_flight=0`, no failed rows, and
   no dead letters.
4. Run a medium corpus of representative repos.
5. Run the full corpus only after focused and medium lanes pass.

A larger timeout can prove correctness. It is not the final performance answer
until phase timing and write-shape evidence justify the larger budget.

For full-corpus or remote proof, record commit/image, schema/bootstrap state,
clean-volume state, pprof state, effective environment, terminal queue counts,
and API/MCP truth checks. Switch to Neo4j only for compatibility verification,
not to explain an unclassified NornicDB slowdown.

## Canonical Write Budget

These knobs affect source-local canonical graph writes from ingester and
bootstrap-index.

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` on NornicDB | Bounds each NornicDB graph execution with a client context deadline and Bolt transaction timeout. Raise only for focused validation or proven cloud latency. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | Broad grouped-statement cap for phases without a narrower cap. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | Grouped-statement cap for `phase=files`. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | Rows per file-upsert statement. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | Grouped-statement cap for canonical entity phases before label-specific caps apply. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | Default rows per canonical entity statement before label-specific caps apply. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | Label-specific row caps for canonical entity writes. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | Label-specific grouped-statement caps. |
| `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` | `NumCPU`, clamped to `16` | Parallel chunk dispatch for canonical `entities` and `entity_containment` phases. Set to `1` only for a serial comparison. |
| `ESHU_CANONICAL_RETRACT_BATCH` | `2000` | Nodes deleted per iteration of the bounded full-refresh retract drain loop (File, Directory, and Entity canonical retracts on NornicDB). Valid range: `1`–`10000`. Lower when a single drain iteration approaches the NornicDB write budget on very large repos (e.g. if `graph_write_timeout` appears on a retract statement at corpus scale). Does not change worker counts or per-statement timeouts. |

Two dimensions matter:

- `*_BATCH_SIZE` controls rows inside one statement.
- `*_PHASE_GROUP_STATEMENTS` controls how many statements run in one grouped
  Bolt transaction.

Effective grouped row pressure is roughly:

```text
label row batch size * label grouped statement cap
```

For example, `Variable=100` and `Variable=5` means one grouped execution can
carry about 500 Variable rows. Raising the grouped-statement cap to `25` moves
that pressure toward 2,500 rows without changing the row-batch knob.

`ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` is a separate axis. It controls how
many grouped transactions run in parallel. Peak Bolt session demand is roughly:

```text
bootstrap-index: ESHU_PROJECTION_WORKERS * ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY
ingester: ESHU_PROJECTOR_WORKERS * ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY
```

Pin concurrency lower only when NornicDB shows parallel commit contention.

## Semantic And Shared Reducer Budget

These knobs affect reducer-owned materialization and shared projection.

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_REDUCER_WORKERS` | `NumCPU` on NornicDB | Reducer intent worker count. Lower only when conflict-domain fencing still shows backend saturation or graph write conflicts. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | worker count on NornicDB | Reducer intents claimed per poll. Keep near worker count so claimed work starts heartbeat-protected execution promptly. |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | unset / disabled | Optional cap on cross-scope semantic entity claims after the source-local drain gate opens. Set only from focused backend-saturation evidence. |
| `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES` | `Annotation=5,Function=10,ImplBlock=10,Module=10,TypeAlias=5,TypeAnnotation=50,Variable=10` | Label-specific row caps for semantic entity materialization. |
| `ESHU_SHARED_PROJECTION_WORKERS` | runtime default is `NumCPU` capped at `4`; Compose defaults to `4`; Helm sets `8` | Shared projection partition workers. Raise only when queue age grows and graph backend health is proven. |
| `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT` | `8` | File-scoped CALLS projection partition count. Change only between clean drains or after active leases expire. |
| `ESHU_CODE_CALL_PROJECTION_WORKERS` | `4` | Concurrent file-scoped CALLS partition workers, clamped by partition count. Raise only with queue and graph-write evidence. |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | Correctness guard for complete accepted repo/run scan before rewriting CALLS edges. |

Semantic materialization is reducer-owned. Tune semantic labels only after
timeout summaries name the semantic label and row count. Use the claim cap only
when distinct-scope semantic writes still saturate NornicDB after source-local
drain and conflict-domain fencing are working.

`ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` is not a graph-write tuning
knob. Increase it only when reducer logs name an acceptance-scan cap and a
discovery advisory proves the volume is authored source that should remain in
the graph.

## Producer Write-Timeout Backpressure

When the graph backend is slow, individual canonical writes time out. Eshu
already converts a recoverable timeout into a requeue rather than a dead letter:
`go/internal/storage/cypher/canonical_node_writer.go` wraps a transient write
error (`*neo4j.TransactionExecutionLimit`, `*neo4j.ConnectivityError`, the typed
NornicDB commit-time `UNIQUE` codes, and `GraphWriteTimeoutError`, which reports
`Retryable() == true`) with `WrapRetryableNeo4jError`, so the projector/reducer
queue records `projection_retryable` and requeues the item into the `retrying`
state with `ESHU_*_RETRY_DELAY` backoff, bounded by `ESHU_*_MAX_ATTEMPTS`.

That per-item path stops a single slow write from dead-lettering, but it does not
slow the producer. While the backend stays slow, the ingester keeps enqueuing new
reducer intents, graph-write-timeout retrying depth climbs, and items eventually
exhaust their attempt budget and dead-letter as `retry_exhausted` (the #3560 /
376-224 backlog shape). The producer-side gate below closes that loop.

The pressure signal is **failure-class-scoped**: it counts only reducer rows that
are retrying with `failure_class = graph_write_timeout`. A graph-write retry —
whether a bounded-deadline `GraphWriteTimeoutError` or a transient driver retry
wrapped by `WrapRetryableNeo4jError` (`*TransactionExecutionLimit`,
`*ConnectivityError`, retryable Neo4j codes) — self-classifies into this class and
is preserved on the retrying row by the reducer queue. Reducer *readiness*
backlogs also persist as `retrying` rows (for example
`secrets_iam_endpoint_not_ready` and the other `*_not_ready` cross-scope readiness
misses), but they keep their own non-graph failure class and are therefore
excluded from the signal. Scoping to `graph_write_timeout` is what stops a large
readiness backlog from false-throttling unrelated admission while the graph
backend is healthy.

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` | `500` | Defers ingester reducer-intent admission once the count of reducer rows retrying with `failure_class = graph_write_timeout` reaches this value. Set to `0` to disable the graph-write backpressure gate. |
| `ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK` | `100` | Hysteresis floor: admission resumes only after the graph-write-timeout retrying depth falls below this value. Must be less than the high-water mark. An unset or out-of-range value is clamped to one fifth of the high mark. |

### Bootstrap-Index In-Flight Canonical Gate (Issue #4515, Lane B)

`bootstrap-index` supports the same in-flight canonical write gate the reducer
and projector use (`go/internal/graphbackpressure`), bounding concurrent
canonical NornicDB writes so a slow backend holds its permits longer instead of
being hit by every worker's write at once. It reuses the existing knobs:

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` | `8` (shipped Compose/Helm default; code default is unset/passthrough) | Shared concurrent-write ceiling. bootstrap-index reads it as the fallback for its canonical class, same as the reducer and projector. Setting this alone to a positive value ENABLES the gate for bootstrap-index's canonical class; it is not a no-op knob. Set to `0` for legacy unbounded (passthrough). |
| `ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` | unset | Canonical-class ceiling; takes precedence over the shared knob for bootstrap-index's single write class. Leaving this unset does NOT by itself disable the gate — it only falls back to the shared knob above. |

The gate is a passthrough (no bound, inner executor unchanged) only when
**both** knobs are unset or non-positive (the library-level code default). The
shipped Compose and Helm deployments set `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=8` so
the gate is bounded out of the box; set it to `0` to restore the legacy
unbounded behavior.

**Why 8 (issue #4456 / #3624).** A measured NornicDB concurrent-writer sweep
(N writers issuing the canonical entity-upsert shape, 8s holds) showed write
throughput peaks near 12-16 concurrent writers then *collapses* — beyond the
knee, more concurrency yields less throughput and p99 latency climbs to the
`ESHU_CANONICAL_WRITE_TIMEOUT`. Unbounded, a concurrent bootstrap+reducer write
storm blows past the knee: a full 909-repo clean-volume E2E ran 100 failed
canonical projections and 55 dead-lettered work items with the gate unset,
versus 13 and 3 at `=8` (bootstrap did not hit the rc=1 catastrophic failure).
A per-writer ceiling of 8 keeps the two writers that run concurrently in the
measured E2E (bootstrap-index + reducer) at a combined in-flight ≈ 16 = the top
of the zero-timeout plateau. This is a **per-process** bound, not a global
cross-process ceiling — each gated writer (reducer, projector, bootstrap-index)
independently bounds its own in-flight writes to N; a truly global budget across
processes is a larger design tracked separately. Operators on a backend with
more write headroom should raise it; the goal is to sit at the knee, not below
it.

Bootstrap-index's canonical NornicDB executor
(`bootstrapNornicDBPhaseGroupExecutor` in `go/cmd/bootstrap-index/nornicdb_wiring.go`)
implements `cypher.PhaseGroupExecutor` (`ExecutePhaseGroup`), and for the
`entities`/`entity_containment` phases its `ExecutePhaseGroup` fans a single
call out into up to `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` concurrent
`ge.ExecuteGroup` calls against its inner `cypher.GroupExecutor`
(`executeEntityPhaseGroupConcurrently`). Wrapping the gate around that outer
`ExecutePhaseGroup` call would acquire only ONE permit per call, leaving every
concurrent inner `ExecuteGroup` call in the fan-out unbounded —
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=2` would still allow up to
`ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` (default up to 16) simultaneous Bolt
writes. `bootstrapCanonicalExecutorForGraphBackend` therefore wraps the gate
around the INNER `GroupExecutor`-capable layer (`bounded`, the
`TimeoutExecutor` over the instrumented/retrying raw executor) BEFORE
assigning it to `bootstrapNornicDBPhaseGroupExecutor.inner`, so every
concurrent `ge.ExecuteGroup` call the fan-out makes independently draws a
permit from the same shared gate, bounding actual concurrent backend writes
regardless of `entityPhaseConcurrency`. The same wrap applies to the
non-NornicDB (Neo4j) return path, which is a bare `GroupExecutor` with no
fan-out wrapper. Because the wrap sits inside `bounded` (which already
includes the `ESHU_CANONICAL_WRITE_TIMEOUT` deadline), a permit-wait does not
extend that per-write timeout budget beyond what the inner
`TimeoutExecutor`/retry stack already bounds.

The gate itself is constructed exactly ONCE per bootstrap-index run
(`newBootstrapCanonicalGate` in `go/cmd/bootstrap-index/graph_write_backpressure_wiring.go`,
called from `openBootstrapCanonicalWriter` in `go/cmd/bootstrap-index/wiring.go`)
and threaded into `bootstrapCanonicalExecutorForGraphBackend` as a single
shared instance, so the ceiling bounds in-flight writes across every
`projector.Service` worker goroutine for the whole run, not per-worker.

No-Regression Evidence: `go test ./internal/storage/cypher
./internal/graphbackpressure ./cmd/bootstrap-index ./cmd/reducer -race
-count=1` (930 tests, 4 packages) proves the reducer's and projector's existing
`GroupExecutor`-based wiring is unchanged while bootstrap's inner-layer wiring
correctly bounds writes. Peak-concurrency proof:
`TestBootstrapCanonicalGateBoundsConcurrentEntityFanOut`
(`go/cmd/bootstrap-index/nornicdb_canonical_gate_fanout_test.go`) drives
`ExecutePhaseGroup` with `entityPhaseConcurrency=8` and a gate ceiling of 2,
and asserts peak concurrent inner `ExecuteGroup` calls reaches exactly 2
(before the inner-layer fix: unbounded up to 8; after: capped at, and reaching,
2), with every chunk still executing — this is the confirmed-bug regression:
gating the outer `ExecutePhaseGroup` call alone would still show unbounded
concurrency at the inner `ExecuteGroup` layer.
`TestBootstrapCanonicalGateTerminatesUnderMixedFanOutPressure` drives a mix of
succeeding and failing concurrent chunks at a ceiling of 3 and asserts the
whole `ExecutePhaseGroup` call terminates within a bounded deadline, proving
the permit releases on both the success and the error path so a saturated
pool cannot deadlock.
`TestBootstrapCanonicalGateDisabledFanOutIsUnbounded` proves the default (nil
gate) path lets the fan-out reach its full configured
`entityPhaseConcurrency`, matching pre-existing behavior with the gate
disabled.

Observability Evidence: bootstrap-index's gate reuses the existing
`eshu_dp_graph_write_backpressure_engaged_total{gate="canonical"}` counter and
`eshu_dp_graph_write_backpressure_wait_seconds{gate="canonical"}` histogram; no
new metric, span, or log field is introduced.

The gate reuses the existing admission loop in
`go/cmd/ingester/reducer_admission.go` and the
`eshu_dp_reducer_admission_deferrals_total` counter. The failure-class-scoped
count comes from `QueueObserverStore.ReducerGraphWriteTimeoutDepth`, a query that
filters retrying reducer rows to `failure_class = graph_write_timeout` (reusing
the active-generation CTE so superseded stale rows do not inflate the signal). A
deferral carries a `reason` attribute (`graph_write_pressure` when the scoped
graph-write-timeout depth tripped the gate, `high_water` when total outstanding
depth tripped the original gate) and a `failure_class` attribute naming the class
that drove a graph-write-pressure deferral. The hysteresis flag is shared and
mutex-guarded across concurrent producer Enqueue calls, so the producer pauses
and resumes as one unit instead of flapping per worker.

This is bounded admission, not serialization: worker counts, batch sizes, and
concurrent writers are unchanged. When the backend recovers, graph-write-timeout
retrying depth drains below the low-water mark and full producer throughput
resumes automatically.

### Root cause of the recurring write/retract timeouts

The recurring NornicDB write timeouts trace to commit-time work on canonical
`MERGE` writes under concurrent producers, not to a missing Eshu retry. Two
NornicDB behaviors documented in
[NornicDB Pitfalls](nornicdb-pitfalls.md) compound under load:

- Concurrent `MERGE` on the same `uid` can lose at commit with a
  `TransactionCommitFailed` `UNIQUE` violation; the driver's internal
  `session.ExecuteWrite` retries this for up to its deadlock budget (about 30s)
  before surfacing `*TransactionExecutionLimit`. Under sustained fan-in, that
  budget is consumed and the client-side `ESHU_CANONICAL_WRITE_TIMEOUT`
  (`30s` local, `120s` Helm) elapses, producing a `graph_write_timeout`.
- Relationship `MERGE` commit cost is dominated by start-node outgoing-fanout
  existence checks and constraint validation, so commit latency rises with graph
  size even at a fixed batch size.

The correct fix is therefore twofold and already partly in place: keep the
backend safety net (`RetryingExecutor` + `WrapRetryableNeo4jError`) so a
recoverable timeout requeues instead of dead-lettering, and add producer
backpressure so a slow backend slows new admission before the retrying bucket
exhausts its attempt budget. Lowering worker counts is explicitly rejected as a
fix: it hides the contention rather than absorbing it, and the project rule
"Serialization Is Not A Fix" applies.

No-Regression Evidence: `go test ./cmd/ingester ./internal/storage/cypher
./internal/storage/postgres -count=1 -race` proves the gate is failure-class
scoped. A readiness backlog of 800 `*_not_ready` retrying rows with zero
graph-write-timeout depth does NOT throttle
(`TestReducerAdmissionReadinessBacklogDoesNotThrottle`), while a graph-write-timeout
backlog above the high-water mark does
(`TestReducerAdmissionGraphWriteTimeoutBacklogThrottles`), holds through the
high/low hysteresis gap, resumes on recovery, records the
`graph_write_timeout` failure class on the deferral
(`TestReducerAdmissionGraphWritePressureRecordsFailureClass`), and loses no
intents under 16 concurrent producers. The reducer queue preserves
`graph_write_timeout` on the retrying row
(`TestReducerQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget`) while a
readiness miss keeps its own class
(`TestReducerQueueFailRetriesReadinessBacklogKeepsOwnFailureClass`); the scoped
depth query excludes `reducer_retryable` and readiness classes
(`TestReducerGraphWriteTimeoutDepthFiltersFailureClass`). The disabled gate keeps
the original single-read fast path (`BenchmarkReducerAdmissionDisabled`,
`BenchmarkReducerAdmissionBelowHighWater`).

Observability Evidence: producer deferrals increment
`eshu_dp_reducer_admission_deferrals_total{reason="graph_write_pressure",failure_class="graph_write_timeout"}`
and emit a `WARN` log naming the `reason`, `failure_class`, scoped depth, both
water marks, and the poll interval, so an operator can confirm at 3 AM that graph
writes are timing out (not a readiness backlog) before the producer backs off.
The graph-write-timeout retrying depth is exposed by
`QueueObserverStore.ReducerGraphWriteTimeoutDepth`, and per-item requeues keep the
existing `eshu_dp_neo4j_deadlock_retries_total` counter.

## Content-Store Tuning

Content writes are Postgres work, not NornicDB graph work.

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_CONTENT_ENTITY_BATCH_SIZE` | `300`, valid `1..4000` | Rows per Postgres `content_entities` upsert statement. Use only after content writer logs show `upsert_entities` dominates. |
| `ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES` | unset / `false` | Local-authoritative bulk-load knob that defers expensive content trigram search indexes and rebuilds them after clean drain. Do not use as a deployed Postgres schema default. |

If changing `ESHU_CONTENT_ENTITY_BATCH_SIZE` changes statement count but not
wall time, inspect source-cache size and trigram index maintenance before
raising the batch again.

## Compatibility And Conformance Switches

| Variable | Default | Use |
| --- | --- | --- |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | unset / `false` | Conformance-only switch. On NornicDB it is honored as per-dependency-phase commits, not a single grouped transaction: whole-materialization atomic canonical writes are unsupported because an `UNWIND`-driven `MATCH` cannot see a same-transaction `MERGE`, which silently drops nested files (#4027). Leave unset for normal runs. |
| `ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK` | unset / `false` | Test gate that requires grouped-write rollback conformance. |
| `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT` | unset / `true` | Cross-file batched entity containment. Set `false` only for measured fallback comparisons against the older file-scoped shape. |

Do not disable batched entity containment because one run is slow. The default
row-scoped containment shape has repo-scale correctness proof. Fallback
comparisons must capture statement count, label summaries, retries, dead
letters, and terminal drain state.

## NornicDB Process Diagnostics

| Variable | Default | Use |
| --- | --- | --- |
| `NORNICDB_PPROF_ENABLED` | unset / `false` | Enables NornicDB profiling when Eshu logs no longer identify an Eshu-side batching mistake. |
| `NORNICDB_PPROF_LISTEN` | `127.0.0.1:9091` | Bind address for pprof. Use `0.0.0.0:9091` only on trusted private test hosts. |
| `NORNICDB_SEARCH_BM25_ENABLED` | `false` in Eshu Compose and Helm | Keeps BM25 indexing off for the canonical graph database. |
| `NORNICDB_SEARCH_VECTOR_ENABLED` | `false` in Eshu Compose and Helm | Keeps vector indexing off for the canonical graph database. |
| `NORNICDB_SEARCH_BM25_WARMING` | `lazy` in Eshu Compose and Helm | Uses the supported lazy trigger if BM25 is enabled for a deliberate search proof. |
| `NORNICDB_SEARCH_VECTOR_WARMING` | `lazy` in Eshu Compose and Helm | Uses the supported lazy trigger if vector search is enabled for a deliberate search proof. |
| `NORNICDB_PERSIST_SEARCH_INDEXES` | `false` in Eshu Compose and Helm | Keeps disabled search indexes from creating graph-lane restart artifacts. |
| `NORNICDB_EMBEDDING_ENABLED` | `false` in Eshu Compose and Helm | Keeps embedding generation off during Eshu indexing. Enable only for semantic-search experiments after indexing baseline is understood. |

### Search Index Gate

Treat disabled BM25/vector indexing as the normal canonical graph deployment.
Eshu's graph lane owns canonical truth; BM25, vector, and hybrid retrieval need
a curated search projection with its own proof.

Do not re-enable BM25/vector indexing in Eshu until a focused proof records
build state, duration, document count, vector count, artifact size, and failure
class before changing Compose or Helm
defaults.

## Hosted Defaults

The Helm chart owns hosted defaults for NornicDB container flags, probes,
`ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and
`ESHU_SHARED_PROJECTION_WORKERS=8`. See
[Helm Runtime Values](../deploy/kubernetes/helm-runtime-values.md) before
changing chart values and [NornicDB Tuning Evidence](nornicdb-tuning-evidence.md)
before changing their proof trail.

## Add A New Knob

Add another NornicDB-specific Eshu environment variable only after this proof:

1. Capture a timeout or slow-phase log that names phase, label, row count,
   grouped statement count, and duration.
2. Prove whether the failure is statement width, row width, query shape,
   missing NornicDB functionality, schema/index state, or machine pressure.
3. Prefer fixing NornicDB when Eshu is missing a Neo4j-equivalent primitive
   that belongs in the database.
4. Add the narrowest Eshu adapter seam only when evidence shows an Eshu-side
   shape or bounded budget is the right fix.
5. Update this page, [Local Testing](local-testing.md), and affected
   package-local docs in the same PR.

One-row or very-low-row statements that are still slow usually point to query
shape, schema/index setup, or backend hot-path eligibility. Confirm those
before lowering global workers.

## Related Docs

- [NornicDB Tuning Evidence](nornicdb-tuning-evidence.md)
- [NornicDB Pitfalls](nornicdb-pitfalls.md)
- [Graph Backend Installation](graph-backend-installation.md)
- [Graph Backend Operations](graph-backend-operations.md)
- [Backend Conformance](backend-conformance.md)
- [Cypher Performance Discipline](cypher-performance.md)
- [Environment Variables](environment-variables.md)
