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
| Workflow fairness unit coverage | `FamilyFairnessScheduler` performs deterministic weighted round-robin across collector families and rotates instances inside one family. #2748 wired the shared fair claim-dispatch boundary inside `internal/collector`, and #2773 added `MultiSourceCollectorHost`, which registers multiple claim-aware sources behind one shared dispatcher and resolves the source per dispatched target while `ClaimedService` stays the sole claim-lifecycle owner. | Production binary wiring and hosted-growth multi-family starvation proof remain; the host abstraction and its fairness/race unit proof are in place. |

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
| blocked domains | Any domain that retracts by repository, broad graph neighbor, language family, or ambiguous generated/source-map ownership stays whole-scope. The scope-wide resource relationship and posture domains (`aws_relationship_materialization`, `gcp_relationship_materialization`, `azure_relationship_materialization`, `iam_can_assume_materialization`, `s3_logs_to_materialization`, `s3_external_principal_grant_materialization`, `rds_posture_materialization`, and the Kubernetes-correlation and security-group reachability reducers) load, write, or retract by scope today, so they remain whole-scope. A partition key derived from a raw path, commit SHA, private locator, random ID, IP-address-shaped value, or graph readback is invalid. | Whole-scope fallback must be explicit in logs/status; it cannot be hidden behind lower worker counts. |

