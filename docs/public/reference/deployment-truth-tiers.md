# Deployment Truth Tiers

The deployment truth tier is a closed, typed vocabulary that classifies the
strongest class of deployment evidence available for a traced workload. It
replaces ad-hoc confidence-reason strings with a shared vocabulary across all
query surfaces.

## Tiers

| Tier | Wire String | Priority (`Rank()`, higher = stronger) | Evidence Class |
|------|------------|------|---------------|
| Runtime Confirmed | `runtime_confirmed` | 4 (strongest) | Live observation confirms the workload runs (e.g. exact kubernetes_live correlation producing a `RUNS_IMAGE` edge, or a cloud-observed instance). |
| Provenance CI/CD Declared | `provenance_ci_declared` | 3 | CI/CD or supply-chain provenance declares a deployment (e.g. ci_cd run correlation, attestation). |
| Declared Ref | `declared_ref` | 2 | A named ref (branch/SHA) is declared deployed through a `DEPLOYS_REF` edge (#5393). *Constant defined; evidence source not yet wired.* |
| Config Only | `config_only` | 1 (weakest) | Only config materialization evidence (config-derived `WorkloadInstance`, deployment sources, or config environments) exists. |

The tiers are strictly ordered. A workload classified as `runtime_confirmed`
has stronger evidence than one classified as `provenance_ci_declared`, and so
on. The Priority column is exactly the integer `DeploymentTruthTier.Rank()`
returns (higher wins; see `Compare`) -- the constants and `rank()` switch live
in `go/internal/truth/deployment_tiers.go`, and every consumer reads through
the same `ClassifyDeploymentTruthTier` helper.

## What qualifies (and what does not)

### `runtime_confirmed`

QUALIFIES:
- An exact-outcome kubernetes correlation row (`outcome: "exact"`) from the
  Postgres active-fact read model (`reducer_kubernetes_correlation`) whose
  `image_ref` matches a config-declared image reference from the workload's
  deployment-source controllers. The match means a live cluster observably
  runs the workload's declared image, and the reducer's image-identity
  evidence confirmed the digest or fixed-tag exact match. `RUNS_IMAGE` is
  the graph-side projection of the same exact outcomes, read through the
  same Postgres read model.
- A cloud-observed instance that confirms the workload runs in a measurable
  environment.
- For `supply_chain_impact` findings: an observed cloud compute resource (a
  running ECS task or an image-package Lambda function, captured as an
  `aws_resource` fact) whose `running_image_digest` equals the finding's
  subject digest. The exact scanned vulnerable image is running on that
  resource, so the finding is `runtime_confirmed` and names the resource ARN
  in `cloud_runtime_resource_refs`. This is a query-time `CloudResource` graph
  probe (`go/internal/query/supply_chain_impact_cloud_runtime_probe.go`), not a
  reducer-materialized field — the digest is the artifact's content-addressed
  identity, so the match is exact, not a shared-base-image coincidence (#5452).

DOES NOT QUALIFY:
- Config-materialized `WorkloadInstance` rows. Despite the legacy confidence
  reason string `materialized_runtime_instances`, these are config-derived,
  not live observations.
- Deployment-source (`DEPLOYMENT_SOURCE`/`DEPLOYS_FROM`) edges.
  These are config- or provenance-derived, not live observations.
- Config environments declared in configuration files.

### `provenance_ci_declared`

QUALIFIES:
- A CI/CD run correlation with an `exact` or `derived` outcome.
- A supply-chain attestation that declares a deployment.
- An OCI image identity fact linked to a deployment.

DOES NOT QUALIFY:
- Config-materialized instances or deployment sources.
- Live kubernetes observations (those are `runtime_confirmed`).

### `declared_ref`

QUALIFIES:
- A named ref (branch or SHA) declared as deployed through a `DEPLOYS_REF`
  edge. *Not yet wired; the constant exists for forward compatibility (#5393).*

DOES NOT QUALIFY:
- Live observations, CI/CD provenance, or config-only evidence.

### `config_only`

QUALIFIES:
- Config-materialized `WorkloadInstance` rows.
- `DEPLOYMENT_SOURCE` or `DEPLOYS_FROM` edges connecting the workload to a
  repository.
- Config environments declared in configuration files.

DOES NOT QUALIFY:
- Any live or CI/CD-declared evidence.

## Consumer surfaces

| Surface | Field | Route/Tool |
|---------|-------|-----------|
| `trace_deployment_chain` | `deployment_fact_summary.deployment_truth_tier` | `POST /api/v0/impact/trace-deployment-chain` |
| `supply_chain_impact` | `findings[].deployment_truth_tier` (plus `findings[].cloud_runtime_resource_refs` naming the observed running resource when `runtime_confirmed`) | `GET /api/v0/supply-chain/impact/findings` |
| Service story | `deployment_overview.deployment_truth_tier` | `GET /api/v0/services/{name}/story` |

All surfaces use the same `ClassifyDeploymentTruthTier` helper from
`go/internal/truth`, so the *vocabulary* is applied consistently — the same
four tier strings, the same rank order, the same closed set. The *inputs*
each surface feeds that helper are not yet uniform; see "Known surface gaps"
below for the surfaces where that still under-reports a workload's true tier.

## Known surface gaps

The same workload can report a stronger tier from one surface than another
today, because a surface has not yet been wired to feed
`ClassifyDeploymentTruthTier` every signal it is entitled to. These are
tracked disclosure gaps, not vocabulary inconsistencies — closing them is a
matter of wiring more evidence into the existing classifier, not changing
the tier semantics above.

- **Service story** (`deployment_overview.deployment_truth_tier`,
  `go/internal/query/service_story_overview.go`): `hasLiveEvidence` and
  `hasDeploymentSources` are hardcoded `false`, so a workload
  `trace_deployment_chain` reports as `runtime_confirmed` can report only
  `config_only` or no tier at all from the service story surface for the
  same workload. Tracked in [#5582](https://github.com/eshu-hq/eshu/issues/5582).
- **Supply-chain impact** (`findings[].deployment_truth_tier`,
  `go/internal/query/supplychain/impact/supply_chain_impact_result.go`): now differentiates all
  three evidence classes (#5452, closing the earlier gap tracked in #5472/#5474,
  both merged). A finding whose subject digest is observed running on a cloud
  resource classifies as `runtime_confirmed` (see the runtime_confirmed
  qualifier above); a finding with a matched `reducer_ci_cd_run_correlation`
  deployment hop classifies as `provenance_ci_declared`; a finding with only
  config-materialized deployment anchors or environments classifies as
  `config_only`. The runtime tier is sourced from a query-time `CloudResource`
  graph probe, so it degrades to the CI/config tiers (never a fabricated
  runtime tier) when no cloud evidence is wired. Remaining nuance: the probe
  runs on the findings-list read; other supply-chain read surfaces that do not
  build results through `buildSupplyChainImpactFindingResult` do not yet carry
  the runtime tier. Because `CloudResource` nodes carry no `scope_id` or
  freshness marker, the probe gates its digest matches through the Postgres
  owner ledger (active-generation + scope grants): an authorized scoped-token
  caller receives the runtime tier for cloud resources it is granted, while a
  stale (retracted-in-a-later-scan) node or a cross-scope resource never
  promotes a finding.

## Legacy reason → tier mapping

The legacy `overall_confidence_reason` strings inside
`trace_deployment_chain` map to the tier vocabulary as follows:

| Legacy Reason | Maps To Tier | Notes |
|--------------|-------------|-------|
| `live_runtime_observation` | `runtime_confirmed` | New reason string added in #5471 alongside the tier field. |
| `materialized_runtime_instances` | `config_only` | Legacy name is misleading: these are config-derived instances, not live runtime observations. |
| `canonical_deployment_sources` | `config_only` | Deployment-source edges are config evidence. |
| `config_only_evidence` | `config_only` | Direct match. |
| `no_deployment_evidence` | *(absent)* | No tier emitted when no evidence exists. |

## No-invented-tiers rule

The tier vocabulary is closed. The four tiers above are exhaustive for the
evidence classes Eshu recognizes. Do not invent new tier strings without
adding the constant to `go/internal/truth/deployment_tiers.go`, updating
`ClassifyDeploymentTruthTier`, and recording the change in this document.

## Confidence calibration

The `runtime_confirmed` tier is calibrated at **0.95** — higher than the
`materialized_runtime_instances` baseline (0.9) because live evidence is a
direct observation, not a config-derived inference. This calibration point
is recorded in the [confidence calibration reference](confidence-calibration.md).

## Version resolution reuse (#5469)

`supply_chain_impact` findings also disclose `version_resolution_tier` and
`version_resolution_corroboration[]` (`go/internal/query/supplychain/impact/supply_chain_impact_version_resolution.go`).
These fields reuse the exact same closed `DeploymentTruthTier` vocabulary
above — no new tier enum. They answer a narrower question than
`deployment_truth_tier`: not "what is the strongest evidence that this
workload was deployed at all", but "which tier's evidence backs the judged
`subject_digest`/`image_ref`/`observed_version` on this finding, and which
weaker tiers also made a version/digest claim (agreeing or not)".

A finding's `version_resolution_tier` can be weaker than its
`deployment_truth_tier` when the strongest tier's evidence exists but carries
no concrete artifact identity — for example a `provenance_ci_declared` hop
that matched only through repository+environment+operational anchor (the
weak match branch behind #5426) holds `deployment_truth_tier` at
`provenance_ci_declared` but drops `version_resolution_tier` to
`config_only` — or omits it entirely when the finding carries no
`observed_version`, `subject_digest`, or `image_ref` either — since that hop
makes no version/digest claim to disclose. `declared_ref` is fail-closed here
too: #5393 has no evidence producer, so it is never emitted by either field.

The mechanism (issue #5469 review): the reducer bakes the matched
`cicd_run_correlation` deployment's OWN declared identity onto the finding as
`ci_declared_artifact_digest`/`ci_declared_image_ref`
(`bakeSupplyChainCIDeclaredArtifactIdentity`,
`go/internal/reducer/supply_chain_impact_runtime.go`), but ONLY when that
deployment matched through a strong branch (its own `artifact_digest` equals
the finding's `subject_digest`, or its own `image_ref` equals the finding's
`image_ref`) — never the weak branch. `version_resolution_tier`'s
`provenance_ci_declared` claim reads exclusively from those baked fields, not
from the finding's own `subject_digest`/`image_ref`. A strong image-ref match
whose own declared digest contradicts the finding's subject digest (a tag
that moved to a different build) bakes that contradicting digest as real
evidence — but a contradicting `provenance_ci_declared` claim is **never
eligible to win** (review finding R1): crediting a foreign artifact's digest
as the judged `version_resolution_tier` would put it in direct conflict with
`config_only`/`runtime_confirmed`, which still report the finding's own
identity. Resolution falls through to the next eligible tier (typically
`config_only`'s own `subject_digest`), and the contradicting CI claim appears
in `version_resolution_corroboration` with `agreement: disagrees` — a genuine
same-axis digest-vs-digest disagreement, distinguished from the cross-axis
digest-vs-version mismatch a `config_only` `observed_version` corroboration
shows, which is reported `agreement: not_comparable` rather than a misleading
`disagrees` (review finding R6; `agreement` is a closed three-state
vocabulary — `agrees`, `disagrees`, `not_comparable` — not a plain boolean).

`version_resolution_tier` can also be **present** when `deployment_truth_tier`
is **absent**: `deployment_truth_tier`'s `config_only` branch requires a
deployment anchor (`workload_ids`, `deployment_ids`, `image_ref`, or
`subject_digest`) or a config environment, while `version_resolution_tier`'s
`config_only` claim only requires `observed_version` (or a fallback
digest/ref). A finding whose only evidence is a static
`observed_version` — for example an npm/PyPI dependency with no
container image, workload, or environment evidence at all — reports
`version_resolution_tier: config_only` while `deployment_truth_tier` is
omitted entirely.

## Cross-references

- [Truth label protocol](truth-label-protocol.md) — the truth-envelope
  negotiation shared by all API/MCP read surfaces.
- [HTTP API reference](http-api.md) — field documentation for
  `trace_deployment_chain`, `supply_chain_impact`, and service story
  endpoints.
- `go/internal/truth/deployment_tiers.go` — the canonical typed constants
  and `ClassifyDeploymentTruthTier` helper.
