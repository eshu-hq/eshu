# Local Eshu Service Performance Envelope

This page defines the expected local performance envelope. Local Eshu should be
useful on a normal developer laptop, not only on ideal hardware or empty
repositories.

Reference hardware is an Apple Silicon or mid-range x86 laptop with at least
4 cores and 16 GB RAM.

## Target Profiles

| Profile | Runtime shape | Target |
| --- | --- | --- |
| `local_lightweight` | Eshu plus embedded Postgres | cold start under `5s`; warm restart under `2s`; exact symbol lookup p95 under `500ms`; content search p95 under `800ms`; complexity query p95 under `1500ms`; single-file reindex to visible search update under `2s`. |
| `local_authoritative` | Eshu plus embedded Postgres and embedded NornicDB | cold start under `15s`; warm restart under `5s`; transitive caller and call-chain p95 under `2s` on an active repo; active-repo dead-code scan under `10s`; reducer bulk write batch under `10s` for `50K` facts; single-file reindex to visible graph update under `5s`. |

Warm restart means the same workspace data root is reused and no full reindex is
required. Cold start means starting from stopped processes.

Memory budgets must be measured for each profile. `local_authoritative`
measurements include the Eshu host process plus graph backend.

## Dogfood Tiers

| Tier | Shape | Use |
| --- | --- | --- |
| 0 | Synthetic fixtures and package tests | Handler, parser, graph, and query contracts. |
| 1 | Active repo under about `5K` files or `50K` entities | Normal local developer proof. |
| 2 | Large repo under about `25K` files or `300K` entities | Language dogfood before promotion. |
| 3 | Stress repo over about `25K` files or `300K` entities | Backend and projection pressure tests. |
| 4 | Multi-repo corpus | Scheduling, queue, and memory pressure. |

A dogfood note must include tier, commit or branch, repository name, language
focus, file count, entity count, fact count, terminal state, stage durations,
backend, runtime knobs, retry counts, and dead letters.

## Evidence Rules

- Apply schema before indexing for Compose, Kubernetes, and backend comparisons.
- Record collector stream complete, projection or bootstrap complete, and
  queue-zero separately.
- Walk the proof ladder in order: focused fixture, single repo,
  representative medium subset, then full corpus.
- Treat timeouts as symptoms. Classify query shape, missing schema/index,
  backend fallback, transaction validation, queue behavior, stale images,
  background backend work, and real timeout-budget misses before changing a
  timeout.
- Do not ship serialization as a performance fix. Worker-count reductions,
  single-threaded drains, disabled concurrent writers, or batch size `1` are
  diagnostics unless the serial path is the proven permanent contract.
- Keep performance and observability evidence in versioned repo files. PR text
  alone is not proof.

