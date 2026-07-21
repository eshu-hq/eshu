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
| `supply_chain_impact` | `deployment_context.deployment_truth_tier` | `POST /api/v0/supply-chain/impact/findings` (deployment-context payload) |
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
- **Supply-chain impact** (`deployment_context.deployment_truth_tier`,
  `go/internal/query/supply_chain_impact_result.go`): live runtime evidence
  (`runtime_confirmed`) and CI provenance (`provenance_ci_declared`) are not
  yet differentiated in the reducer's finding payload, so every
  deployment-anchored finding classifies as `config_only` today. Tracked in
  [#5472](https://github.com/eshu-hq/eshu/issues/5472) (CI/CD correlation
  graph projection) and [#5474](https://github.com/eshu-hq/eshu/issues/5474)
  (gate extensions).

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

## Cross-references

- [Truth label protocol](truth-label-protocol.md) — the truth-envelope
  negotiation shared by all API/MCP read surfaces.
- [HTTP API reference](http-api.md) — field documentation for
  `trace_deployment_chain`, `supply_chain_impact`, and service story
  endpoints.
- `go/internal/truth/deployment_tiers.go` — the canonical typed constants
  and `ClassifyDeploymentTruthTier` helper.
