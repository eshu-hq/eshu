# Deployment Truth Tiers

The deployment truth tier is a closed, typed vocabulary that classifies the
strongest class of deployment evidence available for a traced workload. It
replaces ad-hoc confidence-reason strings with a shared vocabulary across all
query surfaces.

## Tiers

| Tier | Wire String | Rank | Evidence Class |
|------|------------|------|---------------|
| Runtime Confirmed | `runtime_confirmed` | 1 (strongest) | Live observation confirms the workload runs (e.g. exact kubernetes_live correlation producing a `RUNS_IMAGE` edge, or a cloud-observed instance). |
| Provenance CI/CD Declared | `provenance_ci_declared` | 2 | CI/CD or supply-chain provenance declares a deployment (e.g. ci_cd run correlation, attestation). |
| Declared Ref | `declared_ref` | 3 | A named ref (branch/SHA) is declared deployed through a `DEPLOYS_REF` edge (#5393). *Constant defined; evidence source not yet wired.* |
| Config Only | `config_only` | 4 (weakest) | Only config materialization evidence (config-derived `WorkloadInstance`, deployment sources, or config environments) exists. |

The tiers are strictly ordered. A workload classified as `runtime_confirmed`
has stronger evidence than one classified as `provenance_ci_declared`, and so
on. The constants live in `go/internal/truth/deployment_tiers.go` and every
consumer reads through the same `ClassifyDeploymentTruthTier` helper.

## What qualifies (and what does not)

### `runtime_confirmed`

QUALIFIES:
- An exact kubernetes_live correlation producing a `RUNS_IMAGE` edge from a
  `KubernetesWorkload` to an `OciImageManifest`, reachable through the
  workload's `INSTANCE_OF → RUNS_ON → OBSERVED_ON` topology.
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
`go/internal/truth`, so the tier vocabulary is applied consistently.

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