Hot-path PRs must use one performance marker consumed by
`scripts/verify-performance-evidence.sh`:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`

They must also use either `Observability Evidence:` or
`No-Observability-Change:`.

## Next-Phase Scale Envelope (#2696)

This section records the measured boundary for the next collector and reducer
growth phase. It completes the #2696 parent audit by naming what is safe today,
what is risky until a focused implementation ticket lands, and what stays
blocked until production-shaped proof exists.

The current envelope is evidence-based, not a promise that every collector can
scale indefinitely. Operators should promote from `local/dev` to
`hosted-small` or `hosted-growth` only when the proof below matches their
runtime shape.

Existing proof that feeds this envelope:

| Proof | What it establishes | Remaining boundary |
| --- | --- | --- |
| Generation retention implementation evidence in `docs/internal/design/2248-retention-semantics-generations-facts-content.md` | Superseded-generation cleanup can prune a 63K-fact fixture in bounded batches while preserving active and retained-window reads. | It is not a fact-table partitioning or hosted-growth index-size migration proof. |
| Reducer claim readiness benchmark in `go/internal/storage/postgres/evidence-notes.md` | Readiness-gated reducer claim latency stayed bounded across 1,000 queue rows and up to 5,000 readiness phase rows after the data-shaped lookup change. | It does not prove all high-cardinality collector growth or relation-size thresholds. |
| Code-graph sub-scope partitioning contract | CALLS has versioned file-scoped partition keys and a partitioned runner foundation with whole-scope fallback. | Semantic entity, SQL relationship, and inheritance domains still need their own partition-key proof. |
| Workflow fairness unit coverage | `FamilyFairnessScheduler` performs deterministic weighted round-robin across collector families and rotates instances inside one family. | Production claim dispatch still claims one collector family/instance at a time until #2748 wires the scheduler or an equivalent boundary. |

| Profile | Intended shape | Gate before promotion |
| --- | --- | --- |
| `local/dev` | One developer or CI stack, bounded repositories, embedded or Compose stores. | Package tests, bounded fixture proof, no retrying or dead-letter queue state, and no unexpected graph-write contention. |
| `hosted-small` | Split API, MCP, ingester, reducer, workflow coordinator, and a bounded collector set. | `/admin/status` terminal queue proof, fact and queue row-count readback, p95 claim latency, graph-write timing, retry/dead-letter counts, and bounded collector status. |
| `hosted-growth` | Many claim-driven collectors, larger fact tables, and high-cardinality shared projection domains. | Dedicated Postgres partition or index proof, per-family collector fairness proof, provider throttle/backpressure proof, and shared projection contention proof. |

### Reducer Conflict-Domain Audit (#2697)

Reducer truth still follows the durable flow:

```text
source facts -> reducer queue claim -> handler extraction -> graph or read-model write -> ack, retry, or dead-letter
```

Safe domains today are domains whose conflict keys already name the complete
write set or whose handlers remain whole-scope by design:

| Class | Current state | Operator signal |
| --- | --- | --- |
| safe domains | Whole-scope `code_graph` fencing, source-local projection gates, graph-readiness-gated cloud relationship domains, and generation retention cleanup keep correctness first. CALLS has the strongest file-scoped partition-key foundation, but defaults remain compatible with whole-scope behavior. | Reducer queue status, bounded readiness blockage rows, shared-intent backlog, graph write metrics, reducer execution counters, and reducer completion logs. |
| risky domains | `semantic_entity_materialization`, `sql_relationship_materialization`, and `inheritance_materialization` can become partitionable only when the reducer can name the full source-owner retract and write set before graph mutation. | Pending/retrying/dead-letter rows must keep domain, conflict key, failure class, retry count, and fallback reason visible. |
| blocked domains | Any domain that retracts by repository, broad graph neighbor, language family, or ambiguous generated/source-map ownership stays whole-scope. A partition key derived from a raw path, commit SHA, private locator, random ID, or graph readback is invalid. | Whole-scope fallback must be explicit in logs/status; it cannot be hidden behind lower worker counts. |

Issue #2751 owns the next implementation proof for extending partition keys
beyond CALLS. Until that lands, Eshu must not claim broad intra-repository
parallel graph writes for semantic, SQL, inheritance, or other high-cardinality
domains. Issue #2754 owns the generic shared projection selection gap: hashed
shared domains should use indexed partition candidates before Eshu promotes
more high-cardinality shared projection lanes. Issue #2755 owns the resource
collector conflict-domain audit for cloud, IAM, Kubernetes, and related
materialization domains that currently do not have a CALLS-style partition-key
contract.

### Postgres Fact And Queue Growth Envelope (#2698)

Postgres currently owns facts, queue rows, status, content, recovery state,
generation retention, workflow control, and shared projection intents. The
implemented retention proof keeps superseded history bounded by scope and
protects active, pending, retrying, claimed, running, failed-current, and
dead-letter work from unsafe cleanup.

The growth gates are:

| Data class | Current gate | Hosted-growth trigger |
| --- | --- | --- |
| `fact_records` | Active reads join `ingestion_scopes.active_generation_id`; retention prunes eligible superseded generations in bounded batches. | Sustained fact rows or index size grow faster than retention can prune while active reads or fact writes exceed the known-normal band. |
| `fact_work_items` | Projector and reducer claims use `FOR UPDATE SKIP LOCKED`, lease fencing, retry/dead-letter states, and conflict-domain gates. | Claim latency rises with queue depth, retry storms, or dead-letter replay while CPU/disk are not saturated. |
| `shared_projection_intents` | Batched upsert, stable intent IDs, partition hash selectors, partition leases, and status backlog readbacks bound code-call-heavy repositories. | Non-CALLS domains need partition-key proof or shared-intent backlog grows without graph-write saturation. |
| workflow rows | Claim-aware collector rows carry `fairness_key`, retry state, lease fencing, and expired-claim reaping. | Per-family queue depth, claim wait, or lease age shows starvation across many collector families. |

Issue #2749 owns the hosted-growth Postgres partition or migration proof. That
work must record row counts, index size, representative read/write latency,
queue drain behavior, migration rollback, empty-table behavior, large-table
behavior, old-generation safety, stale-row safety, and active-claim safety
before changing production schema layout.

### Collector Fairness And Provider Backpressure (#2699)

The workflow package already has a deterministic weighted round-robin
`FamilyFairnessScheduler`, and workflow work items persist `fairness_key` for
target grouping. The production claim query is still intentionally FIFO within
one collector family and instance; `workflow_control_sql.go` leaves
multi-family fairness for a follow-up wiring phase.

The current fairness and backpressure envelope is:

| Concern | Current state | Required next proof |
| --- | --- | --- |
| claim wait | Claim-aware collectors can expose pending, claimed, retryable, expired, terminal, and completed work through workflow status and Postgres query spans. | #2748 must wire family-level scheduling or an equivalent dispatcher so one busy family cannot consume all claim attempts. |
| lease age | Heartbeats and expired-claim reaping preserve ownership fencing and recovery. | Proof must cover slow collectors, expired leases, recovery, and stale owner rejection without dropping active work. |
| retry/dead-letter | Retryable and terminal claim failures are durable, and workflow runs reconcile terminal failures into blocked completeness. | #2750 must add or identify provider throttle outcomes and retry-storm status without putting provider targets in metric labels. |
| per-family queue depth | `fairness_key` and collector kind are present on work rows; selected status readbacks aggregate bounded registry and workflow state. | Hosted-growth proof must surface per-family queue depth, provider throttle, and starvation signals in metrics, status, or logs with bounded labels. |

Provider-rate backpressure must remain provider-family aware. A rate-limited
provider should delay or retry its own claim stream; it must not force unrelated
families into a permanent serial path. Issue #2756 owns the code path that must
propagate provider `Retry-After` values into claim retry timing and wire a
consistent max-attempt budget for claim-driven collector services that currently
omit one.

No-Regression Evidence: this #2696 scale-envelope update is documentation and
verification-script only. It adds no reducer conflict key, queue SQL, graph
write, Cypher, worker, lease, batch, runtime knob, schema DDL, metric, span, log
field, status field, API/MCP route, collector runtime, or provider call.

No-Observability-Change: the update records which existing and follow-up
signals operators must use, but it does not change runtime telemetry. Current
diagnosis still uses `/admin/status`, reducer queue status, workflow work item
state, Postgres query spans and duration metrics, reducer execution counters,
shared-intent backlog, graph-write metrics, and collector/coordinator logs.

## Current Hot-Path Evidence

### Hosted-Growth Postgres Fact And Queue Proof Gate (#2749)

No-Regression Evidence: #2749 adds a storage-evaluation proof contract and
public-safe verifier only. It does not change Postgres DDL, relation indexes,
queue claim SQL, reducer workers, runtime defaults, graph writes, or collector
fanout. The focused proof suite first failed because
`ValidateHostedGrowthPostgresProof` and
`scripts/verify-hosted-growth-postgres-proof.sh` did not exist, then passed
after the contract required `fact_records`, `fact_work_items`,
`shared_projection_intents`, and `shared_projection_acceptance` row/index size
measurements; fact and queue read/write latency; reducer queue drain evidence;
empty and large table migration scenarios; stale rows; retry/dead-letter rows;
active-claim preservation; active-generation read correctness; changed-since
retained-window correctness; rollback behavior; and a hosted-small to
hosted-growth operator gate.

No-Observability-Change: the slice adds no metric, span, log field, status
route, worker, lease, batch size, runtime knob, or data-plane query. It defines
the evidence future hosted-growth proof runners must report: relation sizes,
index sizes, read/write latency, queue depth, oldest queue age, retry count,
dead letters, stale rows, active claims, migration duration, rollback status,
and public-safe summary output. Raw repositories, hostnames, IPs, paths, DSNs,
logs, source payloads, principals, accounts, and credentials remain
operator-local.

### Collector Fact Evidence Status Read (#1678)

No-Regression Evidence: issue #1678 baseline on remote
`eshu-remote-e2e-bg-qa` timed out the collector fact evidence read under a
15-second client budget with 3,551,004 `fact_records` rows. The fix changes
`ReadStatusSnapshot` from a per-fact `workflow_work_items` lateral lookup to
an active-scope fact pre-aggregation plus one workflow identity lookup per
`collector_kind`/`scope_id`/`generation_id`. Focused regression proof:
`go test ./internal/storage/postgres -run
'TestCollectorFactEvidenceQueryPreAggregatesBeforeWorkflowIdentity|TestReadCollectorFactEvidenceUsesBoundedActiveFactMetadata|TestBootstrapDefinitionsIncludeCollectorStatusFactIndex|TestWorkflowControlSchemaIndexesCollectorScopeGenerationLookup|TestWorkflowControlEmbeddedSchemaMatchesDataPlaneSchema'
-count=1` verifies the bounded query shape and the two schema indexes. The
input shape remains active, non-tombstone facts for known collector kinds, and
the output stays capped at 200 collector/evidence rows.

No-Observability-Change: the read still runs inside `ReadStatusSnapshot`, the
status API handlers, and the existing Postgres query spans and duration
metrics. Operators diagnose it through the existing collector status response
fields, `postgres.query` telemetry, request cancellation, and backend timeout
signals; no route, worker, metric label, log field, or runtime knob changes.

### EC2 Block-Device KMS Posture Writer (#1304)

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
BenchmarkEC2BlockDeviceKMSPostureNodeWriter -benchmem -count=3` on darwin/arm64
Apple M4 Pro writes 5,000 uid-anchored EC2 posture property rows at
2.43-2.45ms/op, 3.61MB/op, and 35,068 allocs/op with a no-op group executor,
isolating Eshu-owned statement construction and batching from graph round trips.
The writer uses one batched `UNWIND` + `MATCH (resource:CloudResource {uid:
row.uid})` + `SET` shape and never performs per-volume graph lookups.