Issue #2755 (merged) closed the generic shared projection selection gap: hashed
shared domains now select indexed `partition_hash` candidates rather than
scanning pending rows by domain and filtering partition membership in memory.
Issue #2751 audited which high-cardinality shared domains can use that
foundation; see [Partition-Key Proof Beyond CALLS (#2751)](#partition-key-proof-beyond-calls-2751)
below. Until each named domain's per-domain implementation ticket lands, Eshu
must not claim broad intra-repository parallel graph writes for the inheritance,
SQL relationship, rationale, documentation, or other high-cardinality shared
edge domains.

Issue #2754 (merged) defined the resource collector conflict domains and
partition keys for cloud, IAM, Kubernetes, and related materialization domains,
and intentionally kept the GCP, Azure, EC2, Kubernetes, and security-group node
materializers plus the relationship and posture reducers on a hashed
`resource_scope` whole-scope fallback. Two follow-ups carry the partition-filtered
proof those domains need before promotion: issue #2782 owns partition-filtered
resource node materialization, and issue #2783 owns splitting the scope-wide
resource relationship and posture reducers into partition-safe load, write,
retract, retry, and dead-letter behavior. Until those land, resource domains
stay on the explicit `resource_scope` fallback and Eshu must not claim
per-resource parallel graph writes.

### Partition-Key Proof Beyond CALLS (#2751)

CALLS has the strongest file-scoped partition-key foundation: its edge originates
at `source`, and its delta retract anchors on `source.path` / `source.repo_id`,
so a file partition names the complete write-and-retract set. The audit of the
other high-cardinality shared **edge** domains found a structural prerequisite:
unlike CALLS and cross-repo resolution, the inheritance, SQL relationship,
rationale, and documentation edge handlers call the edge writer directly and do
**not** persist shared-projection intents today. They are listed in
`sharedProjectionDomains` but the generic partitioned runner finds zero pending
rows for them. Promotion is therefore not "set a partition key at emit" — it is
building the same intent-persist pipeline CALLS has (emit via `UpsertIntents`
with a versioned file-scoped key and `delta_projection` payload), then proving
convergence and concurrency, **per domain**.

The governing invariant for any promotion: the **emit partition-key dimension
must equal the delta retract anchor**. A file-scoped key paired with a
whole-repository retract would over-retract a neighbor's edges; a whole-scope key
paired with a file retract would silently under-retract. The promotion test must
pin that the partitioned and whole-scope paths converge to byte-identical graph
and query truth.

| Shared edge domain | Durable source-owner anchor | Classification |
| --- | --- | --- |
| `inheritance_edges` | `child.path` / `child.repo_id` (delta retract already anchored on `child.path`) | SAFE to promote — key on the child file. |
| `sql_relationships` | `source.path` / `source.repo_id` (per-label delta retract on `source.path`) | SAFE to promote — key on the source file. |
| `rationale_edges` | `target.path` (the comment co-locates with the entity it precedes; delta retract anchors on `target.path`) | SAFE to promote — key on the **target** file, not the rationale uid. |
| `documentation_edges` | `section.scope_id` + `section.document_id` / `section.uid` | RISKY — the retract is scope-id-anchored, not code-file-path; the runner currently threads `repo_id` where the retract needs `scope_id`. Reconcile the `scope_id`-vs-`repo_id` retract plumbing before promotion. |

No shared edge domain is BLOCKED outright: each already has a narrow delta
retract path. The blocking risk is pairing a key with a broader retract, which
the invariant above forbids. Per-domain implementation tickets carry each
promotion: #2867 (`inheritance_edges`, first — cleanest anchor), #2868
(`sql_relationships`), #2869 (`rationale_edges`, target-file keyed), and #2870
(the `documentation_edges` `scope_id`-vs-`repo_id` retract reconciliation
precursor that must land before documentation is promoted). No domain is promoted
in #2751 itself, and none is promoted by lowering worker counts, batch size, or
graph-writer concurrency.

No-Regression Evidence: #2751 is an audit and documentation deliverable. It adds
no reducer conflict key, intent emit, queue SQL, graph write, Cypher, worker,
lease, batch, runtime knob, schema DDL, metric, span, log field, status field,
API/MCP route, collector runtime, or provider call. The classification is backed
by the existing delta-scope retract proofs (`inheritance_delta_scope_test.go`,
`sql_relationship_delta_scope_test.go`) and the direct-write handler call sites.

No-Observability-Change: the audit records which durable anchors and convergence
proofs a future promotion must carry; it changes no runtime telemetry.

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

Issue #2749 (merged) delivered the hosted-growth Postgres fact and queue
partition migration proof gate; see [Hosted-Growth Postgres Fact And Queue Proof
Gate (#2749)](#hosted-growth-postgres-fact-and-queue-proof-gate-2749) below. The
gate records relation row counts, index size, representative read/write latency,
queue drain behavior, migration rollback, and the empty-table, large-table,
old-generation, stale-row, retry/dead-letter, and active-claim scenario proofs
required before changing production schema layout. The actual production schema
partition migration stays gated behind that proof and is only triggered when the
`fact_records` or `shared_projection_intents` growth thresholds in the table
above are crossed.

### Collector Fairness And Provider Backpressure (#2699)

The workflow package has a deterministic weighted round-robin
`FamilyFairnessScheduler`, and workflow work items persist `fairness_key` for
target grouping. The production claim query stays intentionally FIFO within one
collector family and instance; `workflow_control_sql.go` keeps multi-family
fairness in the explicit scheduler rather than the claim SQL to avoid a silent
starvation regression. Issue #2748 (merged) wired the shared fair claim-dispatch
boundary inside `internal/collector`.

The current fairness and backpressure envelope is:

| Concern | Current state | Delivered proof and remaining boundary |
| --- | --- | --- |
| claim wait | Claim-aware collectors expose pending, claimed, retryable, expired, terminal, and completed work through workflow status and Postgres query spans, #2748 added the family-level dispatch boundary so one busy family cannot consume all claim attempts, and #2773 added `MultiSourceCollectorHost` so one runtime can register multiple claim-aware sources behind the shared dispatcher. | Production binary wiring of the host and hosted-growth cross-family starvation proof remain. |
| lease age | Heartbeats and expired-claim reaping preserve ownership fencing and recovery, and #2699 added `eshu_dp_workflow_claim_lease_age_seconds` (labeled by `collector_kind`, `source_system`) so rising lease age is visible before lease-TTL expiry. | Multi-source-host proof (#2773) must cover slow collectors, expired leases, recovery, and stale owner rejection without dropping active work. |
| retry/dead-letter | Retryable and terminal claim failures are durable, workflow runs reconcile terminal failures into blocked completeness, #2750 (merged) surfaced provider throttle/backpressure status, and #2699 added per-family `eshu_dp_workflow_claim_retries_total` (`failure_class`) and `eshu_dp_workflow_claim_provider_throttle_total` (`outcome`) counters without provider targets in labels. | Hosted-growth proof must still confirm retry-storm status under many concurrent provider families. |
| per-family queue depth | `fairness_key` and collector kind are present on work rows, and #2857 added the `eshu_dp_workflow_family_queue_depth` observable gauge (labeled by `collector_kind`, `source_system`, `status`) over `workflow_work_items`. | Hosted-growth starvation proof across a multi-source host stays with #2773. |

Provider-rate backpressure must remain provider-family aware. A rate-limited
provider should delay or retry its own claim stream; it must not force unrelated
families into a permanent serial path. Issue #2756 (merged) propagated provider
`Retry-After` values into claim retry timing and wired a consistent max-attempt
budget for claim-driven collector services that previously omitted one.

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

### Shared Projection Indexed Partition Selection (#2755)

The dedicated code-call runner already selected pending rows through the indexed
`partition_hash` predicate, but the generic shared projection runner
(`SelectPartitionBatch` in `go/internal/reducer/shared_projection_worker.go`)
still scanned pending rows by domain and filtered partition membership in
memory. Under a high-cardinality shared domain a leased partition's work could
sit behind a full `maxSharedSelectionScanLimit` (10,000-row) head slice of other
partitions and never be selected, surfacing as a "shared partition selection
reached scan cap" error rather than progress.

`SelectPartitionBatch` now prefers the indexed candidate readers
(`ListPendingDomainPartitionIntents` plus the legacy
`ListPendingDomainUnhashedIntents`) when the intent reader implements them, and
keeps the in-memory domain scan with its widen-and-cap behavior for readers that
do not (test fakes and any non-Postgres backend). The selector reuses the
existing partial indexes
(`shared_projection_intents_domain_partition_pending_idx` and
`shared_projection_intents_domain_unhashed_pending_idx`); no new schema, index,
DDL, or migration is introduced. Correctness rests on the invariant that the SQL
`mod(partition_hash, count)` equals the Go `PartitionForKey` assignment used by
the partition lease, so the indexed path returns exactly the rows the in-memory
path would have, and same-key fencing across the hashed and unhashed lanes
deduplicates by intent id.

No-Regression Evidence: focused TDD proof in
`go/internal/reducer/shared_projection_partition_candidate_test.go`. The new
tests first failed on `main` and pass after the selector change:
`TestSelectPartitionBatchUsesIndexedPartitionCandidatesWhenReaderSupportsIt`
(indexed predicate is used, the in-memory domain scan is not called),
`TestSelectPartitionBatchDoesNotHitScanCapWithIndexedSelection` (a target-
partition row buried behind a 10,000-row other-partition head slice is returned
without the scan-cap error that the legacy path raises),
`TestSelectPartitionBatchMergesUnhashedFallbackForIndexedReader` (legacy
`partition_hash IS NULL` rows are partition-matched, merged in
`created_at`/`intent_id` order, and counted), and
`TestSelectPartitionBatchKeepsLegacyScanWhenReaderUnsupported` (readers without
the candidate interface keep the unchanged in-memory scan). Backend: package
fakes, no live graph or Postgres round trip; input shape covers pending,
unhashed-fallback, empty-partition, and starvation-cap cases. The full
`go test ./internal/reducer ./internal/storage/postgres -count=1` suite stays
green, and a compile-time assertion in
`shared_intents_partition_candidates.go` locks `*SharedIntentStore` to the
candidate contracts so the runner cannot silently fall back to the in-memory
scan. The fix uses no worker-count reduction, batch-size serialization, or graph
query. End-to-end throughput proof for promoting specific high-cardinality
shared domains to intra-repository parallel writes remains open and is tracked
under #2751.

Classification: correctness and scheduling win. It removes a partition-
starvation failure mode and cross-partition scan dilution for hashed shared
domains; it does not by itself change graph-write wall time.

No-Observability-Change to metrics and spans; the runner adds two bounded fields
to the existing `shared projection cycle completed` log — `indexed_selection`
(bool) and `unhashed_fallback_rows` (count) — so operators can confirm which
selection path a domain used and watch pre-hash rows drain, without any new
metric label, high-cardinality identifier, or runtime knob. Partition lease
churn stays diagnosable through the existing `lease_claim_duration_seconds`,
`selection_duration_seconds`, and shared-intent backlog signals.

### Collector Fairness Backpressure Metrics (#2699)

The fair claim-dispatch boundary (#2748), provider throttle status (#2750), and
Retry-After/max-attempt budget (#2756) merged earlier. #2699 closes the
remaining per-family observability gap so an operator paged at 3 AM can attribute
retry pressure, provider backpressure, and lease stalls to a collector family
without high-cardinality labels. It adds three bounded instruments recorded on
the existing claim failure and heartbeat paths:

- `eshu_dp_workflow_claim_retries_total` — labeled `collector_kind`,
  `source_system`, `failure_class`; incremented on each retryable claim re-queue.
- `eshu_dp_workflow_claim_provider_throttle_total` — labeled `collector_kind`,
  `source_system`, `outcome` (`retry_after_honored` or `poll_backoff`);
  incremented only when a retryable failure carries a rate-limited failure class
  or a positive provider `Retry-After`. Ordinary retryable failures (5xx,
  transport, deadline) wrapped in `sdk.ProviderFailure` report a zero
  `RetryAfterDelay()` and are deliberately excluded so generic outages do not
  read as provider backpressure.
- `eshu_dp_workflow_claim_lease_age_seconds` — labeled `collector_kind`,
  `source_system`; the active claim's held duration at heartbeat time.

All labels reuse the existing bounded `collector_kind`/`source_system`/
`failure_class`/`outcome` enums; no provider target, account, URL, token env, or
instance id enters a label. The per-family `eshu_dp_workflow_family_queue_depth`
observable gauge landed in #2857 (see below); the production multi-source-host
starvation proof stays with #2773.

No-Regression Evidence: focused tests in
`go/internal/collector/claimed_service_backpressure_metrics_test.go` prove each
counter/histogram increments with exactly the bounded label set
(`TestClaimedServiceRecordsPerFamilyRetryCounter`,
`TestClaimedServiceProviderThrottleRecordsRetryAfterHonored`,
`TestClaimedServiceProviderThrottleRecordsPollBackoff`,
`TestClaimedServiceRecordsClaimLeaseAge`). The recording is additive on the
existing `failRetryable` and heartbeat paths; it introduces no worker-count
reduction, batch-size serialization, or coordinator-owned claim mutation, and
`go test ./internal/collector ./internal/telemetry -count=1` stays green.

Observability Evidence: the three instruments above are the operator-facing
signals; they are recorded after the durable claim mutation (outside the
dispatcher scheduler lock) and on the heartbeat tick, so they add no
high-cardinality label and no runtime knob.

### Per-Family Queue-Depth Gauge (#2857)

#2857 completes the #2699 metric set with the `eshu_dp_workflow_family_queue_depth`
Int64 observable gauge. The reducer registers it with a read-only callback over
`WorkflowControlStore.WorkflowFamilyQueueDepths`, which groups outstanding
`workflow_work_items` (`pending`, `claimed`, `failed_retryable`, `expired`) by
`collector_kind`, `source_system`, and `status`. Completed and terminally-failed
rows are excluded because they are not live queue depth. The callback runs on
the meter collection goroutine and issues a single `GROUP BY` query backed by the
partial index `workflow_work_items_family_queue_depth_idx`
(`(collector_kind, source_system, status) WHERE status IN (...)`), so each scrape
is an index scan over only outstanding rows rather than a sequential scan of the
full table. Operators must not drop that index. No claim SQL, claim ownership, or
runtime knob changes.

No-Regression Evidence: `TestWorkflowControlStoreFamilyQueueDepthsGroupsByFamilyAndStatus`
(`go/internal/storage/postgres`) proves the query groups by family and status and
scopes to outstanding statuses;
`TestRegisterWorkflowFamilyQueueDepthObservableGauge_WithObserver` and
`_NilObserver` (`go/internal/telemetry`) prove the gauge observes each
`(collector_kind, source_system, status)` triple and is a no-op without an
observer. A compile-time assertion binds `*WorkflowControlStore` to
`telemetry.WorkflowFamilyQueueDepthObserver`. `go test ./internal/telemetry
./internal/storage/postgres ./cmd/reducer -count=1` is green.

Observability Evidence: the gauge is the per-family queue-depth signal the #2699
envelope required; labels reuse the bounded `collector_kind`/`source_system`/
`status` enums only — no instance id, target locator, account, URL, or token env
enters a label.

### Multi-Source Collector Host (#2773)

#2773 adds `MultiSourceCollectorHost` (`go/internal/collector/claimed_multi_source_host.go`)
so one runtime can register multiple claim-aware source adapters behind a single
shared `FairClaimDispatcher`. The host builds its dispatch candidates from durable
collector instance state — filtering disabled and claims-disabled instances out
before requiring sources — and refuses to start if any claim-enabled candidate
has no registered source, then runs N concurrent `ClaimedService` workers over the
shared dispatcher. Each worker resolves the source per dispatched target by
`(collector_kind, collector_instance_id)` (`resolveClaimedSource`, preferring an
exact instance registration over a kind wildcard), because some claim-aware
sources reject work whose instance id does not match their configured instance.
Claim ownership does not move into the
host or the coordinator: `ClaimedService` remains the sole owner of heartbeat,
fenced commit, retry, terminal failure, release, and completion. Per-instance
FIFO ordering is unchanged because every claim still flows through the existing
`ClaimNextEligible` path.

No-Regression Evidence: focused tests in
`go/internal/collector/claimed_multi_source_host_test.go`:
`TestClaimedServiceResolvesSourcePerDispatchedTarget` (two instances of one kind
get distinct sources; the dispatched instance's source serves the work, the
sibling instance's does not),
`TestMultiSourceCollectorHostRunsLifecycleWithoutStarvingFamilies` (the busy
family completes the full claim lifecycle through the host while the empty
family's lane is still queried — no starvation),
`TestMultiSourceCollectorHostSharedSchedulerIsRaceFree` (eight concurrent
workers exercising the shared scheduler/dispatch path with a concurrency-safe
unique claim-id generator, green under `-race`),
`TestNewMultiSourceCollectorHostRejectsCandidateWithoutSource`, and
`TestNewMultiSourceCollectorHostIgnoresDisabledInstances` (a disabled instance
with no source does not block startup). The host requires
`ClaimIDFunc` to be collision-free and concurrency-safe (documented on the host
config) so concurrent workers never share a claim-fence identity. The change adds no
worker-count reduction, batch-size serialization, or coordinator-owned claim
mutation; `go test ./internal/collector ./cmd/ingester ./cmd/reducer -count=1`
and `go test -race` on the concurrency test are green.

No-Observability-Change: the host reuses the existing claim-wait, retry,
provider-throttle, lease-age (#2860), and per-family queue-depth (#2857) signals
emitted by `ClaimedService` and the workflow gauge; it adds no new metric, label,
or runtime knob. Production binary wiring and a hosted-growth multi-family
starvation proof on the remote corpus remain open follow-ups.

### Partition-Filtered Resource Node Materialization (#2782)

#2754 marked AWS resource node materialization safe to promote off the whole-scope
`resource_scope` fallback to a per-resource `cloud_resource_node` conflict key,
because the canonical node writer is an idempotent `MERGE (r:CloudResource {uid:
row.uid})` with no scope-wide retract — so concurrent partitions can never delete
one another's writes. #2782 proves the promoted key actually fences correctly:

| State | Domains | Conflict key |
| --- | --- | --- |
| safe (promoted) | `aws_resource_materialization` | per-resource `cloud-resource-node:v1:<hashed entity key>` |
| risky (whole-scope fallback) | `gcp_resource_materialization`, `azure_resource_materialization`, `ec2_instance_node_materialization`, `kubernetes_workload_materialization`, security-group node domains | hashed `resource-scope:v1:<scope>` until provider-specific contention/case-fold proof lands |
| blocked | resource relationship and posture domains (AWS/GCP/Azure relationship, IAM, S3, RDS, EC2 posture, K8s correlation, SG reachability) | hashed `resource_scope`; they retract scope-wide, so partitioning races (see #2783) |

No-Regression Evidence: `go/internal/storage/postgres/reducer_queue_resource_node_fencing_test.go`.
`TestCloudResourceNodeConflictKeyFencesSameResourceSeparatesDistinct` proves two
AWS resource intents for distinct resources get distinct `cloud_resource_node`
keys (distinct-key concurrency) while the same resource gets the identical
deterministic key (same-key fencing), and the key never leaks the raw provider
locator. `TestReducerQueueClaimAndBatchFenceOnConflictKey` proves both the single
and batch reducer claim queries fence on `(conflict_domain, COALESCE(conflict_key,
scope_id))`, the queue mechanism that turns the per-resource key into
serialization for the same resource and concurrency across distinct resources.
The risky and blocked domains stay on the explicit `resource_scope` fallback; no
worker-count reduction, batch-size serialization, or scope-wide retract is used,
and `go test ./internal/storage/postgres -count=1` is green.

No-Observability-Change: the conflict key is an internal claim fence; it adds no
metric, label, span, log field, status route, or runtime knob, and the hashed key
keeps raw ARNs, paths, IDs, and IP-shaped values out of the queue.

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
