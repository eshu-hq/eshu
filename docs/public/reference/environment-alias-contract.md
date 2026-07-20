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
| namespace_label | Defined for #5434 |

The defined-for-later classes exist in the vocabulary so callers can reference
them by constant; their producers land in follow-on work.

## Environment-unbound state

`StateEnvironmentUnbound` (`"environment-unbound"`) is the contract-level
vocabulary for a workload with no resolved environment. This PR defines the
constant; existing `'unknown'` buckets, `missing_environment` tallies, and
compare-handler messages are **not** rewired to this value and remain
wire-compatible. The constant's runtime wiring lands with the consumers that
emit the new evidence classes: #5434 (namespace labels) and #5444 (ArgoCD
destination), which surface unbindable evidence as `environment-unbound`
instead of dropping it silently.

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
`workload-instance:api:prod`). The workload materializer is MERGE-based and has
no stale-instance retraction, so a pre-change instance with an alias-cased ID
becomes an orphan on first regeneration. This is a pre-existing condition of
the materializer, not a new hazard class: the orphan carries no edges the new
projection would write differently, and no cleanup mechanism existed before
this contract either. Deployments that never used alias or mixed-case overlay
directories are unaffected.