Observability Evidence: `reducer.ec2_block_device_kms_posture_materialization`
wraps fact load, dual readiness, extraction, retract, and graph write. The
handler emits `eshu_dp_ec2_block_device_kms_posture_decisions_total` by
`outcome`/`reason`, `eshu_dp_ec2_block_device_kms_posture_skipped_total` by
`skip_reason`, and a completion log with resource/relationship/posture counts,
row count, decision and skip tallies, and stage durations.

### Semantic Entity Delta Projection (#2257)

No-Regression Evidence: `go test ./internal/reducer ./internal/storage/cypher
-run 'TestSemanticEntity.*Delta|TestSemanticEntityMaterializationHandlerScopesDeltaRetractToFiles|TestSemanticEntityWriterRejectsDeltaRetractWithoutFilePaths'
-count=1` failed before `SemanticEntityWrite` carried file-delta scope, then
passed after delta semantic materialization supplied qualified changed/deleted
file paths and the Cypher writer required those paths before retracting. The
focused shape uses no live graph backend: reducer fakes cover one changed file
plus one deleted file, a deleted-only delta with zero semantic rows, and a
malformed delta with no file paths; storage fakes cover one semantic row and two
scoped retract paths through the no-op recording executor. `go test
./internal/reducer ./internal/storage/cypher -count=1` also passed.

Observability Evidence: semantic entity materialization continues to emit the
existing completion log with fact, repo, row, and stage-duration fields, and now
adds `delta_projection` plus `delta_file_count` so operators can distinguish a
repo-wide semantic refresh from a file-scoped delta cleanup without adding a new
metric label or runtime knob.

## Manual Gates

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

These gates prove startup and synthetic query paths. They are not substitutes
for active-repo transitive-caller, call-chain, dead-code, reducer-throughput,
memory-budget, or full-corpus drain evidence.

## Open Evidence

These targets remain open until accepted perf gates land:

- active-repo dead-code scan
- reducer bulk write throughput for `50K` facts
- idle and active memory budgets for Eshu host plus graph backend
- active-repo transitive-caller and call-chain latency
- full-corpus `local_authoritative` drain with terminal queue-zero state

If local Eshu misses these targets, update the docs and capability matrix to
show the actual supported envelope. Do not hide the miss behind stale evidence.
