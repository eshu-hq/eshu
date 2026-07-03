# Architecture Review 2026-07: Performance and Scale

Part 3 of the July 2026 architecture review; start at [Architecture Review
2026-07](architecture-review-2026-07.md).

---

## Part E — Performance, HA, failure modes, scale

### E.1 Concrete bottlenecks (cited)

- **Giant-repo parse tail** — `git_source_processing.go:99-131`.
  Largest-first + byte-balanced partitions took the 896-repo parse stage from
  ~1586s to ~675s, but the single worst repo (16,659 files) still costs ~238s
  (#3711). *Measured.*
- **Per-handler fact loading** — handlers load full fact sets per (scope,
  generation) via `FactLoader.ListFacts`/`ListFactsByKind`
  (`fact_kind_loader.go:51-70`); budgets are enforced per handler
  (`reducer-claim-latency-gate.md`: AWS node materialization ≤16.1M ns/op,
  AWS relationship edges ≤37M, value-flow cold ≤30.6M). No cross-handler
  cache. *Measured (budgeted).*
- **Claim-query predicate growth** — every readiness-gated edge domain adds
  an `EXISTS` to the hot claim SQL (`reducer_queue_claim_query.go`),
  documented as linear-in-domain-count with a p95 ≤1.10× baseline gate.
  Polling only — no LISTEN/NOTIFY anywhere in the queue path. *Measured
  (gated).*
- **Shared projection lane overhead** — 8 partitions × 9 domains = 72
  lease/selection attempts per cycle at 500ms–5s adaptive poll
  (`shared_projection_runner.go:20-41,150-194`); two readiness gates
  (EndpointPresence #2809, RefreshFence #2898) can hold whole domains. The
  deferred relationship backfill was the historical monster — 20+ min serial,
  now 882s via 896-partition concurrency (#3710/#3725). *Measured.*
- **Graph write throughput** — `storage/cypher/writer.go` (batch 500 default,
  1000 for code-call edges), phase-group execution, deadlock retry delegated
  to the executor, and the backpressure permit pools
  (`go/internal/graphbackpressure/backpressure.go`;
  `ESHU_GRAPH_WRITE_{CANONICAL,SEMANTIC}_MAX_IN_FLIGHT`, split after
  #3560/#3652 head-of-line blocking). Bootstrap projection ≈1,245s for 3.5M
  facts ≈ 2,800 facts/s sustained into NornicDB. *Measured.*

Honesty note: the phase sums (675s parse + 1,245s projection + 882s backfill)
are per-stage measurements, not wall clock — stages pipeline. The <15-minute
896-repo end-to-end figure is the pipelined known-good baseline; the
per-stage numbers are the decomposition to optimize against.

### E.2 Scaling the reducer horizontally — real options

The reducer is **not architecturally single-instance**. Stateless workers,
SKIP LOCKED claims, 60s leases with 30s heartbeats, the live-lease unique
index, and proven idempotent replay mean N replicas are safe today
(`service-runtimes.md` says scale it when telemetry shows it is the
bottleneck). Options in ascending cost:

| Option | What you get | What breaks / costs |
| --- | --- | --- |
| 1. Add replicas (supported now) | More handler throughput; correctness holds via conflict fence + idempotent MERGE | Nothing correctness-wise. Claim-SQL load scales with replicas; conflict-fenced domains stay serial by design. Diminishing returns once claim p95 or graph permits saturate. |
| 2. Domain-sharded replicas (`ESHU_REDUCER_CLAIM_DOMAINS`, supported now) | Isolation: slow cloud materialization cannot starve code-graph domains | Ops complexity; possible idle shards; no code change. Cheapest meaningful step beyond #1. |
| 3. Split the three projection lanes into own deployments | Lanes already have independent lease managers and poll loops; extraction isolates the projection tail from claim/handler capacity | Wiring in `cmd/reducer` (`buildReducerService`, main.go:293-387); no ordering breaks — leases already assume multi-claimant. |
| 4. Scope-partition sharding (`ingestion_scopes.partition_key` exists) | True horizontal claim scaling; per-fleet Postgres read replicas later | Breaks cross-scope domains (DeploymentMapping, DeployableUnitCorrelation, RepoDependencyProjection) — they need a global lane or two-phase design. Do only when 1–3 headroom is spent. |
| 5. LISTEN/NOTIFY claim wake-up | Cuts idle claim latency and poll load | A scheduling win, not throughput. |
| 6. Backpressure into collectors | Bounds the queue instead of growing it | Coordinator must pace claim issuance on queue depth — designed nowhere today (gap, E.3). |

The ceiling behind all of these: every fleet funnels into one Postgres and
one NornicDB. At ~2,800 facts/s projection throughput, a 10× corpus means
either ~10× write throughput or a ~3.5-hour projection tail — the graph
backend, not the reducer fleet, is the terminal bottleneck. **(reasoned from
measured 1,245s / 3.5M facts.)**

### E.3 Where there is no HA story today

- **Ingester**: StatefulSet with a single workspace PVC — one instance, by
  documented design. Death = collection pauses until reschedule; claims
  expire and requeue; nothing corrupts, but there is no work-stealing or
  shard takeover. Sharding exists in env form (`ESHU_REPO_SHARD_COUNT/INDEX`)
  but not as an operated pattern.
- **Workflow coordinator**: Helm default 1 replica, no leader election found
  (**inferred** from chart + code absence). Two coordinators would
  double-issue eligibility scans — probably safe because claims are fenced,
  but unproven; either prove N-safe or add leader election.
- **Postgres and the graph backend**: single instances in the chart, no
  replication/failover documented. NornicDB death mid-materialization: phase
  writes are atomic per phase-group and the work item retries on lease
  expiry, so no partial-phase corruption; but there is no graph-level repair
  scan — recovery is queue-replay (`/admin/replay`, `/admin/refinalize`,
  `runtime/recovery_handler.go:14-35`). A restored-from-backup Postgres
  alongside a newer graph (or vice versa) has no reconciliation story.
  **(gap, reasoned.)**
- **Dead-letter visibility**: durable `failure_class` taxonomy,
  `/admin/status` promotion proofs, replay endpoints filterable by class —
  but no first-class operator listing surface (operators query
  `fact_work_items` by SQL). Cheap fix, high operator value.
- **No collector-side backpressure**: nothing slows intake when
  `fact_work_items` grows; enqueue is batched (250/insert,
  `workflow_control.go:111-136`) but unbounded. A weekend reducer outage
  under active webhooks balloons the queue and facts tables; recovery is then
  gated by claim-query performance over a huge backlog. **(reasoned.)**

### E.4 Scalability ceiling

**Measured**: 896 repos / ~3.5M facts; parse ~675s; bootstrap projection
~1,245s (~2,800 facts/s into NornicDB); deferred backfill 882s over 896
partitions (`collector-performance-envelope.md`,
`reducer-claim-latency-gate.md`, `specs/scale-lab-corpus.v1.yaml`).

**At 10× (≈9k repos, ≈35M facts), first breaks in order (reasoned):**

1. Postgres `fact_records` query behavior — the table carries 60+ indexes
   (63 `CREATE INDEX` statements in
   `schema/data-plane/postgres/003_fact_records.sql`, many on payload keys)
   plus hot claim predicates over a 10× table; per-handler fact loads blow
   their budgets first. No Postgres tuning doc exists to say what to watch.
2. Graph write throughput — 35M facts at ~2,800 facts/s ≈ 3.5h projection
   tail; permit pools then throttle everything upstream by design.
3. Claim-query p95 — linear domain growth × backlog depth × replica count.
4. Single-ingester I/O — 10× clone/checkout/read volume on one PVC.

---

## Part F — SDK and repo-split plan

### F.1 SDK public surface

v1 = what exists, hardened, plus the payload schema layer: keep the
`Claim`/`Result` types exactly; extend the validator with per-kind schema
validation; ship fixture packs and the guarantees doc; tag SDK releases with
a compatibility table. Deliberately deferred: push/streaming collection,
direct queue or graph access (never), reducer-side extension points,
non-Go SDKs (publish the JSON Schemas instead), OCI runner as default.

### F.2 Migration path for the first collector

PagerDuty is the designated reference
(`collector-extraction-policy.md`): packaging, trust, claim execution,
fact-shape parity, Compose proof, redaction, and operator evidence are
complete; classification is `extraction_candidate`; nothing is
`external_ready` yet. The caveat the policy itself states: the reference
component emits namespaced example kinds (`dev.eshu.examples.pagerduty.*`)
that the incident-routing readback does not consume — flipping the in-tree
collector off today would silently break incident routing.

Before the split is safe rather than early, in order:

1. Payload schemas for the `incident.*` / `incident_routing.*` kinds.
2. Kind-parity decision: the external collector emits the core fact kinds
   (registry marks lifecycle owner as the external package), or readback
   gains kind aliasing. Core kinds under the existing registry entry is
   simpler and matches the parity already proven.
3. Tagged SDK + fixture pack releases the external repo pins in CI.
4. Dual-run window: in-tree and external collectors side by side on distinct
   `CollectorInstanceID`s in a staging corpus; the `parity/` harness compares
   fact streams until byte-parity holds for N cycles.
5. Operational readiness: revocation drill executed once; dead-letter
   visibility through component diagnostics confirmed; Helm wiring
   default-off → opt-in → default flip as three separate releases.
6. Move the code, mark the family `external_ready`, delete the in-tree copy
   one release later.

### F.3 Governance once collectors have other maintainers

| Problem | Solved by the contract system? | Still needs a separate answer |
| --- | --- | --- |
| Schema drift | Yes — payload schemas + fail-closed conformance + schema-diff gate | Enforcement discipline only. |
| Version skew | Mostly — envelope semver + `compatibleCore` + at-least-once idempotent facts + `unsupported_minor` quarantine | A published support window (policy statement). |
| Malicious collector | Mostly designed — no handles by construction, digest pinning, allowlist/strict Sigstore+SLSA, revocation | Wire strict verification into default hosted activation; live signed index; egress/resource-limit enforcement (**inferred** to be per-deployment K8s config today). |
| Buggy collector flooding facts/queue | Partially — attempt budgets, fact-count statuses | Per-component quotas + coordinator pacing (same design as the E.3 backpressure gap). |
| Review/CI burden | Partially — conformance gives reviewers a machine verdict; extraction-readiness diagnostics encode policy | Maintainer ladder + tiered trust + contributor-tier CI. |
| Security response for third-party packages | No | Written policy: who revokes, SLA, notification channel. |

---

## Prioritized punch list

Ordered accuracy → performance → scale, and by how much each unlocks the
OSS/SDK goal:

1. **Close the payload contract hole** (now the
   [Contract System v1 design](design/contract-system-v1.md)). An accuracy
   bug class today and the prerequisite for every public-boundary ambition.
2. **Declare the IDL decision** — registry + Go types + JSON Schema
   normative; demote or delete `proto/`. (Resolved by the design doc.)
3. **Ship consumer-driven fixture packs + the guarantees doc.**
4. **Unified finding read contract**
   ([A.5](architecture-review-2026-07.md#a5-how-findings-actually-work-today))
   — design it before GCP/Azure drift findings multiply the fragmentation.
5. **Publish the SLO/performance contract and the Postgres tuning doc.**
6. **PagerDuty to `external_ready`** via F.2, tagging SDK v0.2 on the way.
7. **HA hygiene**: dead-letter operator surface, coordinator
   leader-election-or-proof, backpressure/quota design.
8. **Then scale work in evidence order**: domain-sharded fleets and
   projection-lane extraction, LISTEN/NOTIFY as a scheduling win, and only
   after those are spent, scope-partition sharding — knowing the graph
   backend's ~2,800 facts/s is the ceiling that decides 10×.
9. **Collector pattern convergence**: GCP typed-depth registry as the
   documented standard; hold Azure to it now, migrate AWS opportunistically.

The single most important sentence in this review: **the seam about to become
public already exists and is better than assumed — but it is versioned only
down to the envelope, and the graph's correctness currently depends on
unwritten agreements about JSON keys.** Fix that first; everything else is
sequencing.
