# Environment Alias Contract

The canonical environment naming contract for the Eshu platform. Before this
contract, environment tokens, aliases, and normalization rules were duplicated
across three locations with inconsistent case handling. This contract
centralizes them in `go/internal/environment`.

## Single normalization rule

`Normalize(raw)`: trim whitespace, lowercase. This is the only normalization
rule on the platform. No other normalization, no locale folding, no
domain-specific exceptions.

## Alias table

The shared alias table maps non-canonical forms to canonical names:

| Alias | Canonical |
| --- | --- |
| production | prod |
| staging | stage |
| development | dev |

All other canonical names (qa, test, sandbox, preview, uat, preprod) have no
aliases and pass through as-is.

`Canonical(raw)` applies normalization then the alias table. Unknown values
pass through normalized — they are **never** rejected and **never** invented.

## Known-token set

`IsKnownToken(token)` returns true for exactly these 12 tokens:

prod, production, qa, stage, staging, uat, preprod, dev, development, test,
sandbox, preview.

This is the union used for artifact-path token detection. The token set is
case-sensitive; callers must normalize before calling.

## Evidence classes

`EvidenceClass` is a closed vocabulary of environment evidence provenance
classes:

| Class | Status |
| --- | --- |
| path_overlay | Emitted today |
| namespace_fallback | Emitted today |
| artifact_path_token | Emitted today |
| ci_observation | Emitted today |
| cloud_tag | Emitted today |
| operator_declared | Emitted today |
| hostname_inference | Emitted today |
| explicit_alias_config | Defined for later wiring |
| argocd_destination | Defined for #5444 |
| namespace_label | Landed for #5434 |

The defined-for-later classes exist in the vocabulary so callers can reference
them by constant; their producers land in follow-on work.

## Environment-unbound state

`StateEnvironmentUnbound` (`"environment-unbound"`) is the contract-level
vocabulary for a workload with no resolved environment. The constant's
runtime wiring is landing incrementally with the consumers that emit the new
evidence classes, which surface unbindable evidence as `environment-unbound`
instead of dropping it silently: #5434 (namespace labels) has landed --
`go/internal/reducer/kubernetes_namespace_materialization.go`'s
`KubernetesNamespaceMaterializationHandler` classifies every
`kubernetes_live.namespace` fact whose labels carry no alias-recognized
environment value as `StateEnvironmentUnbound` and writes NO `Environment`
node for it. #5444 (ArgoCD destination) is not yet wired. Existing `'unknown'`
buckets, `missing_environment` tallies, and compare-handler messages are
**not** rewired to this value and remain wire-compatible.

## Comparison semantics

| Path | Semantics |
| --- | --- |
| Graph joins (USES edge, exact match) | Case-sensitive string equality on the Cypher property. See documented follow-on below. |
| Canonical alias compare | `environment.Canonical()` — trim+lowercase+alias. Used by `compare_evidence.go`, `service_contract_helpers.go`. |
| EqualFold compare | Case-insensitive string equality, **not alias-aware** (`"production"` ≠ `"prod"`). Used by `deployment_config_influence.go:250` for row filtering. Follow-on, not migrated in this PR. |
| Exact selector match | Case-sensitive equality (`i.environment = $environment`). Used by `service_workload_resolution.go:292-293` Cypher filters. A caller passing an alias (`"production"`) does not match a canonicalized graph value (`"prod"`). Follow-on, not migrated in this PR. |
| Artifact-path token detection | Normalized (lowercase) token lookup via `environment.IsKnownToken`. |

The USES edge exact-join in `go/internal/storage/cypher/workload_cloud_relationship_writer.go:22,25`
is a documented follow-on that must be migrated to canonical comparison so a
workload instance environment that was canonicalized at projection time matches
the same-cased Cypher property on the graph node.

## No-invented-environments rule

An unknown value passes through normalized. It is **never** mapped to a
canonical name without explicit evidence (config, collector fact, or
operator-declared annotation). Namespace, folder, or repo-name heuristics must
not invent environment truth.

