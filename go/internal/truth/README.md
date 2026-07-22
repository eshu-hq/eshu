# Truth

## Purpose

`truth` owns the canonical truth contracts shared across Eshu: the layered
materialization contract and the unified evidence record. It defines the four
bounded source layers, a typed `Layer` enum with parse and validate helpers,
the `Contract` value that binds one canonical kind to the set of source layers
a reducer accepts as evidence, and the canonical `Evidence` value.

Every reducer registration, proof-domain assertion, and query-side
`truth.layer` / `truth.backend` response field reaches for these symbols
rather than redefining them locally.

### Canonical evidence (issue #3489)

`Evidence` is the single evidence record that carries BOTH a bounded `[0,1]`
`Confidence` AND a byte-level `Citation`, plus typed `Provenance`. It unifies
three former shapes that each carried only part of that contract:

| Former shape | Had | Lacked |
| --- | --- | --- |
| `relationships.EvidenceFact` | confidence, free-form `Details` | byte citation |
| `query.evidenceCitation` | path/line/hash/commit | confidence, provenance |
| documentation evidence packets | versioned finding model | unified citation+confidence |

`Citation` locates a file (`RepoID` + `RelativePath`) or an entity
(`EntityID`), then refines it with a line range, a byte offset/length window,
and the `ContentHash` / `CommitSHA` that pin the cited bytes. `Provenance`
records the `Basis` (source content, graph projection, assertion, derived),
`Rationale`, `Actor`, and `Source`.

### Deployment truth tiers (#5471)

`DeploymentTruthTier` is a closed, strictly-ranked vocabulary classifying the
strongest class of deployment evidence available for a traced workload:

| Tier | Rank | Evidence class |
| --- | --- | --- |
| `runtime_confirmed` | 4 (strongest) | Live observation confirms the workload runs (exact `kubernetes_live` correlation, cloud-observed instance). |
| `provenance_ci_declared` | 3 | CI/CD or supply-chain provenance declares a deployment. |
| `declared_ref` | 2 | A named ref declared deployed via a future `DEPLOYS_REF` edge (constant defined; evidence source not yet wired, #5393). |
| `config_only` | 1 (weakest) | Only config-materialization evidence (config-derived `WorkloadInstance`, deployment sources, config environments) exists. |

`ClassifyDeploymentTruthTier(hasLiveEvidence, hasInstances,
hasDeploymentSources, hasConfigEnvironments)` is the single shared classifier:
`trace_deployment_chain`, `supply_chain_impact`, and the service story all
call through it, so the tier vocabulary reads the same way on every surface
that emits it. Full tier semantics — including what qualifies and what does
not per tier — live in
[`docs/public/reference/deployment-truth-tiers.md`](../../../docs/public/reference/deployment-truth-tiers.md).

## Where this fits

```mermaid
flowchart LR
  A["reducer/registry.go\nContract declaration"] --> B["truth.Contract\ntruth.Layer"]
  B --> C["reducer.Domain\nadmission gate"]
  B --> D["query/status.go\ntruth.layer in response"]
```

## Ownership boundary

`truth` owns the `Layer` enum and `Contract` struct. It does not own
reducer dispatch, proof-domain storage, or query response serialization.
The package has no internal-package imports and no runtime state.

## Exported surface

- `Layer` — string-typed enum for a bounded truth layer.
  Constants: `LayerSourceDeclaration`, `LayerAppliedDeclaration`,
  `LayerObservedResource`, `LayerCanonicalAsset`.
  Methods: `Layer.Validate`.
- `ParseLayer(raw string) (Layer, error)` — trims whitespace and validates
  one layer string against the known set.
- `Contract` — binds `CanonicalKind` (string) to `SourceLayers` ([]`Layer`).
  Methods: `Contract.Validate`, `Contract.Supports(layer Layer) bool`.
- `Evidence` — unified evidence record (`Kind`, `Confidence`, `Citation`,
  `Provenance`). Method: `Evidence.Validate`.
- `Citation` — byte-level source pointer. Method: `Citation.Validate`.
- `Provenance` — typed origin record. Method: `Provenance.Validate`.
- `ProvenanceBasis` — enum: `ProvenanceBasisSourceContent`,
  `ProvenanceBasisGraphProjection`, `ProvenanceBasisAssertion`,
  `ProvenanceBasisDerived`. Method: `ProvenanceBasis.Validate`.
- `DeploymentTruthTier` — string-typed enum for the deployment evidence
  vocabulary. Constants: `TierRuntimeConfirmed`, `TierProvenanceCIDeclared`,
  `TierDeclaredRef`, `TierConfigOnly`. Methods: `DeploymentTruthTier.Rank`,
  `DeploymentTruthTier.Compare`.
- `ParseDeploymentTruthTier(raw string) (DeploymentTruthTier, error)` —
  trims whitespace and validates one tier string against the known set.
- `AllDeploymentTruthTiers() []DeploymentTruthTier` — every known tier in
  strict descending rank order.
- `ClassifyDeploymentTruthTier(hasLiveEvidence, hasInstances,
  hasDeploymentSources, hasConfigEnvironments bool) DeploymentTruthTier` —
  the shared classifier every tier-emitting query surface calls through.

See `doc.go` for the godoc contract.

## Dependencies

Standard library only (`fmt`, `strings`). No internal packages.

## Telemetry

None. This is a pure value-type package with no runtime I/O.

## Gotchas / invariants

- `LayerCanonicalAsset` is reducer output, not a source input.
  `Contract.Validate` (`model.go:60`) rejects it in `SourceLayers`. Registering
  a contract that cites `LayerCanonicalAsset` as a source layer will fail at
  domain registration time.
- `SourceLayers` must be non-empty and free of duplicates. `Contract.Validate`
  (`model.go:53`) enforces both checks before returning nil.
- `Contract.Supports` (`model.go:74`) is a linear scan over the slice. The
  slice is intentionally short; callers should not cache results.
- Adding a new layer requires updating the `Validate` switch in `model.go`,
  `ParseLayer`, and any downstream materialization that switches on `Layer`
  values.
- `Evidence.Validate` (`evidence.go`) bounds `Confidence` to `[0,1]` and
  rejects `NaN`; it does not fetch content, so a citation that passes
  `Citation.Validate` may still point at drifted or missing bytes. Consumers
  that hydrate content must re-check `ContentHash` / `CommitSHA`.

## Related docs

- `docs/public/architecture.md` — ownership table and pipeline overview
- `docs/public/reference/http-api.md` — `truth.layer` and `truth.backend`
  response fields
- `docs/public/reference/deployment-truth-tiers.md` — full deployment truth
  tier semantics, what qualifies per tier, and consumer surfaces
- `go/internal/reducer/README.md` — reducer domain registration and
  `Contract` usage
