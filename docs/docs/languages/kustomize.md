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

## Known Limitations
- `components` are folded into normalized `resource_refs`, but they are not yet broken out as a separate standalone field; `configurations` sections are still not extracted
- Inline patch bodies within `kustomization.yaml` are not traversed for field-level details
- Go surfaces patch targets, the patch-link heuristic, and typed deploy-source refs on the normal query path; the remaining limitations here are bounded non-goals for the documented surface.