## Consumer migration table

| Consumer | Before | After | Classification |
| --- | --- | --- | --- |
| `canonicalEnvironmentName` (query/compare_evidence.go) | Inline alias loop | `environment.Canonical()` | Output-preserving |
| `environmentAliases` (query/service_hostname_evidence.go) | Package-level var | `environment.Aliases()` | Output-preserving |
| `canonicalEnvironmentAlias` (query/service_contract_helpers.go) | Calls `detectEnvironmentAliases` | Same logic, shared data | Output-preserving |
| `isKnownEnvironmentToken` (reducer/cross_repo_evidence_artifacts.go) | Inline switch | `environment.IsKnownToken()` | Output-preserving |
| `isDeploymentEnvironmentToken` (query/repository_deployment_evidence_read_model.go) | Inline switch | `environment.IsKnownToken()` | Output-preserving |
| `namespaceEnvironmentFallback` (reducer/projection_helpers.go) | Original-case return | `environment.Canonical()` return | Expected-delta: canonical case |
| `ExtractOverlayEnvironments` (reducer/projection.go) | Raw capture | `environment.Canonical(captured)` | Expected-delta: alias→canonical |
| CI env decision (reducer/ci_cd_run_correlation.go) | `trimmedCICDPtr` only | `environment.Canonical(trimmedCICDPtr(...))` | Expected-delta: alias→canonical |
| USES edge exact-join (storage/cypher) | Exact string match | Documented follow-on | Not in this PR |
| `deployment_config_influence.go:250` (query) | `strings.EqualFold` | Documented follow-on (alias-aware compare) | Not in this PR |
| `service_workload_resolution.go:292-293` (query) | Exact Cypher match | Documented follow-on (canonical compare) | Not in this PR |

## One-time identity shift note

Canonicalizing `ExtractOverlayEnvironments` changes `WorkloadInstance` identity
keys for alias or mixed-case inputs (`workload-instance:api:production` becomes
`workload-instance:api:prod`). The workload materializer is MERGE-based, so a
pre-change instance with an alias-cased ID would otherwise survive forever
alongside the new canonical key — duplicate deployment and runtime truth. A
retraction pass closes this gap (`ReconcileWorkloadInstanceRetraction` /
`WorkloadMaterializer.RetractInstances` in `go/internal/reducer`):

- **Scope.** A superseded instance is only eligible for retraction if it is
  owned by a repository materialized in the current pass (by `repo_id`) and
  tagged with the workload-materialization evidence source
  (`evidence_source`). Retraction can never cross a repository or evidence
  source it does not own.
- **Positive-evidence guard.** A repository must have written at least one
  `WorkloadInstance` row in the *current* pass before any of its existing
  instances are considered superseded. A repository that produced zero
  instance rows this pass — for example a transient gap in one of the seven
  environment-alias evidence classes — supplies no signal that its
  environments actually changed, so none of its prior instances are touched.
- **Delete-time predicate.** The retract statement re-validates
  `i.repo_id IN $repo_ids AND i.evidence_source = $evidence_source` at DETACH
  DELETE time, not just when the retraction decision is computed. Instance ids
  are not repository-namespaced and separate-scope materialization passes can
  drain concurrently, so a decision computed from a stale snapshot could
  otherwise delete a node a different scope has since re-owned; the delete-time
  predicate makes that impossible — a node whose current owner no longer
  matches survives.
- **Ordering.** Retraction runs after the replacement generation's MERGE write
  is confirmed, so a failed write can never retract an instance before its
  canonical replacement is durable.
- **Blast radius.** DETACH DELETE removes every relationship incident to the
  retracted node — INSTANCE_OF, DEPLOYMENT_SOURCE, and RUNS_ON, plus any USES
  edge as collateral (USES has its own independent scope-retraction pass
  elsewhere; that pass is not made redundant by this one).

Deployments that never used alias or mixed-case overlay directories are
unaffected: they produce no pre-canonical instance keys to retract.
