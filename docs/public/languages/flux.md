# Flux Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `flux`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/flux_comprehensive/`
- Unit test suite: `go/internal/parser/yaml/flux_test.go`, `go/internal/parser/yaml/flux_source_test.go`, `go/internal/parser/engine_yaml_flux_semantics_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Kustomization (`kustomize.toolkit.fluxcd.io`) | `flux-kustomizations` | supported | `flux_kustomizations` | `name, line_number` | `node:FluxKustomization` | `go/internal/parser/yaml/flux_test.go::TestIsFluxKustomization` | Compose-backed fixture verification | Distinct from a generic `kustomization.yaml` build manifest (issue #5342). |
| Kustomization sourceRef | `flux-kustomization-source-ref` | supported | `flux_kustomizations` | `name, line_number, source_ref_kind, source_ref_name, source_ref_namespace` | `property:FluxKustomization.source_ref_kind/name/namespace` | `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationCapturesSourceRefAndOmitsAbsentFields` | Compose-backed fixture verification | Namespace is omitted, never defaulted, when absent -- Flux's own sourceRef-namespace default (falling back to the Kustomization's namespace) is a reducer-side rule, not a parser fabrication. |
| Kustomization source path | `flux-kustomization-source-path` | supported | `flux_kustomizations` | `name, line_number, source_path` | `property:FluxKustomization.source_path` | `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationCapturesSourceRefAndOmitsAbsentFields` | Compose-backed fixture verification | `spec.path` is captured under the `source_path` property key (not `spec_path`) so it lines up with the key deployment-trace helpers already read for other GitOps controllers. |
| Kustomization target namespace | `flux-kustomization-target-namespace` | supported | `flux_kustomizations` | `name, line_number, target_namespace` | `property:FluxKustomization.target_namespace` | `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationCapturesSourceRefAndOmitsAbsentFields` | Compose-backed fixture verification | - |
| GitRepository (`source.toolkit.fluxcd.io`) | `flux-git-repositories` | supported | `flux_git_repositories` | `name, line_number` | `node:FluxGitRepository` | `go/internal/parser/yaml/flux_source_test.go::TestIsFluxGitRepository` | Compose-backed fixture verification | Previously fell through to the generic `k8s_resources` bucket, which drops `spec.url`/`spec.ref`. |
| GitRepository url | `flux-git-repository-url` | supported | `flux_git_repositories` | `name, line_number, url` | `property:FluxGitRepository.url` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxGitRepositoryCapturesURLAndRef` | Compose-backed fixture verification | `spec.url` is captured as the immutable clone coordinate for the source. |
| GitRepository ref | `flux-git-repository-ref` | supported | `flux_git_repositories` | `name, line_number, ref_branch, ref_tag, ref_semver, ref_commit` | `property:FluxGitRepository.ref_branch/ref_tag/ref_semver/ref_commit` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxGitRepositoryCapturesURLAndRef` | Compose-backed fixture verification | Flux resolves exactly one of branch/tag/semver/commit per revision; each is captured independently and only when present. |
| OCIRepository (`source.toolkit.fluxcd.io`) | `flux-oci-repositories` | supported | `flux_oci_repositories` | `name, line_number` | `node:FluxOCIRepository` | `go/internal/parser/yaml/flux_source_test.go::TestIsFluxOCIRepository` | Compose-backed fixture verification | - |
| OCIRepository url | `flux-oci-repository-url` | supported | `flux_oci_repositories` | `name, line_number, url` | `property:FluxOCIRepository.url` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxOCIRepositoryCapturesURLAndRef` | Compose-backed fixture verification | `spec.url` is the OCI artifact repository coordinate. |
| OCIRepository ref | `flux-oci-repository-ref` | supported | `flux_oci_repositories` | `name, line_number, ref_tag, ref_semver, ref_commit` | `property:FluxOCIRepository.ref_tag/ref_semver/ref_commit` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxOCIRepositoryCapturesURLAndRef` | Compose-backed fixture verification | `spec.ref.digest` folds into `ref_commit` alongside GitRepository's commit SHA -- both identify an immutable content-addressed revision. |
| Bucket (`source.toolkit.fluxcd.io`) | `flux-buckets` | supported | `flux_buckets` | `name, line_number` | `node:FluxBucket` | `go/internal/parser/yaml/flux_source_test.go::TestIsFluxBucket` | Compose-backed fixture verification | - |
| Bucket coordinates | `flux-bucket-coordinates` | supported | `flux_buckets` | `name, line_number, bucket_name, endpoint, provider` | `property:FluxBucket.bucket_name/endpoint/provider` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxBucketCapturesBucketFields` | Compose-backed fixture verification | - |
| generateName evidence (all Flux kinds) | `flux-generate-name` | supported | `flux_kustomizations` / `flux_git_repositories` / `flux_oci_repositories` / `flux_buckets` | `name, line_number, generate_name` | `property:Flux*.generate_name` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxGitRepositoryGenerateNameOnly`, `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationGenerateNameOnly` | Compose-backed fixture verification | A CR using `metadata.generateName` instead of `metadata.name` has an empty `name` (never `"<nil>"`) and carries the literal `generate_name` field so the empty name is explained (issue #5360 PR A). |
| Kustomization `RECONCILES_FROM` source edge | `flux-reconciles-from-edge` | supported | n/a (projector-canonical, not a parser bucket) | `resolution_mode, source_ref_kind, source_ref_name, source_ref_namespace, namespace_defaulted` | `edge:FluxKustomization-[RECONCILES_FROM]->FluxGitRepository/FluxOCIRepository/FluxBucket` | `go/internal/storage/cypher/canonical_flux_edges_test.go` | `go/internal/query/relationships_catalog_flux_test.go`, B-12 `rc-152` | Resolved in Go at projection time (not the parser): same-repo, deterministic T1-T4 tiers applying Flux's own same-namespace default; a dangling ref, a declared-namespace mismatch, or an unresolved ambiguity (ties, ambiguous ownerless candidates) is an honest non-link, never a fabricated join (issue #5360 PR B). |

## Framework And Library Support

Supported today:

- Flux is deployment-reconciliation evidence, not application-framework
  reachability.
- Kustomization, GitRepository, OCIRepository, and Bucket are modeled as
  typed content entities reachable through `get_entity_context`.
- The `RECONCILES_FROM` correlation edge from a `FluxKustomization` to the
  `FluxGitRepository`/`FluxOCIRepository`/`FluxBucket` its `sourceRef` names
  is materialized and browsable through `list_relationship_edges` (issue
  #5360 PR B) -- see the capability table above and Known Limitations below
  for what it does and does not resolve.

Not claimed today:

- `HelmRelease` (`helm.toolkit.fluxcd.io`) and `HelmRepository`
  (`source.toolkit.fluxcd.io`) are not modeled; they continue to fall
  through to the generic `k8s_resources` bucket.
- Flux notification/alerting CRDs (`notification.toolkit.fluxcd.io`) and
  image-automation CRDs (`image.toolkit.fluxcd.io`) are not modeled.

## Known Limitations

- **The correlation edge resolves same-repo only.** `FluxKustomization
  -[:RECONCILES_FROM]-> FluxGitRepository`/`FluxOCIRepository`/`FluxBucket`
  (issue #5360 PR B) is resolved in Go at projection time
  (`go/internal/storage/cypher/canonical_flux_edges.go`), never in the
  parser: it applies Flux's own same-namespace default (an empty
  `sourceRef.namespace` falls back to the Kustomization's own namespace,
  recorded on the edge as `namespace_defaulted`), then four deterministic
  tiers -- namespace-exact; a multi-cluster same-name/same-namespace
  duplicate disambiguated by same-file-then-nearest-directory (an
  unresolved tie skips, never a representative pick); a unique candidate
  whose own namespace is absent when the declared namespace has no match;
  and a namespace-fully-unknown unique name repo-wide. A dangling ref, a
  declared-namespace mismatch, or an unresolved ambiguity produces no edge,
  ever -- this is a same-repo-only join (two tenants can share
  `flux-system`/`flux-system`; Eshu has no cross-repo cluster identity), so
  a `sourceRef` naming a CR that lives in a different repository is also an
  honest non-link, not a cross-repo lineage limitation (see below).
- **No deployment-trace reachability yet.** `trace_deployment_chain` and the
  deployment-lineage provenance-family derivation for Flux are not covered
  by this change; the correlation edge is browsable only through
  `list_relationship_edges` and `get_entity_context` today.
- **HelmRelease sourceRef is a follow-up.** `HelmRelease.spec.chart.spec.sourceRef`
  (or `chartRef`) can reference a `HelmRepository`, `GitRepository`, or
  `OCIRepository`; `HelmRepository` is a fourth, currently uncaptured source
  kind. Tracked as a follow-up to issue #5360.
- **Cross-repo lineage is a follow-up.** `FluxGitRepository.spec.url` resolving
  to a specific `Repository` identity in the trace path is not modeled.
- Namespace defaulting (Flux's own rule: an empty `sourceRef.namespace`
  defaults to the Kustomization's own namespace) is not applied by the
  parser -- an absent `source_ref_namespace` property is recorded as absent,
  never fabricated. The default is applied at correlation time by the
  `RECONCILES_FROM` edge builder instead (issue #5360 PR B, see above).
- **`metadata.generateName` yields an empty `name`.** A Flux CR that uses
  `metadata.generateName` (server-assigned name) instead of `metadata.name`
  has an empty `name` property -- never a fabricated `"<nil>"` -- and carries
  the literal `generate_name` evidence field so the empty name is explained.
  Node identity is `(repo_id, path, label, name, start_line)`, which stays
  unique for a nameless CR because multi-document YAML forces a distinct `---`
  document start line per entity, so two same-label nameless entities in one
  file cannot collide.
