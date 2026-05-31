# Observability Coverage Correlation Read Model — Design Memo

Status: **design for principal review; NO code and NO schema change in this PR.**
Issue: #391 (reducer: observability coverage correlation read model). Parent: #373.
Owners (proposed): reducer/projection owners + AWS scanner fleet.
Scope of this memo: decide whether #391 is build-safe, and if so, fix the fact
shape, reducer domain, graph-write plan, correlation truth matrix, and phased PR
plan **before** any implementation lands.

This note lives under `docs/internal/design/` because it is maintainer design,
not operator-facing reference. `docs/mkdocs.yml` sets `docs_dir: public`, so this
file is intentionally outside the strict mkdocs build — same placement decision
as `docs/internal/aws-relationship-edge-materialization-design.md` (#805), which
is the closest shipped precedent and the structural template for this memo.

---

## 0. The grounding correction that reshapes this issue

The issue body and its release-lane triage comment are built on one assumption
that is **partly stale**, and getting this right is the whole decision:

> "there is no deployed collector producing dashboard/monitor/scrape/rule/pipeline/alert
> facts for this reducer to correlate."

That is true for the **generic multi-provider** observability collector the issue's
source docs describe (`docs/docs/adrs/2026-05-15-observability-collector.md`,
`docs/docs/reference/collector-reducer-readiness.md` — note the stale `docs/docs/...`
paths; current is `docs/public/...`). Those docs model Datadog/Prometheus-style
*dashboards, monitors, scrape jobs, telemetry pipelines, rules, paging alerts*.
No such collector exists on `main`, and `collector-reducer-readiness.md` line 115
correctly lists "observability" as **"design or research only."**

But it is **not** true for AWS-native observability. The CloudWatch, CloudWatch
Logs, and X-Ray scanners are **already shipped on `main`** and already emit
observability objects as facts:

| Source object | Scanner (on main) | Emitted as | Identity / anchor |
| --- | --- | --- | --- |
| CloudWatch metric alarm | `go/internal/collector/awscloud/services/cloudwatch/scanner.go` | `aws_resource` (`ResourceTypeCloudWatchMetricAlarm`) + `aws_relationship` (`cloudwatch_alarm_observes_metric`, `cloudwatch_alarm_notifies_sns_topic`) | ARN + name; `CorrelationAnchors: [alarmARN, name]` |
| CloudWatch composite alarm | same | `aws_resource` + `cloudwatch_composite_alarm_has_child_alarm` | ARN + name |
| CloudWatch dashboard | same | `aws_resource` (`ResourceTypeCloudWatchDashboard`), metadata-only (no body JSON) | ARN + name |
| CloudWatch Logs log group | `.../services/cloudwatchlogs/scanner.go` | `aws_resource` (`ResourceTypeCloudWatchLogsLogGroup`) + `cloudwatch_logs_log_group_uses_kms_key` | log group ARN + name |
| X-Ray group / sampling rule | `.../services/xray/scanner.go` | `aws_resource` + `xray_sampling_rule_matches_service` | rule/group ARN + name; carries `service_name` |

Crucially, those facts already flow through the **`CloudResource` node
materialization pipeline** that #805 landed:

- `DomainAWSResourceMaterialization` (`go/internal/reducer/aws_resource_materialization.go`)
  projects every `aws_resource` fact — alarms, dashboards, log groups, X-Ray
  objects included — into canonical `CloudResource` graph nodes keyed by a stable
  `uid = hash(account_id, region, resource_type, resource_id)`.
- `DomainAWSRelationshipMaterialization` (`go/internal/reducer/aws_relationship_materialization.go`)
  projects `aws_relationship` facts into `(:CloudResource)-[:AWS_<type>]->(:CloudResource)`
  edges, gated on the `GraphProjectionPhaseCanonicalNodesCommitted` readiness phase.

So the observability **objects** are already nodes in the graph today. What does
**not** exist is the **coverage correlation read model**: the reducer-owned truth
that answers *"which monitored resource/service has an alarm / log group / trace
sampling attached, and which has none (a coverage gap)."* That gap — not the raw
ingestion — is the deliverable of #391.

**Net effect on the recommendation:** #391 splits cleanly into two slices. The
**AWS-native coverage correlation** slice is build-safe today against real facts
on `main` using the #390 (`service_catalog_correlation`) provenance-only template
plus the #805 readiness-gate pattern. The **generic multi-provider** slice
(Datadog/Prometheus monitors, scrape jobs, pipelines, paging alerts) remains
"needs more design" and is correctly blocked on a collector that does not exist.
This memo designs the first and scopes the second out with explicit gates.

---

## 1. Problem and acceptance criteria

### Problem (from the issue)

Correlate observability source objects through stable service / workload / repo /
cloud / catalog anchors, so API/MCP surfaces can answer whether a given resource
or service has observability coverage, and so coverage **gaps / drift** (uncovered
resources) are visible. Emit **coverage findings**, not health assertions derived
from historical telemetry values.

### Acceptance criteria (verbatim from the issue, annotated)

1. Outcomes include `exact`, `derived`, `ambiguous`, `unresolved`, `stale`, and
   `rejected`. — the six-outcome contract, identical to
   `ServiceCatalogCorrelationOutcome` in
   `go/internal/reducer/service_catalog_correlation.go`.
2. Title-only, raw metric-name-only, high-cardinality, stale, or unsafe signals
   are **suppressed** (kept as `rejected`/`unresolved`, never promoted to truth).
3. API/MCP surfaces can answer whether a service has dashboards, monitors, scrape
   jobs, telemetry pipelines, and paging alerts.
4. Telemetry reports dashboard, monitor, scrape job, rule, pipeline, and alert
   correlation outcomes.

### How this memo maps the criteria to what is buildable now

| Criterion | AWS-native slice (build-safe now) | Generic slice (needs collector) |
| --- | --- | --- |
| Six outcomes | Yes — reuse the exact outcome enum and provenance-only contract from #390. | Same enum reused later; no new design needed. |
| Suppress weak signals | Yes — `cloudwatch_alarm_observes_metric` already redacts customer-tag dimension values (`dimensionSummary`, `redact.Key`); raw-metric-name-only alarms map to `rejected`. | Pending generic collector's own redaction contract. |
| API/MCP answers coverage | "alarms / log groups / traces" answerable now from CloudWatch + X-Ray facts. | "monitors / scrape jobs / pipelines / paging alerts" (Datadog/Prometheus) blocked. |
| Telemetry outcome counters | Yes — add one `ObservabilityCoverageCorrelations` counter dimensioned by outcome + coverage signal. | Same counter, extra signal dimensions later. |

The AWS-native slice satisfies criteria 1, 2, 4 fully and criterion 3 for the
CloudWatch/X-Ray signal classes. The generic slice is the remaining open work and
is the subject of Open Question Q1.

---

## 2. How it fits Eshu

Standard write path (`docs/public/architecture.md` §"Write Path"):

```
collector (cloudwatch / cloudwatchlogs / xray scanners — already on main)
  -> emit aws_resource + aws_relationship facts (Postgres facts, redacted, metadata-only)
  -> projector enqueues reducer intent for the scope/generation
  -> REDUCER:
       Stage A (exists, #805): aws_resource -> CloudResource graph nodes
                               aws_relationship -> AWS_<type> edges (readiness-gated)
       Stage B (THIS issue):   observability_coverage_correlation
                               reads CloudResource nodes + observability aws_resource/
                               aws_relationship facts -> emits durable coverage-finding
                               reducer facts (provenance-only) + optional COVERS edges
  -> query/MCP/API read the coverage read model
```

The correctness boundary holds: scanners observe AWS truth; the reducer decides
coverage truth. This domain is **cross-source** (alarm/log/trace objects vs. the
monitored resource/service they cover) and **cross-scope** (a resource in one
scan scope may be covered by an alarm discovered in another), satisfying the
`OwnershipShape` invariant in `registry.go`.

Placement decision: this is a **reducer correlation domain**, mirroring
`service_catalog_correlation` (#390) and `aws_cloud_runtime_drift` (#39), **not**
a new collector. The facts already exist; only the correlation read model is new.

---

## 3. Fact / schema shape

### 3.1 Inputs (all already on `main` — no collector change)

- **`aws_resource`** facts (`facts.AWSResourceFactKind`) whose `resource_type` is
  an observability object:
  `ResourceTypeCloudWatchAlarm`, `ResourceTypeCloudWatchCompositeAlarm`,
  `ResourceTypeCloudWatchDashboard`, `ResourceTypeCloudWatchLogsLogGroup`,
  X-Ray group / sampling-rule types. Each carries `arn`, `resource_id`, `name`,
  `account_id`, `region`, `service_kind`, and `correlation_anchors`.
- **`aws_relationship`** facts (`facts.AWSRelationshipFactKind`) that link an
  observability object to what it watches:
  `cloudwatch_alarm_observes_metric` (alarm → metric identity, dimensions
  redacted), `cloudwatch_alarm_notifies_sns_topic` (paging fan-out),
  `cloudwatch_logs_log_group_uses_kms_key`, `xray_sampling_rule_matches_service`
  (carries `service_name`).
- **`aws_resource`** facts for the **monitored** resources (EC2, RDS, Lambda,
  ELB, etc.) — the coverage *targets*. Already materialized as `CloudResource`
  nodes by #805.

### 3.2 New reducer-owned output fact kind

Add **one** durable reducer fact kind, declared next to the other reducer
correlation kinds (the #390 pattern: a package-level `const` in the writer file,
mirroring `serviceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"`):

```
observabilityCoverageCorrelationFactKind = "reducer_observability_coverage_correlation"
```

It is written through the shared `canonicalReducerFactInsertQuery` path exactly
like `PostgresServiceCatalogCorrelationWriter` — no new table, no schema DDL in
the fact store. **This is why #391's `risk:schema` label can be discharged with a
metadata-only fact-kind addition, not a migration.**

### 3.3 Decision record (the payload shape)

Mirror `ServiceCatalogCorrelationDecision`. One decision per **coverage edge
candidate** — i.e. per (observability object, target resource-or-service) pair,
plus one per **uncovered target** (the gap finding). Concrete fields:

| Field | Type | Meaning / redaction |
| --- | --- | --- |
| `provider` | string | `aws` (v1); future `datadog`, `prometheus`. |
| `coverage_signal` | string | `alarm` / `composite_alarm` / `dashboard` / `log_group` / `trace_sampling` / `paging` — the six AWS-native signal classes (maps to the issue's dashboard/monitor/scrape/rule/pipeline/alert vocabulary). |
| `observability_object_ref` | string | ARN/name of the alarm/log group/etc. Already redacted upstream; no body JSON is ever ingested (dashboard body is excluded by the scanner). |
| `observability_resource_uid` | string | `CloudResource.uid` of the observability object (recomputed via `cloudResourceUID`). |
| `target_uid` | string | `CloudResource.uid` of the monitored resource, when resolved. |
| `target_service_ref` | string | service identity for `xray_sampling_rule_matches_service` (the `service_name`); empty for resource-only coverage. |
| `outcome` | enum | one of the six (§5). |
| `reason` | string | human-readable classification reason (the #390 convention). |
| `coverage_status` | string | `covered` / `gap` / `ambiguous` / `stale` / `rejected` — the operator-facing roll-up. |
| `provenance_only` | bool | true unless an exact uid/ARN match proves the coverage edge. |
| `resolution_mode` | string | `arn` / `bare_id` / `correlation_anchor` / `service_name` — which index resolved the target (mirrors #805 §5.2 join modes). |
| `candidate_target_uids` | []string | populated on `ambiguous`. |
| `evidence_fact_ids` | []string | source `aws_resource` / `aws_relationship` fact IDs. |
| `source_layers` | []string | `truth.LayerObservedResource` (these are observed, not declared); `LayerSourceDeclaration` only if an IaC-declared alarm is later joined. |

Redaction posture: **no new redaction surface is introduced.** All inputs are
already metadata-only and customer-tag-redacted at the scanner. The reducer
**must not** ingest metric *values* or dashboard body JSON (the scanner already
forbids both), so the "no health assertions from historical telemetry values"
acceptance criterion is satisfied structurally, not by policy. The
`reason`/`evidence_fact_ids` fields carry IDs and classifications only, never raw
dimension values.

Stable identity (for the fact `StableFactKey` and `FactID`, per
`serviceCatalogCorrelationIdentity`):
`(scope_id, generation_id, provider, coverage_signal, observability_object_ref, target_uid)`.
The empty-`target_uid` gap finding keys on `(…, target_resource_uid)` of the
uncovered resource so gaps are stable and de-duplicated across retries.

---

## 4. Reducer projection / read-model design

### 4.1 Domain, intent, registry wiring (mirrors #390 exactly)

- **`intent.go`** — add `DomainObservabilityCoverageCorrelation Domain =
  "observability_coverage_correlation"` next to `DomainServiceCatalogCorrelation`,
  with a doc comment matching the existing style (cross-source, cross-scope,
  provenance-until-corroborated).
- **`registry.go`** — add an **additive** `observabilityCoverageCorrelationDomainDefinition()`
  (not in `DefaultDomainDefinitions`, exactly like the service-catalog and AWS
  drift domains) with:
  ```
  Ownership: OwnershipShape{CrossSource: true, CrossScope: true,
                            CanonicalWrite: true, CounterEmit: true}
  TruthContract: truth.Contract{CanonicalKind: "observability_coverage_correlation",
                                SourceLayers: []truth.Layer{truth.LayerObservedResource}}
  ```
- **`defaults.go`** — add a wiring gate in `implementedDefaultDomainDefinitions`:
  register the domain **only** when `handlers.FactLoader != nil &&
  handlers.ObservabilityCoverageCorrelationWriter != nil` (and, for the optional
  graph-edge phase, when `handlers.ReadinessLookup != nil`). This is the honest
  additive pattern — registering without the writer would silently drop every
  intent, the exact failure mode `registry.go`'s comments warn against.
- **`DefaultHandlers`** — add `ObservabilityCoverageCorrelationWriter
  ObservabilityCoverageCorrelationWriter` next to
  `ServiceCatalogCorrelationWriter`.

### 4.2 Queue work kind / intent emission

The projector enqueues the intent when observability `aws_resource` facts appear
for a scope generation, mirroring `buildAWSCloudRuntimeDriftReducerIntent` (the
existing `aws_resource` → reducer trigger). Because the coverage correlation must
read `CloudResource` nodes that #805 Stage A materializes, the **graph-edge phase
of this domain gates on `GraphProjectionPhaseCanonicalNodesCommitted`** on the
`GraphProjectionKeyspaceCloudResourceUID` keyspace — the identical readiness gate
`AWSRelationshipMaterializationHandler` uses. The **fact-only phase** (writing
coverage-finding reducer facts) does **not** need the gate, because it reads facts
not nodes; this lets the read model populate even before the graph edges land.

### 4.3 Handler shape (mirrors `ServiceCatalogCorrelationHandler`)

```
type ObservabilityCoverageCorrelationHandler struct {
    FactLoader  FactLoader
    Writer      ObservabilityCoverageCorrelationWriter
    Instruments *telemetry.Instruments
    // optional, edge phase only:
    ReadinessLookup GraphProjectionReadinessLookup
}
```

`Handle`:
1. Reject any non-matching `intent.Domain` (the #390/#805 guard).
2. `loadFactsForKinds(ctx, FactLoader, scope, generation, {AWSResourceFactKind,
   AWSRelationshipFactKind})`.
3. Build an **in-memory coverage index** keyed to `CloudResource.uid` — the
   bounded-join discipline from #805 §5.1. Three maps over the *target* resource
   facts (`byARN`, `byResourceID`, `byAnchor`) so each observability relationship
   resolves its target by O(1) lookup. **No per-edge Cypher, no N+1.**
4. Classify each observability object against its relationship target into one of
   the six outcomes (§5). Emit a `gap` finding for every target `CloudResource`
   that has **no** resolving observability relationship of a given signal class
   (the negative/uncovered case — the core deliverable).
5. `Writer.WriteObservabilityCoverageCorrelations(...)` — durable facts.
6. Emit outcome counters; return `Result` with `CanonicalWrites` and an evidence
   summary string in the `serviceCatalogCorrelationSummary` format.

The classifier is a pure function (`BuildObservabilityCoverageDecisions(envelopes,
index)`) so it is table-test-friendly with zero I/O, matching
`BuildServiceCatalogCorrelationDecisions`.

---

## 5. Correlation truth matrix (per `eshu-correlation-truth`)

The six outcomes, made concrete for observability coverage. The invariant: a
coverage **edge** is canonical truth **only** when an observability object
resolves to a target by a stable identity (uid/ARN); name-coincidence and
metric-name-only signals stay provenance and never fabricate a covered edge.

| Outcome | When | `coverage_status` | `provenance_only` | Graph write |
| --- | --- | --- | --- | --- |
| **exact** | Observability relationship target resolves to a `CloudResource.uid` by ARN or bare resource-id (e.g. an alarm whose dimension is `InstanceId=i-…` that matches a scanned EC2 `CloudResource`; an X-Ray sampling rule whose `service_name` matches a known service anchor). | `covered` | false | optional `COVERS` edge (Phase 3) |
| **derived** | Target resolves only after deterministic normalization (e.g. `correlation_anchor` name match, ARN-vs-resource-id canonicalization). Coverage is real but inferred, not exact. | `covered` | false | optional `COVERS` edge |
| **ambiguous** | One observability object's target identity matches **multiple** active `CloudResource` nodes (e.g. a dimension value that is non-unique across regions/accounts in scope). Record `candidate_target_uids`; do **not** pick one. | `ambiguous` | true | **none** |
| **unresolved** | Observability object is valid but its target is not present as a `CloudResource` in this generation (forward-looking: the watched service type was not scanned). Counted, not dropped — the #805 §6 unresolved-target discipline. | `gap` | true | none |
| **stale** | The observability relationship matched only a **tombstoned** resource fact (the watched resource was deleted but the alarm/log group lingers — a real drift signal). | `stale` | true | none |
| **rejected** | Signal is too weak/unsafe to promote: raw metric-name-only alarm with no dimension identity, title-only dashboard with no resource reference, high-cardinality dimension that was redacted to nothing, name-only with no anchor hit. Suppressed per acceptance criterion 2. | `rejected` | true | none |

Plus the **gap (negative) finding** that is unique to coverage: a target
`CloudResource` of a class operators expect to be monitored that has **zero**
resolving observability relationship → emitted as an `unresolved`/`gap` decision
keyed on the *target*. This is the "uncovered resources / coverage drift" half of
the issue and is the negative case `eshu-correlation-truth` mandates.

Mandatory proof matrix (must be covered by table tests before any handler code,
per `golang-engineering` TDD + `eshu-correlation-truth`):

- **Positive:** alarm with `InstanceId` dimension → matching EC2 `CloudResource` →
  `exact`, `covered`, `COVERS` edge eligible.
- **Negative / gap:** RDS `CloudResource` with no alarm relationship → `gap`,
  provenance-only, **no** fabricated coverage.
- **Ambiguous:** dimension value matching two `CloudResource` uids → `ambiguous`,
  both candidates recorded, no edge.
- **Stale:** alarm observing a tombstoned resource → `stale`.
- **Rejected:** metric-name-only alarm (`AWS/Billing EstimatedCharges`, no
  resource dimension) → `rejected`, suppressed.
- **Graph proof:** if Phase 3 lands, inspect that only resolved-exact/derived
  pairs produced `COVERS` edges and no `CloudResource` was fabricated.
- **Query proof:** the coverage read model and any graph edge agree (covered set
  identical), and the gap set is the complement within the target class.

---

## 6. Graph-write plan

Two-phase, smallest-first, mirroring #805's staged delivery.

### Phase A/B (already shipped, dependency only): `CloudResource` nodes + `AWS_<type>` edges
No new work. The observability objects are already nodes; the alarm/log/trace
relationships are already `AWS_<relationship_type>` edges.

### Phase 3 (optional, gated): a reducer-owned `COVERS` coverage edge
Only `exact`/`derived` coverage decisions are eligible. Write shape mirrors the
#805 edge writer (`cloud_resource_edge_writer.go`) — graceful-degradation,
no-fabrication:

```cypher
UNWIND $rows AS row
MATCH (obs:CloudResource {uid: row.observability_resource_uid})
MATCH (target:CloudResource {uid: row.target_uid})
MERGE (obs)-[c:COVERS {coverage_signal: row.coverage_signal}]->(target)
SET c.evidence_source   = 'reducer/observability-coverage',
    c.resolution_mode   = row.resolution_mode,
    c.scope_id          = row.scope_id,
    c.generation_id     = row.generation_id
```

- **Idempotent MERGE key:** logical edge identity is
  `(obs_uid, coverage_signal, target_uid)`. Per `cypher-query-rigor`, the
  immutable `coverage_signal` is in the `MERGE` map (it distinguishes
  alarm-coverage from log-coverage between the same pair) and the mutable
  provenance is in `SET`. Open Question Q3 asks the principal whether to instead
  follow the #805 static-relationship-type token convention
  (`AWS_COVERS_<signal>`) to stay on NornicDB's fast relationship-upsert path,
  which does **not** route property-keyed relationship `MERGE` through the fast
  path (#805 §5.3 measured a 20s timeout for property-map relationship MERGE vs.
  0–1ms for the static token). **Recommendation: adopt the static-token shape**;
  it is the proven-fast contract.
- **Two `MATCH`es before `MERGE`** → if either endpoint node is absent the row
  produces no edge and **no node is fabricated** (the #805 §5.3 safety property,
  also the SQL-relationship writer's property). A coverage edge to a not-yet-
  scanned target simply does not materialize; it is counted `unresolved`.
- **No `COVERS` schema constraint needed** beyond the existing
  `cloud_resource_uid_unique` constraint + `nornicdb_cloud_resource_uid_lookup`
  index #805 already added; both `MATCH` anchors hit that uid index, so there is
  no label scan. No new index in `go/internal/graph/schema.go` for Phase 3.

### Conflict-key partitioning and "Serialization Is Not A Fix"

- **Conflict domain:** `COVERS` edges keyed by `(obs_uid, coverage_signal,
  target_uid)`, partitioned by scope generation. The fact-write conflict domain is
  the coverage-finding fact keyed by the §3.3 stable identity.
- **Partition by source uid:** run the edge phase as a shared-projection domain
  partitioned by `obs_uid` (`PartitionKey`), so concurrent workers never touch
  the same observability node's outgoing `COVERS` edges — the #805 §7 model.
  Useful concurrency is preserved; overlap is removed by partitioning on the
  conflict key, **not** by single-threading.
- **Retry scope:** idempotent `MERGE` → a retried batch cannot duplicate edges or
  facts. The fact writer's stable `FactID`/`StableFactKey` (§3.3) makes the
  durable fact write idempotent under retry, exactly like
  `serviceCatalogCorrelationFactID`.
- **Stale/superseded generations:** the existing generation-supersede check skips
  intents whose generation is no longer active (`ResultStatusSuperseded`).
  Prior-generation `COVERS` retraction filters on the **edge's own** `c.scope_id`
  (set at write time), never on a `CloudResource` node property — the #805 §7
  correctness note: `CloudResource` nodes are cross-scope canonical and carry no
  scope, so a node-scoped delete predicate would leak stale edges.
- **Empty/first generation:** zero observability facts → zero rows → no-op write,
  no retract on first generation (the `PriorGenerationCheck` skip).

Serialization is **not** used as a correctness device. Per the repo rule, the
write is idempotent under concurrent execution and partitioned by conflict key.

---

## 7. Risks

**Accuracy**
- *False coverage from non-unique dimensions.* A CloudWatch dimension value
  (`InstanceId`, `FunctionName`) can be non-unique across accounts/regions in a
  multi-scope graph. Mitigation: resolve target uid from the full
  `(account_id, region, resource_id, resource_type)` identity, never the bare
  dimension string; non-unique matches go `ambiguous`, never `exact`.
- *Metric-name-only alarms (`AWS/Billing`, custom namespaces) imply no resource
  coverage.* Mitigation: `rejected` outcome, suppressed (criterion 2).
- *Gap false positives.* Declaring a resource "uncovered" requires knowing the
  expected-monitored class. v1 emits gaps only for resource classes that have at
  least one covered peer in scope (evidence-bounded), avoiding "everything is a
  gap" noise. Open Question Q2.

**Performance**
- Bounded by O(R + E): R = observability + target `aws_resource` facts, E =
  observability `aws_relationship` facts in the generation. In-memory index build
  is O(R); resolution is O(E) map lookups; the graph write is one batched
  `UNWIND` per coverage signal. **No per-edge graph round trip, no unbounded
  traversal, no N+1** — the #805 §8 contract. Stop threshold: profile before merge
  if any stage exceeds the fixture-corpus known-normal band by >10% or >60s
  (`eshu-diagnostic-rigor`).
- Phase 3 adds `COVERS` edges to a label (`CloudResource`) that may already be the
  graph's largest AWS label; the partitioned, batched, uid-anchored write keeps it
  bounded, but this is the item that most warrants a real NornicDB Compose probe
  before merge (Q3).

**Concurrency**
- The conflict domain, partition key, transaction scope, retry scope, and
  supersede/retract behavior are specified in §6 and follow the proven #805 model.
  Residual risk: the gap-finding writer touches the **target** resource's identity,
  which could be the conflict domain of a *different* partition; v1 keeps gap
  findings as facts only (no graph edge), so they have no edge-level conflict
  domain — only the idempotent fact key. This is the safe default.

**Schema**
- `risk:schema` is discharged by a **fact-kind addition** (`reducer_observability_
  coverage_correlation`) written through the existing
  `canonicalReducerFactInsertQuery` — no migration, no new table. Phase 3 adds a
  `COVERS` relationship type but **no new constraint/index** (it reuses the #805
  uid constraint/index). If the principal prefers no graph write at all in v1,
  the domain registers as `CounterEmit`-capable and ships fact-only (the
  `config_state_drift` precedent), deferring `COVERS` entirely.

---

## 8. Telemetry (operator metrics/spans for 3 AM)

- **Counter:** add `ObservabilityCoverageCorrelations metric.Int64Counter` to
  `telemetry.Instruments` (next to `ServiceCatalogCorrelations`,
  `CICDRunCorrelations`), dimensioned by `domain`, `outcome` (six values), and
  `coverage_signal` (alarm/composite_alarm/dashboard/log_group/trace_sampling/
  paging). This directly satisfies acceptance criterion 4 — every dashboard /
  monitor(alarm) / scrape(log_group) / rule / pipeline / alert(paging) class is a
  counter dimension.
- **Edge tally (Phase 3):** a `materialized` vs `unresolved` count dimensioned by
  `coverage_signal` and `resolution_mode`, plus an `unresolved_target_by_signal`
  field in the completion log — the #805 §6/§9 unresolved-target surface, so an
  operator can answer "which signal class is losing coverage edges, and is it
  because the watched service type isn't scanned yet."
- **Structured completion log:** `observability coverage correlation completed`
  with `scope_id`, `generation_id`, `fact_count`, `decision_count`, per-stage
  durations (`load` / `build_index` / `classify` / `fact_write` /
  `graph_write` / `phase_publish`), mirroring `logAWSResourceMaterializationCompleted`
  so fact-load time, classify time, and graph-write time are separable at 3 AM.
- **Span:** `query.observability_coverage_correlations` registered in the
  telemetry contract list (the `SpanQueryServiceCatalogCorrelations` precedent),
  for the read surface.
- This is **runtime-affecting** work, so each implementation PR must carry the
  `cypher-query-rigor` / `concurrency-deadlock-rigor` evidence markers
  (`Performance Evidence:` or `No-Regression Evidence:` + `Observability
  Evidence:`) in a tracked file, gated by
  `scripts/verify-performance-evidence.sh`.

---

## 9. Phased PR plan (smallest-first)

| PR | Contents | Risk surface | Gate |
| --- | --- | --- | --- |
| **PR 1 — read model (facts only)** | New `DomainObservabilityCoverageCorrelation`; `intent.go` + additive `registry.go` definition + `defaults.go` wiring gate; `ObservabilityCoverageCorrelationHandler` + pure `BuildObservabilityCoverageDecisions`; `PostgresObservabilityCoverageCorrelationWriter` writing `reducer_observability_coverage_correlation` facts; in-memory coverage index; six-outcome classifier incl. gap findings; the `ObservabilityCoverageCorrelations` counter; package docs (`doc.go`/`README.md`/`AGENTS.md` already exist for `reducer`, so update them). **No graph write.** Full table-test proof matrix (§5). | Postgres fact write only; no graph, no schema migration. Lowest risk. | Focused `go test ./internal/reducer ./internal/telemetry`; `verify-performance-evidence.sh` (new reducer correlation path); `verify-package-docs.sh`. |
| **PR 2 — query / MCP surface** | Read handler + MCP/API tool that answers "does service/resource X have alarm/log/trace coverage, and list coverage gaps," reading the PR 1 facts. OpenAPI + `http-api.md` lockstep if a new wire contract is added. Bounded reads (scope/limit/timeout/ordering) per `eshu-mcp-call-rigor`. | Read-only. | `go test ./internal/query ./internal/mcp ./cmd/api ./cmd/mcp`; OpenAPI lockstep check. |
| **PR 3 — `COVERS` graph edge (optional, gated)** | Reducer-owned `COVERS` edge for `exact`/`derived` decisions, gated on `GraphProjectionPhaseCanonicalNodesCommitted`; backend-neutral edge writer reusing the #805 static-relationship-token shape; partitioned by `obs_uid`; unresolved/materialized tally. Real NornicDB Compose probe evidence (Q3). | Graph write; highest risk; isolated behind PR 1/2 already shipping value. | NornicDB + Neo4j conformance; Compose probe; `cypher-performance` evidence. |

PR 1 alone satisfies acceptance criteria 1, 2, 4 and the data half of 3. PR 2
completes criterion 3's query surface. PR 3 is value-add graph reachability and is
explicitly optional — the read model is queryable without it.

The **generic multi-provider slice** (Datadog/Prometheus monitors, scrape jobs,
pipelines, paging alerts) is **out of scope for all three PRs** and is gated on a
separate collector issue under #373; the six-outcome enum, the writer, and the
counter are designed to extend to it by adding `provider` values and
`coverage_signal` classes, with **no rework** of the reducer contract.

---

## 10. Open questions for the principal

- **Q1 — provider scope of v1.** Confirm v1 is **AWS-native only** (CloudWatch +
  CloudWatch Logs + X-Ray facts already on `main`) and that the generic
  multi-provider observability collector stays a separate, blocked issue. The
  issue's source ADR implies a generic model; this memo argues the AWS slice is
  the build-safe subset that delivers real value now. Agree?
- **Q2 — gap-finding scope.** Should "uncovered resource" gaps be emitted for
  **all** `CloudResource` of a class, or only classes with at least one covered
  peer in scope (evidence-bounded, avoids "everything is a gap")? This memo
  recommends the evidence-bounded default for v1.
- **Q3 — `COVERS` edge in v1 vs. deferred, and edge shape.** Ship PR 3 in the
  first wave, or land fact-only (PR 1/2) first and defer `COVERS`? And if we
  write the edge, adopt the #805 static-relationship-token shape
  (`AWS_COVERS_<signal>`, proven 0–1ms) over a property-keyed `MERGE` (measured
  20s timeout on NornicDB)? Recommendation: defer PR 3 behind PR 1/2, and if/when
  built, use the static token.
- **Q4 — X-Ray service anchor.** `xray_sampling_rule_matches_service` carries a
  `service_name` string, not a `CloudResource.uid`. Is there a canonical service
  identity reducer this should resolve against, or does X-Ray coverage stay
  `derived`/`provenance_only` keyed on the service name until a service-identity
  anchor exists? This memo treats it as `derived` until corroborated.
- **Q5 — domain ownership.** This sits between the AWS scanner fleet and the
  reducer/projection owners. Confirm the reducer/projection owners hold the domain
  (consistent with #390/#805), with the AWS fleet owning only the fact contract.

---

## 11. Recommendation

**Build-safe for the AWS-native slice; needs-more-design for the generic
multi-provider slice.** Recommend approving **PR 1 (fact-only read model) and PR 2
(query/MCP surface)** to proceed against real CloudWatch/CloudWatch Logs/X-Ray
facts already on `main`, using the proven #390 provenance-only correlation
template and the #805 bounded-join / readiness-gate / no-fabrication patterns.
**Defer PR 3 (`COVERS` graph edge)** pending the Q3 decision and a NornicDB
Compose probe. **Keep the generic Datadog/Prometheus observability collector a
separate, blocked issue** under #373 — the reducer contract designed here extends
to it with no rework.

The issue's release-lane triage ("not ready, no collector exists") is correct for
the generic slice but understated the AWS-native facts already shipped; this memo
unblocks the buildable subset without overreaching into the part that genuinely
lacks inputs.
