# Local Eshu Service Performance Envelope

This page defines the expected local performance envelope. Local Eshu should be
useful on a normal developer laptop, not only on ideal hardware or empty
repositories.

Reference hardware is an Apple Silicon or mid-range x86 laptop with at least
4 cores and 16 GB RAM.

Related: [Collector Performance Envelope](collector-performance-envelope.md)
defines the per-collector claim/ingest/emit/project budgets and gold points
(the measured git 896-repo full-corpus template) that feed the local query
envelope below.

## Target Profiles

| Profile | Runtime shape | Target |
| --- | --- | --- |
| `local_lightweight` | Eshu plus embedded Postgres | cold start under `5s`; warm restart under `2s`; exact symbol lookup p95 under `500ms`; content search p95 under `800ms`; complexity query p95 under `1500ms`; single-file reindex to visible search update under `2s`. |
| `local_authoritative` | Eshu plus embedded Postgres and embedded NornicDB | cold start under `15s`; warm restart under `5s`; transitive caller and call-chain p95 under `2s` on an active repo; active-repo dead-code scan under `10s`; reducer bulk write batch under `10s` for `50K` facts; single-file reindex to visible graph update under `5s`. |

Warm restart means the same workspace data root is reused and no full reindex is
required. Cold start means starting from stopped processes.

Memory budgets must be measured for each profile. `local_authoritative`
measurements include the Eshu host process plus graph backend.

### `eshu mcp start` default owner profile (#3026)

`eshu mcp start`, when it starts its own owner, now selects `local_authoritative`
by default (previously `local_lightweight`). This trades the Postgres-only cold
start for the embedded-graph cold start so a fresh MCP session can answer
graph-backed questions (transitive callers/callees, import dependencies,
read-only Cypher, call-graph metrics) instead of returning
`unsupported_capability`. `eshu index` and other watch-mode owners keep the
`local_lightweight` default. The owner profile resolves in
`defaultProfileForMode` / `resolveLocalHostRuntimeConfigWithDefault`
(`go/internal/cli/localsupervisor/host.go`,
`go/internal/cli/localsupervisor/config.go`); it is a
default-selection change only, not a change to either profile's runtime path.

- Affected stage: local owner cold start for the `eshu mcp start` stdio
  owner-creation path. The attach path (an owner already running) is unchanged.
- Expected cardinality: Tier 1 active repo (about `5K` files / `50K` entities).
- No-Regression Evidence: the `local_authoritative` runtime path is the same
  supervisor `eshu graph start` already uses (same `local-host` flow, embedded
  NornicDB + reducer + ingester), so its envelope is unchanged: cold start under
  `15s`, transitive caller and call-chain p95 under `2s` on an active repo, as
  stated for `local_authoritative` above. The change only selects that
  already-measured profile as the `eshu mcp start` default; it adds no hot-path
  code. The higher cold-start cost versus `local_lightweight` is deliberate and
  bounded by the `local_authoritative` row, and is opt-out via
  `--profile local_lightweight` / `ESHU_QUERY_PROFILE=local_lightweight`.
- Observability Evidence: No-Observability-Change. The booted profile is already
  visible to operators — it is persisted in `owner.json` (`profile`,
  `graph_backend`) and the supervisor logs `bootstrapping local graph schema...`
  / `local graph schema ready` on the authoritative path. No new metric, span, or
  status field is required to diagnose which profile started.
- Proof ladder and stop threshold: focused `go test ./cmd/eshu` (profile
  resolution and flag wiring), then a single-repo `eshu mcp start` cold start
  timed against the `local_authoritative` row. Stop and profile if cold start
  exceeds the `15s` envelope by more than `10%`.

### `make prove` credential-free common path budget (#4397)

`make prove` (`scripts/dev/prove.sh`) is the credential-free local mirror of
the Ifá CI gate: the Ifá contract-layer test, both Docker-matrix hermetic
structural mirrors, and the `ifa coverage` reconcile. This is its own
prove-latency budget, distinct from every row above — it bounds `make prove`'s
common path only, not the path-selected Docker matrix (Layer 2), whose wall
time varies by machine/Docker state and is reported informationally, never
budgeted.

- Affected stage: `make prove`'s credential-free common path (Ifá
  contract-layer test, both hermetic determinism/dead-letter-matrix
  structural mirrors, `ifa coverage` reconcile). The Docker matrix itself is
  out of scope for this budget.
