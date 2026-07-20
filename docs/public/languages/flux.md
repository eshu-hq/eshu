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

## Framework And Library Support

Supported today:

- Flux is deployment-reconciliation evidence, not application-framework
  reachability.
- Kustomization, GitRepository, OCIRepository, and Bucket are modeled as
  typed content entities reachable through `get_entity_context`.

Not claimed today:

- The `RECONCILES_FROM` correlation edge from a `FluxKustomization` to the
  `FluxGitRepository`/`FluxOCIRepository`/`FluxBucket` its `sourceRef`
  names is not yet materialized -- see Known Limitations below.
- `HelmRelease` (`helm.toolkit.fluxcd.io`) and `HelmRepository`
  (`source.toolkit.fluxcd.io`) are not modeled; they continue to fall
  through to the generic `k8s_resources` bucket.
- Flux notification/alerting CRDs (`notification.toolkit.fluxcd.io`) and
  image-automation CRDs (`image.toolkit.fluxcd.io`) are not modeled.

## Known Limitations

- **No correlation edge yet.** This change (issue #5360, PR A) promotes the
  Flux Kustomization and its source CRs to typed graph nodes and makes them
  reachable through `get_entity_context`, but does not materialize a
  `FluxKustomization -[:RECONCILES_FROM]-> FluxGitRepository` (or
  `FluxOCIRepository`/`FluxBucket`) edge. A `FluxKustomization`'s
  `source_ref_kind`/`source_ref_name`/`source_ref_namespace` properties and a
  source CR's own `name`/`namespace` are both present in the graph today, but
  joining them into a traversable edge is separate follow-on work.
- **No deployment-trace reachability yet.** Because the correlation edge is
  not materialized, `trace_deployment_chain` and the deployment-lineage
  provenance-family derivation for Flux are not covered by this change.
- **HelmRelease sourceRef is a follow-up.** `HelmRelease.spec.chart.spec.sourceRef`
  (or `chartRef`) can reference a `HelmRepository`, `GitRepository`, or
  `OCIRepository`; `HelmRepository` is a fourth, currently uncaptured source
  kind. Tracked as a follow-up to issue #5360.
- **Cross-repo lineage is a follow-up.** `FluxGitRepository.spec.url` resolving
  to a specific `Repository` identity in the trace path is not modeled.
- Namespace defaulting (Flux's own rule: an empty `sourceRef.namespace`
  defaults to the Kustomization's own namespace) is not applied by the
  parser -- an absent `source_ref_namespace` is recorded as absent, never
  fabricated. Applying the default is reducer-owned follow-on work alongside
  the correlation edge.
