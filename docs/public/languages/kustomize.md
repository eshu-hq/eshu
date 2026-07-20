# Kustomize Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `kustomize`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/kustomize_comprehensive/`
- Unit test suite: `go/internal/parser/engine_yaml_semantics_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Kustomization overlays (`kustomization.yaml`) | `kustomization-overlays-kustomization-yaml` | supported | `kustomize_overlays` | `name, line_number` | `node:KustomizeOverlay` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Namespace | `namespace` | supported | `variables` | `name, line_number, namespace` | `property:Overlay.property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Resources list | `resources-list` | supported | `kustomize_overlays` | `name, line_number, resources` | `property:Overlay.resources` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Patches list | `patches-list` | supported | `kustomize_overlays` | `name, line_number, patches` | `property:Overlay.patches` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Patch targets (`patches[].target.kind/name`) | `patch-targets` | supported | `kustomize_overlays` | `name, line_number, patch_targets` | `property:KustomizeOverlay.patch_targets` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizePatchTargets` | Compose-backed fixture verification | Inline Kustomize patch targets are normalized into stable `Kind/name` strings and now surface through Go query summaries. |
| Patch-link heuristic | `patch-link-heuristic` | supported | content-backed relationships | `patch_targets` | `relationship:PATCHES` | `go/internal/query/content_relationships_kustomize_test.go::TestBuildContentRelationshipSetKustomizeOverlayPatchesTargetResource` | `go/internal/query/entity_content_iac_fallback_test.go::TestGetEntityContextFallsBackToKustomizeOverlayContentEntity` | Preserves the overlay-to-target patch link on the current query path. |
| Base references | `base-references` | supported | `kustomize_overlays` | `name, line_number, bases` | `property:KustomizeOverlay.bases` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | `bases` is normalized into a stable, sorted list of base paths on the Kustomize payload, so the relation stays first-class instead of being flattened into a comma-delimited string. |
| Typed deploy-source refs | `typed-deploy-source-refs` | supported | `kustomize_overlays` | `resource_refs, helm_refs, image_refs` | `property:KustomizeOverlay.resource_refs`, `property:KustomizeOverlay.helm_refs`, `property:KustomizeOverlay.image_refs` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeTypedDeployReferences` | Compose-backed fixture verification | Go now materializes non-base `resources`/`components`, `helmCharts`, and `images` into stable typed ref lists for downstream query and evidence promotion. |
| Typed deploy-source query fallback | `typed-deploy-source-query-fallback` | supported | content-backed relationships | `resource_refs, helm_refs, image_refs` | `relationship:DEPLOYS_FROM` | `go/internal/query/content_relationships_kustomize_deploy_test.go::TestBuildContentRelationshipSetKustomizeOverlayPromotesTypedDeploySources` | `go/internal/query/entity_content_kustomize_deploy_fallback_test.go::TestGetEntityContextFallsBackToKustomizeOverlayTypedDeploySources` | The Go entity-context fallback now surfaces typed Kustomize deploy-source signals for resources, Helm charts, and images without Python ownership. |
| Flux CD Kustomization sourceRef/path/targetNamespace | `flux-kustomization-source-ref-evidence` | supported | `flux_kustomizations` | `name, line_number, source_ref_kind, source_ref_name, source_path` | `node:FluxKustomization` (see [Flux](flux.md)) | `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationCapturesSourceRefAndOmitsAbsentFields`, `go/internal/parser/engine_yaml_flux_semantics_test.go::TestDefaultEngineParsePathYAMLFluxKustomizationDoesNotMisrouteToOverlay` | Compose-backed fixture verification | A Flux Kustomization CR (`apiVersion: kustomize.toolkit.fluxcd.io/*`) is a distinct object from a generic `kustomization.yaml` build manifest and is no longer misrouted into this bucket (issue #5342). It is now a typed `FluxKustomization` graph node (issue #5360 PR A); see [Flux](flux.md) for the full Flux capability set. |

## Framework And Library Support

Supported today:

- Kustomize is deployment configuration evidence, not application-framework
  reachability.
- Overlays, resources, bases, patches, patch targets, typed deploy-source refs,
  and query fallback relationships are modeled.

Not claimed today:

- Kustomize build output, component expansion beyond normalized refs, field-level
  inline patch semantics, and application runtime behavior are not modeled.

## Known Limitations
- `components` are folded into normalized `resource_refs`; they are not a
  separate standalone field.
- `configurations` sections are not extracted.
- Inline patch bodies within `kustomization.yaml` are not traversed for
  field-level details.
- Patch targets, the patch-link heuristic, and typed deploy-source refs are
  supported on the normal query path. The limitations above are bounded
  non-goals for this documented surface.

## Flux CD Kustomization

A Flux CD `Kustomization` custom resource (`apiVersion:
kustomize.toolkit.fluxcd.io/*`, `kind: Kustomization`) is a cluster
reconciliation object, not a `kustomization.yaml` build manifest -- its
fields nest under `spec` (`sourceRef`, `path`, `targetNamespace`) instead of
sitting at the document root. Before issue #5342 it was misrouted into the
generic Kustomize matcher (`isKustomization`'s bare `"kustomize"` apiVersion
prefix), which reads only top-level keys and produced a near-empty overlay
while silently dropping `spec.sourceRef`, `spec.path`, and
`spec.targetNamespace`.

`isKustomization` now matches only the generic `kustomize.config.k8s.io/*`
group (or a bare `kustomization.yaml`/`.yml` filename with no apiVersion at
all -- an explicit foreign apiVersion vetoes that filename-only match). A
Flux Kustomization is matched separately by `isFluxKustomization` and parsed
by `parseFluxKustomization` (`go/internal/parser/yaml/flux.go`), which
captures `spec.sourceRef.kind`, `spec.sourceRef.name`,
`spec.sourceRef.namespace`, `spec.path` (under the `source_path` row key),
and `spec.targetNamespace` defensively -- an absent field is omitted, never
fabricated -- into a dedicated `flux_kustomizations` payload bucket.

This bucket is now registered as the typed `FluxKustomization` content
entity and reachable through `get_entity_context` (issue #5360 PR A). See
[Flux](flux.md) for the full Flux capability set, including the source CRs
(`GitRepository`, `OCIRepository`, `Bucket`) it reconciles against. The
`RECONCILES_FROM` correlation edge from a `FluxKustomization` to its source
CR is not yet materialized -- that lands in a later change; see
[Flux](flux.md#known-limitations).