- Method: at least three measured runs on the same box, per this platform's
  P4 prove-latency-budget policy. Budget is `max(the three runs)` plus about
  `25%` headroom (worst-case, per this repo's performance-envelope doctrine).
  Measured on an Apple Silicon dev laptop with a warm Go build cache (the
  realistic repeated-local-run shape, not a from-scratch clone): `4s`, `4s`,
  `4s` across three consecutive runs, so `max = 4s`, budget = `4s * 1.25 = 5s`.
- Enforcement: operator-gated (`EnforcementOperatorGated`,
  `go/internal/perfcontract`'s `localEnvelopeThresholds`). `make prove` prints
  its own measured wall time against this budget and WARNS, but does not
  fail, when the budget is exceeded — a prove-latency regression is a bug to
  root-cause, not a hermetic gate failure, per the design doc's flake and
  prove-latency policies.
- Target: the `make prove` credential-free common path stays under `5s`.

### Compose local hash semantic-search default (#3324)

Docker Compose now defaults API, MCP, and reducer to
`ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=auto_hash`. This is a first-run selector
change only: when no governed search provider is configured it enables
deterministic no-network query embeddings and vector sidecar builds for curated
search documents. Real provider-backed `search_documents` embeddings remain
opt-in through provider profile and source policy configuration, and `auto_hash`
yields to exactly one governed provider profile when that configuration is
present. Bootstrap and ingester stay on deterministic no-provider defaults, so
source discovery, parsing, fact emission, and initial indexing do not make
provider calls.

- Affected stage: local Compose API/MCP semantic-search reads and reducer
  search-vector sidecar builds for active curated search documents.
- Baseline: previous Compose passed
  `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=${ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER:-}`
  to API, MCP, and reducer, so no-provider first-run mode left the vector
  builder disabled and `mode=semantic` could not use local vector rows.
- After measurement: the default env shape is
  `${ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER:-auto_hash}` for API, MCP, and
  reducer. Focused regression coverage proves the reducer wires a search-vector
  build runner with provider profile `local`, model `local-hash-v1`, and
  `VectorRetrievalAuto` when no governed search provider is configured.
- Backend/version and input shape: Compose local stack, deterministic local hash
  embedder `local-hash-v1`, curated `search_documents` rows, active-generation
  vector metadata/value sidecar rows, NornicDB graph backend unchanged.
- Terminal state: no queue or row-count migration is introduced. Existing
  reducer projection continues to own row creation; first-run diagnostics use
  `/api/v0/search/semantic` and `retrieval_state` (`semantic_active`,
  `index_unready`, or `semantic_unavailable`) to show whether local vectors are
  ready.
- No-Regression Evidence: `go test ./internal/runtime -run
  'TestDefaultComposePassesSemanticSearchConfigToReadersAndVectorBuilder|TestDockerComposeDocsDescribeSemanticProviderModes'
  -count=1` pins the Compose env defaults and first-run diagnostic docs.
  `go test ./cmd/reducer -run
  'TestBuildReducerServiceWiresSearchVectorBuildRunnerWhen(LocalHash|ProviderProfile)Configured'
  -count=1` pins local hash and governed-provider runner wiring. The broader
  package gate `go test ./internal/runtime ./cmd/reducer ./cmd/api
  ./cmd/mcp-server ./internal/searchembedruntime ./internal/searchembed
  ./internal/searchvector ./internal/query -count=1` passed for the changed
  runtime surfaces.
- Observability Evidence: local hash and provider-backed vector builds remain
  visible through existing bounded search-vector build results, Postgres
  query/exec spans, semantic-search route spans, retrieval state fields, and
  vector metadata failure classes. Provider-backed profiles additionally report
  redacted provider profile status. The Compose change adds no raw prompt,
  credential, endpoint, provider body, path, document id, metric label, span
  attribute, or log field.

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
- Record performance and observability evidence in the **PR description**
  (never in ephemeral files under `docs/internal/evidence/` or appended to
  versioned docs).

Hot-path PRs must use one performance marker consumed by
`scripts/verify-performance-evidence.sh`:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`

They must also use either `Observability Evidence:` or
`No-Observability-Change:`.

## Next-Phase Scale Envelope

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
| Generation retention implementation evidence in #2248 | Superseded-generation cleanup can prune a 63K-fact fixture in bounded batches while preserving active and retained-window reads. | It is not a fact-table partitioning or hosted-growth index-size migration proof. |
| Reducer claim readiness benchmark (`BenchmarkReducerQueueClaimReadinessGateGrowth`) | Readiness-gated reducer claim latency stayed bounded across 1,000 queue rows and up to 5,000 readiness phase rows after the data-shaped lookup change. | It does not prove all high-cardinality collector growth or relation-size thresholds. |
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
by the existing delta-scope retract proofs (`inheritance/delta_scope_test.go`,
`sqlrelationship/sql_relationship_delta_scope_test.go`) and the direct-write
handler call sites.

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

Issue #2749 delivered the hosted-growth Postgres fact and queue partition
migration proof gate. Issue #4044 extends that gate into the `fact_records`
growth breakpoint decision record (#4044): measure growth
pressure first, then choose partitioning, archive tables, fact-family splits,
retention tuning, or deferral from evidence.

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
