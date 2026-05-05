# ADR: Multi-Source Reducer Scope and Consumer Contract

**Date:** 2026-04-20
**Status:** Accepted (2026-04-20)
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `2026-04-20-terraform-state-collector.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- Plan: `docs/superpowers/plans/2026-04-20-terraform-state-collector-architecture-workflow.md`
- Plan: `docs/superpowers/plans/2026-04-20-aws-cloud-scanner-collector-architecture-workflow.md`

This ADR **blocks** the remaining coordinator slice work (per-collector
downstream gates + second-pass domain first-class gates) and **gates** the
`tfstate/*` and `aws/*` collector implementation issues.

---

## Status Review (2026-05-03)

**Current disposition:** Architecture accepted; implementation partial.

The phase registry, workflow contracts, reducer skeletons, and shared consumer
shape exist. This ADR remains the contract future collectors must plug into.

**Remaining work:** Terraform-state and AWS collectors, real projectors, DSL
materialization, and collector-backed MCP/HTTP consumer tools are still
incomplete.

## Context

The platform currently runs a Git-only facts pipeline end-to-end:

```
collector-git → facts queue → reducer → canonical graph → query/MCP
```

Two new collectors are imminent (tfstate, aws). Existing ADRs cover:

- how collectors emit typed facts (per-collector ADRs)
- how coordinator scopes/generations/fencing work (coordinator ADRs)
- how the DSL correlates evidence across sources at the evidence layer
  (2026-04-19 DSL ADR)

What is **not yet codified**:

1. Whether the reducer owns all collector queues or splits per source
2. Where the DSL evaluator runs, when it fires, and what it publishes
3. Which canonical graph writes happen for each new fact kind, and who
   owns each write
4. How `scope_id` and `generation_id` unify across Git/tfstate/aws
5. Which MCP tools and HTTP endpoints surface multi-source truth, and
   what fact fields each one demands from producers
6. Which coordinator phases must be first-class completion gates to make
   cross-source queries deterministic

Without this contract, collector fact shapes are guesses. Downstream
consumers either work around missing fields (fragile) or block on schema
churn (wasted cycles).

---

## Problem Statement

Two failure modes if we ship collectors before the consumer contract:

### Failure mode 1 — fact shapes miss consumer requirements

Example: MCP tool `find_deployed_version(service, environment)` needs to
join Git service definition → tfstate address → AWS running resource. It
needs `module_source_path` on every tfstate fact AND `provider_resolved_arn`
where computable. If tfstate ADR only guarantees `address` + `attributes`,
the DSL has no deterministic anchor and must reverse-engineer ARNs at
query time — the exact "fallback query paths" anti-pattern we committed
to avoid.

### Failure mode 2 — cross-source convergence not deterministic

Example: drift query `find_unmanaged_resources(account, region)` must
compute `AWS_resources ∖ (terraform ∪ git)`. If coordinator completeness
only tracks the shared `canonical_nodes_committed` phase, the query is
defined to return results before the AWS collector has finished its
generation — it will report false positives. A new first-class phase
`cross_source_anchor_ready` is required, and it must be published by a
named owner.

Both failures are prevented by deciding the contract now.

---

## Decision 1 — Reducer Scope

**Decision:** The reducer process owns queues for **all collectors**. Per-source
projector packages run inside the same binary, sharing:

- the work-queue consumer pool (bounded by `ESHU_REDUCER_WORKER_POOL`)
- the canonical graph writer contract (Neo4j writer + content store + projector
  idempotency guarantees)
- the coordinator-handshake publication path (`graph_projection_phase_state`)
- the DSL evaluation stage

Per-source packages:

- `go/internal/reducer/git/*` (exists today)
- `go/internal/reducer/tfstate/*` (new)
- `go/internal/reducer/aws/*` (new)
- `go/internal/reducer/dsl/*` (new; cross-source correlation)
- `go/internal/reducer/tags/*` (new; tag normalization into canonical shape)

**Why single reducer:**

1. Cross-source correlation must happen in one transaction-aware process.
   A separated "dsl service" would need to re-read already-committed
   canonical rows and could not share idempotency guarantees.
2. Convergence ordering is a single concern. Coordinator publishes
   readiness across sources; reducer publishes canonical phase completion;
   a single process makes the handshake atomic.
3. Backfill and repair already live in `go/internal/recovery`; forking a
   second writer surface would double the recovery burden.

**Why rejected split-per-source:** see Alternatives §A.

---

## Decision 2 — DSL Evaluation Stage

**Decision:** DSL evaluation is a **reducer phase** that fires after all
source-local projections for a scope have committed their canonical
writes and **before** the `cross_source_anchor_ready` phase publishes.

Phase ordering for a scope bundle `(scope_id, collector_kind)`:

```
source-local projection (collector_kind-specific)
        ↓
canonical writes (Neo4j, content store)
        ↓
per-source canonical phase publication
        ↓
DSL evaluator (waits for all collector_kinds in scope to reach above)
        ↓
resolved_relationships writes + drift warnings
        ↓
cross_source_anchor_ready phase publication
        ↓
second-pass domains (deployment_mapping, workload_materialization) rerun
```

DSL output shapes:

- `(:ResolvedRelationship)` nodes (existing contract from 2026-04-19 ADR)
- `(:DriftObservation)` nodes for drift queries (new)
- `(:UnmanagedResource)` markers for cloud resources not resolvable to
  any terraform/git source (new)

DSL idempotency:
- Keyed by `(scope_id, correlation_anchor, rule_id)`.
- Re-running DSL over same inputs writes identical output rows (UPSERT by
  the key). No drift detector false positives from reruns.

---

## Decision 3 — Canonical Write Responsibility Matrix

Every fact kind maps to exactly one canonical writer. Dual ownership is
forbidden.

| Fact kind | Source collector | Canonical write target | Owner |
|---|---|---|---|
| `source_local_fact` | git | `(:File)`, `(:Function)`, `(:Class)` | `reducer/git/code_entity_projector` |
| `deployable_unit_fact` | git | `(:DeployableUnit)`, `CONTAINS` | `reducer/git/deployable_unit_projector` |
| `workload_identity_fact` | git | `(:Service)`, `DEFINES` | `reducer/git/workload_identity_projector` |
| `terraform_resource_fact` | terraform_state | `(:TerraformResource)`, `DEFINED_IN` (→ module file) | `reducer/tfstate/resource_projector` |
| `terraform_output_fact` | terraform_state | `(:TerraformOutput)` | `reducer/tfstate/output_projector` |
| `terraform_module_fact` | terraform_state | `(:TerraformModule)`, `SOURCES` | `reducer/tfstate/module_projector` |
| `aws_resource_fact` | aws | `(:CloudResource)` + typed label (e.g. `:LambdaFunction`) | `reducer/aws/resource_projector` |
| `aws_relationship_fact` | aws | native rel (e.g. `TARGETS`, `MOUNTS_VOLUME`, `RUNS_IMAGE`) | `reducer/aws/relationship_projector` |
| `aws_tag_observation_fact` | aws | merged into `(:CloudResource).tags_normalized` | `reducer/tags/normalizer` |
| `aws_dns_record_fact` | aws | `(:DNSRecord)`, `POINTS_TO` | `reducer/aws/dns_projector` |
| `aws_image_reference_fact` | aws | `IMAGE_REFERENCE` rel to `(:ContainerImage)` | `reducer/aws/image_projector` |
| DSL output: `resolved_relationship` | n/a | `(:ResolvedRelationship)` + typed rel (e.g. `DEPLOYS_FROM`, `MANAGED_BY`, `RUNS_IN`) | `reducer/dsl/evaluator` |
| DSL output: drift | n/a | `(:DriftObservation)`, `(:UnmanagedResource)` | `reducer/dsl/drift_evaluator` |

Writer invariants:

- Every writer is idempotent by UPSERT keyed on `(scope_id, generation_id, fact_stable_key)`.
- Every writer publishes one row in `graph_projection_phase_state` keyed
  on `(collector_kind, keyspace, phase)` at end-of-batch.
- Only the owning writer may write its row. No cross-writer canonical
  writes. No cross-writer phase publications.

---

## Decision 4 — Scope and Generation Unification

`scope_id` is the primary key for per-source work. `(scope_kind, identifier_pair)`:

| `scope_kind` | `identifier_pair` | Source |
|---|---|---|
| `repo` | `{owner, name}` | git |
| `terraform_state` | `{backend_kind, locator_hash}` | terraform_state |
| `account_region` | `{account_id, region}` | aws (most services) |
| `account` | `{account_id}` | aws (IAM, Route53 — global services) |
| `webhook_source` | `{provider, endpoint_id}` | webhook (future) |

`generation_id`:

- `git`: commit SHA + coordinator-assigned monotonic `generation_seq`
  (SHA alone is not monotonic; seq lets workflow reconciliation sort)
- `terraform_state`: native state `serial` (monotonic per lineage; lineage
  rotation = new generation series)
- `aws`: coordinator-assigned monotonic `int64` (no native monotonic
  indicator from AWS; coordinator owns it)
- `webhook`: event timestamp + coordinator sequence

`collector_kind` (frozen enum):

- `git`
- `terraform_state`
- `aws`
- `webhook`
- `bootstrap` (legacy; planned deprecation after tfstate+aws land)

Every fact envelope MUST carry the triple `(scope_id, collector_kind, generation_id)`.

**Implication for coordinator:**

The phase contract is keyed on `(collector_kind, keyspace, phase)`.
`keyspace` is the canonical node type the phase publishes about
(e.g. `cloud_resource_uid`, `terraform_resource_uid`, `code_entities_uid`).
This matches the generalization codex completed in the current slice.

---

## Decision 5 — Consumer Surfaces

### 5.1 New MCP tools

| Tool | Purpose | Required fact fields from collectors |
|---|---|---|
| `find_deployed_version(service_identifier, environment?)` | resolves deployed version across git/tfstate/aws | `service_tag_normalized` (aws), `module_source_path` (tfstate), `version_anchor` (git) |
| `trace_arn_to_code(arn)` | chains aws → tfstate → git repo/file | `provider_resolved_arn` (tfstate), `arn` (aws), `module_source_path` (tfstate) |
| `list_account_ownership(account_id, region?)` | map of aws resources → terraform/git ownership | `arn`, `module_source_path` |
| `find_unmanaged_resources(account_id, region?)` | aws resources not in terraform ∪ git | `arn` (aws), `provider_resolved_arn` (tfstate) |
| `find_orphaned_state(scope_id)` | terraform resources with no cloud counterpart | `provider_resolved_arn`, `arn` |
| `find_iac_drift(arn)` | diff between tfstate attributes and aws observed | attribute shape parity (redaction-aware) |
| `correlate_by_tag(tag_key, tag_value)` | all resources matching a normalized tag | `tags_normalized` |

### 5.2 Updates to existing MCP tools

- `resolve_entity`: returns `source_mix` showing which collector_kinds
  contributed evidence and their confidence weighting.
- `find_code`: returns `linked_cloud_resources` when an entity has
  resolved_relationships pointing into `(:CloudResource)`.
- `get_repo_context`: includes `owned_cloud_resources` aggregate count
  per account_region.

### 5.3 New HTTP endpoints

- `GET /api/v1/resources/by-arn/{arn}` → resource + ownership chain
- `GET /api/v1/accounts/{id}/ownership?region=...` → map
- `GET /api/v1/drift/unmanaged?account=...&region=...&service=...` → set
- `GET /api/v1/drift/orphaned?scope_id=...` → set
- `GET /api/v1/drift/diff?arn=...` → per-attribute diff

### 5.4 Consumer contract freeze

The fact-field columns in §5.1 are **required**. Collector ADRs for
tfstate and aws must be amended to list these fields as first-class
envelope attributes, not merely as derived DSL outputs.

---

## 6. Worked Query Traces

Each trace documents the exact canonical shape and fact fields a query
depends on. If a collector cannot emit a field named here, the query is
non-functional — this is the explicit back-pressure on collector design.

### 6.1 "What version of api-service runs in ops-prod?"

Path:

```
MCP: find_deployed_version("api-service", "ops-prod")
 ↓
Cypher:
  MATCH (s:Service {canonical_name: "api-service"})
    -[:DEPLOYS_FROM]-> (tfr:TerraformResource)
    -[:DEFINED_IN]-> (f:File) -[:IN_REPO]-> (r:Repo)
  MATCH (tfr)-[:PROVIDERS]-> (cr:CloudResource)
    -[:RUNS_IN]-> (acct:AWSAccount {environment: "ops-prod"})
  RETURN r.commit_sha, tfr.address, cr.image_digest, cr.last_observed_at
```

Required fact fields:

- git `workload_identity_fact`: `service.canonical_name`, `version_anchor` (commit SHA or tag ref)
- tfstate `terraform_resource_fact`: `address`, `module_source_path`, `provider_resolved_arn`, `attributes.image` (redaction-aware extract for container images)
- aws `aws_resource_fact`: `arn`, `resource_type=ecs_service|lambda_function`, `image_digest`, `tags_normalized.environment`

Confidence calculation:

- All three sources agree → `high`
- Git+tfstate agree, aws missing → `medium` (maybe not yet deployed)
- tfstate+aws agree, git missing → `medium` (unmapped service)
- Only one source → `low`

### 6.2 "What cloud resources exist without IaC?"

Path:

```
MCP: find_unmanaged_resources("123456789012", "us-east-1")
 ↓
DSL evaluator produces (:UnmanagedResource) when:
  AWS_resource.arn NOT IN (
    terraform_resource.provider_resolved_arn
    UNION
    git_inferred_arn      -- weak: e.g., ECR repo name in a Dockerfile
  )
```

Required fact fields:

- aws: `arn` (NOT NULL for every resource with an ARN — enum of resources without ARNs documented in aws ADR)
- tfstate: `provider_resolved_arn` (nullable — nil means DSL cannot claim ownership via this state; triggers a different warning: `tfstate_arn_unresolvable`)

Gating:

- DSL must NOT emit unmanaged warnings before `cross_source_anchor_ready`
  has fired for all `(account, region, service)` in scope. Coordinator
  enforces this via the phase contract.

### 6.3 "What tfstate resources are orphaned?"

Path:

```
MCP: find_orphaned_state(scope_id)
 ↓
(:TerraformResource {scope_id}) WHERE provider_resolved_arn IS NOT NULL
  AND NOT EXISTS (CloudResource {arn: same_arn})
```

Required fact fields:

- tfstate: `provider_resolved_arn` NOT NULL for the query to be meaningful
  (resources with unresolvable ARNs are excluded, warning emitted)
- aws: `arn` indexed

Edge case:

- Resource exists in both but in a region aws hasn't scanned yet →
  **not an orphan**. DSL must gate on `cross_source_anchor_ready` for
  the aws side before emitting orphan warnings.

### 6.4 "For ARN X, show code repo + module"

Path:

```
MCP: trace_arn_to_code("arn:aws:lambda:us-east-1:123:function:foo")
 ↓
(:CloudResource {arn: X})
  <-[:PROVIDERS]- (:TerraformResource {provider_resolved_arn: X})
  -[:BELONGS_TO]-> (:TerraformModule)
  -[:SOURCES]-> (:GitRef {repo_id, commit_sha, path})
```

Required fact fields:

- tfstate: `module_source_path` (normalized to `{repo_host, repo_path, ref}`), `provider_resolved_arn`
- aws: `arn`
- git: `(:Repo)` canonical by `(owner, name)` — existing

Fallback when module source is a registry (`app.terraform.io/foo/bar`):

- Terraform registry → repo resolution is a later phase. Launch emits
  `module_source_kind=registry` and the chain terminates at the
  `(:TerraformModule)` node with a warning.

### 6.5 "What services in account Y managed by which Git repos?"

Path:

```
MCP: list_account_ownership("123456789012")
 ↓
per region:
  collect (:CloudResource)-[:PROVIDERS]-(:TerraformResource)
    -[:BELONGS_TO]->(:TerraformModule)-[:SOURCES]->(:GitRef)
  aggregate by repo → count resources
```

Required fact fields: same union as Q4, Q2, Q3.

Output includes a `unmanaged_count` from Q2 path so operators can judge
how much of an account is IaC-covered.

---

## 7. Fact Field Requirements (Back-Propagation)

Collector ADRs must be amended to guarantee:

### 7.1 tfstate ADR additions

Add to `terraform_resource_fact` envelope (first-class fields, not
attribute-map entries):

- `provider_resolved_arn` — `string` nullable; populated when provider
  schema is known for the resource type
- `module_source_path` — `string` nullable; resolved include chain
  terminal path (e.g. `github.com/org/repo//modules/service?ref=v1.2.3`)
- `module_source_kind` — `enum {git, registry, local, unknown}`
- `correlation_anchors` — `[]string` — e.g. `[arn, tag:Service, tag:Environment]`

Add to `terraform_module_fact`:

- `source_kind`, `source_path` (for SOURCES rel)

### 7.2 aws ADR additions

Add to `aws_resource_fact` envelope:

- `arn` — required for all resource types that have an ARN; enum of
  exceptions documented in ADR (e.g. `iam_instance_profile_membership`)
- `resource_type` — frozen enum matching canonical label
- `tags_normalized` — populated by tag normalizer AFTER fact lands;
  collector emits raw `tags` only. Normalized column added to canonical
  node by `reducer/tags/normalizer`.
- `correlation_anchors` — `[]string`

Document fallback key for resources without ARNs (e.g. ECS task def
revision number, IAM policy version ID).

### 7.3 Shared

Every fact envelope carries:

- `scope_id`, `collector_kind`, `generation_id`, `fence_token`
- `correlation_anchors[]`
- `source_confidence` — enum `{high, medium, low}` — derived from source
  semantics (e.g. tfstate with sensitive-output-only = `medium`)

---

## 8. Coordinator Registry Implications

From the decisions above, the coordinator registry
(`RequiredPhasesForCollector`) extends as follows:

```go
git        → [code_entities_uid.canonical_nodes_committed,
              code_entities_uid.semantic_nodes_committed,
              deployable_unit_uid.deployable_unit_correlation,
              service_uid.canonical_nodes_committed,
              service_uid.deployment_mapping,
              service_uid.workload_materialization]
terraform_state → [terraform_resource_uid, terraform_module_uid, cross_source_anchor_ready]
aws        → [cloud_resource_uid, cross_source_anchor_ready]
webhook    → [webhook_event_uid, cross_source_anchor_ready] // future
```

Git-specific readiness note:

- `deployable_unit_uid` is a first-class completion gate, but it is **not**
  a `canonical_nodes_committed` publication today.
- The truthful reducer publication is
  `deployable_unit_uid.deployable_unit_correlation`, emitted by
  `reducer/deployable_unit_correlation` after the bounded admission pass
  finishes.
- This phase must publish even when the bounded slice admits zero candidates,
  because completion means "the deployable-unit decision is final for this
  slice," not "a canonical node was created."

New shared phase: `cross_source_anchor_ready`

- **Owner:** `reducer/dsl/evaluator`
- **Published when:** every `collector_kind ∈ scope` has published its
  canonical phases AND the DSL has completed one pass over the resulting
  canonical rows.
- **Blocks:** second-pass domains (`deployment_mapping`, `workload_materialization`).

Second-pass domains become first-class completion gates:

- `deployment_mapping` — published by `reducer/dsl/deployment_mapping`
- `workload_materialization` — published by `reducer/dsl/workload_materialization`

These two already exist as reducer actions today; the change is making
them publish to `graph_projection_phase_state` so coordinator completeness
reconciliation waits on them.

**This resolves the codex question:** "if we want second-pass domains to
be first-class completion gates, the reducer still needs to publish those
checkpoints explicitly." → **Yes, they do. Required for drift queries to
be deterministic. Landed as part of this ADR's rollout.**

---

## 9. Rollout Plan

Phased:

### Phase 1 — Contract freeze (docs only, no code)

- [x] This ADR
- [x] Amend tfstate ADR with §7.1 field additions
- [x] Amend aws ADR with §7.2 field additions
- [x] Update collector plans with the accepted reducer/consumer field enums
- [ ] Update coordinator ADR to reference this ADR and note that the
      "real remaining work beyond this slice" (codex note) is tracked here

### Phase 2 — Coordinator registry extension

- [x] Add `cross_source_anchor_ready` to phase enum
- [x] Extend `RequiredPhasesForCollector` map (see §8)
- [x] Add deployment_mapping + workload_materialization as first-class
      published phases (reducer changes publish rows)
- [x] Add truthful git deployable-unit readiness publication
      (`deployable_unit_uid.deployable_unit_correlation`)
- [x] Coordinator run reconciliation tests extended for new phases

### Phase 3 — Reducer scaffolding

- [x] Create `go/internal/reducer/tfstate/` package skeleton
- [x] Create `go/internal/reducer/aws/` package skeleton
- [x] Create `go/internal/reducer/dsl/` package scaffolding (evaluator,
      drift evaluator, publication of `cross_source_anchor_ready`)
- [x] Create `go/internal/reducer/tags/normalizer` (tag normalization pack)

### Phase 4 — Collector implementation

- [ ] Unblock `tfstate/*` gate (#103) + issues #105–#109
- [ ] Unblock `aws/*` gate (#104) + issues #110–#115

### Phase 5 — Consumer surfaces

- [ ] HTTP endpoints (§5.3)
- [ ] MCP tool implementations (§5.1)
- [ ] Updates to existing tools (§5.2)
- [ ] End-to-end query traces validated against remote instance

Phases 2 and 3 can start immediately after Phase 1 lands. Phases 4 and 5
are blocked on 2 + 3.

---

## 10. Alternatives Considered

### A — Split reducer per source

Each collector kind gets its own reducer binary.

Rejected:

- Cross-source DSL evaluation requires either a third process or
  per-source partial DSL runs → duplicated correlation logic
- Idempotency guarantees scatter (each reducer has its own writer contract)
- Backfill/repair operations fork

Accepted trade-off of single reducer: larger binary, per-source package
isolation mitigates blast radius.

### B — DSL outside the reducer

DSL evaluator as a standalone service reading committed canonical rows.

Rejected:

- Loses single-writer guarantees for `(:ResolvedRelationship)` — another
  service is a canonical writer
- Ordering becomes eventually-consistent (DSL might lag canonical by
  minutes); drift queries unreliable
- Doubles observability surface

### C — No cross-source phase; rely on eventual consistency

Drift queries return "best effort" answers that may be wrong during
active scans.

Rejected:

- Violates accuracy > performance > reliability priority
- Drift false positives are actively harmful to operators

### D — Let each collector publish its own DSL-ready phase

Collectors run their own anchor resolution.

Rejected:

- Correlation is cross-source; no single collector can know when all
  peers are ready
- Would reintroduce the git-collector mistake pattern (collector too
  knowledgeable about consumers)

---

## 11. Invariants

These must hold for the system to be correct. Every invariant is a test
case in Phase 2 / Phase 3.

- **I1**: Every fact has `(scope_id, collector_kind, generation_id, fence_token)`. A fact without all four is rejected at queue insert.
- **I2**: Every canonical write is owned by exactly one projector. Dual-writer violations caught by a boot-time registry check.
- **I3**: `cross_source_anchor_ready` cannot publish before every
  `RequiredPhasesForCollector(scope)` row is at `completed`. Coordinator
  enforces.
- **I4**: DSL outputs are idempotent under rerun. Re-running over same
  inputs produces same `(:ResolvedRelationship)` and same
  `(:DriftObservation)` rows (UPSERT by key).
- **I5**: Consumer queries that require `cross_source_anchor_ready`
  return a `source_ready` field so operators can tell between "no match"
  and "not yet converged".
- **I6**: Canonical node type labels are frozen enums. New labels require
  ADR amendment.
- **I7**: No canonical writer reads from another collector's fact queue.
  Cross-source joins happen in DSL only.

---

## 12. Observability

Metrics (additions):

| Metric | Type | Labels | Notes |
|---|---|---|---|
| `eshu_dp_dsl_evaluations_total` | counter | `rule_id`, `result` | DSL rule eval outcomes |
| `eshu_dp_dsl_resolved_relationships_total` | counter | `relationship_kind` | `DEPLOYS_FROM`, `RUNS_IN`, etc. |
| `eshu_dp_dsl_drift_observations_total` | counter | `drift_kind` | `unmanaged`, `orphaned`, `attribute_diff` |
| `eshu_dp_cross_source_anchor_ready_wait_seconds` | histogram | `scope_kind` | time from last canonical publish to anchor-ready publish |
| `eshu_dp_canonical_write_duplicate_attempts_total` | counter | `owner` | I2 violations (must be 0 in steady state) |

Spans (additions):

- `reducer.dsl.evaluate` — `{scope_id, generation_id, rule_count, outputs_count}`
- `reducer.dsl.drift_evaluate` — `{scope_id, unmanaged_count, orphaned_count}`
- `reducer.tags.normalize` — `{batch_size, normalization_hits}`

Dashboards to support:

1. "Which scopes are stuck waiting on cross_source_anchor_ready?" —
   gauge of in-flight scopes grouped by missing collector_kind
2. "DSL rule hit rates" — per-rule eval counter + resolution counter
3. "Drift observation rate over time" — alert on spikes suggesting a
   collector fell behind

---

## 13. Security Notes

- DSL evaluator reads canonical rows only — no access to raw collector
  payloads. Redaction contracts are preserved.
- `correlation_anchors[]` must never contain redactable values. ARNs
  (public identifiers) and module paths are safe. Attribute values are
  NOT anchors.
- Drift diffs (§6.4 output) must run through the shared redaction lib
  before emit; a drift diff of a `password` attribute must not leak the
  value.

---

## 14. Open Questions

| Question | Proposal |
|---|---|
| Terraform registry → Git repo resolution | Phase 5+; launch accepts `(:TerraformModule)` terminus with warning |
| How to handle multi-module states where the same ARN appears in two modules | DSL emits `(:DriftObservation {kind=duplicate_ownership})`; operator reviews |
| What drives confidence weights per source | Hardcoded starter weights (git=high, tfstate=high, aws=medium for tag-only correlation); learning loop deferred to phase 6 |
| Sharding DSL evaluator for scale | Single evaluator per scope_id suffices at launch (parallelism = coordinator scope fan-out) |
| Webhook sources | Placeholder in all matrices; actual collector lands later ADR |

---

## 15. References

- 2026-04-19 correlation DSL and collector readiness ADR (DSL layer design)
- 2026-04-20 workflow coordinator runtime contract ADR (registry extension landing pad)
- 2026-04-20 workflow coordinator claiming/fencing/convergence ADR
- 2026-04-20 terraform state collector ADR (amendment target)
- 2026-04-20 aws cloud scanner collector ADR (amendment target)
- Architecture workflow plans for tfstate and aws (gate checklists)

---

## 16. Acceptance Criteria

This ADR is accepted when:

- Every decision (§1–§5) has a one-line reviewer sign-off in the PR
- Rollout plan (§9) phase 1 items are merged
- Invariants (§11) have test stubs identified in Phase 2/3 plans
- Existing coordinator ADR is amended to reference this ADR
- Gate issues #103 and #104 are amended to include
  "consumer contract sign-off" as a prerequisite
