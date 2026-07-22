# Flux Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `flux`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/flux_comprehensive/`
- Unit test suite: `go/internal/parser/yaml/flux_test.go`, `go/internal/parser/yaml/flux_source_test.go`, `go/internal/parser/yaml/flux_helm_test.go`, `go/internal/parser/engine_yaml_flux_semantics_test.go`, `go/internal/parser/engine_yaml_flux_helm_fixture_negatives_test.go`
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
| generateName evidence (all Flux kinds) | `flux-generate-name` | supported | `flux_kustomizations` / `flux_git_repositories` / `flux_oci_repositories` / `flux_buckets` / `flux_helm_releases` / `flux_helm_repositories` | `name, line_number, generate_name` | `property:Flux*.generate_name` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxGitRepositoryGenerateNameOnly`, `go/internal/parser/yaml/flux_test.go::TestParseFluxKustomizationGenerateNameOnly` | Compose-backed fixture verification | A CR using `metadata.generateName` instead of `metadata.name` has an empty `name` (never `"<nil>"`) and carries the literal `generate_name` field so the empty name is explained (issue #5360 PR A; extended to HelmRelease/HelmRepository by issue #5483 C1). |
| Kustomization `RECONCILES_FROM` source edge | `flux-reconciles-from-edge` | supported | n/a (projector-canonical, not a parser bucket) | `resolution_mode, source_ref_kind, source_ref_name, source_ref_namespace, namespace_defaulted, reconciler_kind` | `edge:FluxKustomization-[RECONCILES_FROM]->FluxGitRepository/FluxOCIRepository/FluxBucket` | `go/internal/storage/cypher/canonical_flux_edges_test.go` | `go/internal/query/relationships_catalog_flux_test.go`, B-12 `rc-152` | Resolved in Go at projection time (not the parser): same-repo, deterministic T1-T4 tiers applying Flux's own same-namespace default; a dangling ref, a declared-namespace mismatch, or an unresolved ambiguity (ties, ambiguous ownerless candidates) is an honest non-link, never a fabricated join (issue #5360 PR B). `reconciler_kind` (`Kustomization`\|`HelmRelease`) is stamped on every edge this verb writes (issue #5483 C1). |
| HelmRelease (`helm.toolkit.fluxcd.io`) | `flux-helm-releases` | supported | `flux_helm_releases` | `name, line_number` | `node:FluxHelmRelease` | `go/internal/parser/yaml/flux_helm_test.go::TestIsFluxHelmRelease` | Compose-backed fixture verification | Previously fell through to the generic `k8s_resources` bucket. |
| HelmRelease chart | `flux-helm-release-chart` | supported | `flux_helm_releases` | `name, line_number, chart, chart_version` | `property:FluxHelmRelease.chart/chart_version` | `go/internal/parser/yaml/flux_helm_test.go::TestParseFluxHelmReleaseCapturesChartSourceRef` | Compose-backed fixture verification | `spec.chart.spec.chart` is a chart NAME for a `HelmRepository` source, a PATH for `GitRepository`/`Bucket` sources. |
| HelmRelease chart sourceRef | `flux-helm-release-source-ref` | supported | `flux_helm_releases` | `name, line_number, source_ref_kind, source_ref_name, source_ref_namespace` | `property:FluxHelmRelease.source_ref_kind/name/namespace` | `go/internal/parser/yaml/flux_helm_test.go::TestParseFluxHelmReleaseCapturesChartSourceRef` | Compose-backed fixture verification | Same three row keys `flux-kustomization-source-ref` uses; namespace is omitted, never defaulted, when absent (the reducer-side default rule applies at edge-resolution time). |
| HelmRelease chartRef | `flux-helm-release-chart-ref` | supported | `flux_helm_releases` | `name, line_number, chart_ref_kind, chart_ref_name, chart_ref_namespace` | `property:FluxHelmRelease.chart_ref_kind/name/namespace` | `go/internal/parser/yaml/flux_helm_test.go::TestParseFluxHelmReleaseCapturesChartRef` | Compose-backed fixture verification | Distinct keys from `source_ref_*` -- a HelmRelease with both `spec.chart` and `spec.chartRef` set (invalid per the Flux API) still captures both verbatim; the exactly-one-of validation belongs to the edge resolver, never the parser. |
| HelmRepository (`source.toolkit.fluxcd.io`) | `flux-helm-repositories` | supported | `flux_helm_repositories` | `name, line_number` | `node:FluxHelmRepository` | `go/internal/parser/yaml/flux_source_test.go::TestIsFluxHelmRepository` | Compose-backed fixture verification | Previously fell through to the generic `k8s_resources` bucket. |
| HelmRepository url | `flux-helm-repository-url` | supported | `flux_helm_repositories` | `name, line_number, url` | `property:FluxHelmRepository.url` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxHelmRepositoryCapturesURLAndType` | Compose-backed fixture verification | `spec.url` is the chart-index coordinate. |
| HelmRepository type | `flux-helm-repository-type` | supported | `flux_helm_repositories` | `name, line_number, repo_type` | `property:FluxHelmRepository.repo_type` | `go/internal/parser/yaml/flux_source_test.go::TestParseFluxHelmRepositoryCapturesURLAndType` | Compose-backed fixture verification | Captured under `repo_type`, deliberately NOT the generic `type` key, to avoid a collision at the content-metadata/query layer. |
| HelmRelease `RECONCILES_FROM` source edge | `flux-helmrelease-reconciles-from-edge` | supported | n/a (projector-canonical, not a parser bucket) | `resolution_mode, source_ref_kind, source_ref_name, source_ref_namespace, namespace_defaulted, reconciler_kind, via` | `edge:FluxHelmRelease-[RECONCILES_FROM]->FluxHelmRepository/FluxGitRepository/FluxOCIRepository/FluxBucket` | `go/internal/storage/cypher/canonical_flux_helm_edges_test.go` | B-12 `rc-` HelmRelease correlation | Reuses the SAME T1-T4 resolution tiers as Kustomization, verbatim (issue #5483 C1). `via` (`chart_source_ref`\|`chart_ref`) records which reference field resolved the edge. `chartRef.kind: HelmChart` is a deliberate honest non-link (see Known Limitations); reachable only through `get_entity_context`, not `list_relationship_edges` (the catalog slice stays anchored on `FluxKustomization`). |
| GitRepository cross-repo `DEPLOYS_FROM` lineage | `flux-git-repository-cross-repo-deploys-from` | supported | n/a (reducer-resolved evidence, not a parser bucket) | `url, normalized_url, flux_git_repository_namespace, flux_git_repository_name` | `edge:Repository-[DEPLOYS_FROM]->Repository` | `go/internal/relationships/flux_evidence_test.go` | `go/internal/query/impact_trace_deployment_flux_test.go`, `go/internal/storage/postgres/ingestion_flux_cross_repo_evidence_integration_test.go` | Query-supported cross-repo lineage: resolved in Go at ingestion-commit time (`discoverStructuredFluxEvidence`, issue #5483 C2), never in the parser: STRICT `repositoryidentity.NormalizeRemoteURL` equality between `spec.url` and each catalog repository's `RemoteURL` -- never the fuzzy alias/token matcher. Exactly one OTHER-repository match emits the edge; a same-repo match, zero matches, or 2+ matches are honest non-links tallied by `eshu_dp_flux_cross_repo_url_resolution_total` (outcome=`linked`\|`self`\|`unresolved`\|`ambiguous`), never guessed. The `trace_deployment_chain` read binds a Flux controller only when its `sourceRef.kind` is `GitRepository` and its effective namespace/name exactly matches the dedicated projected GitRepository namespace/name. Effective namespace is explicit `sourceRef.namespace`, otherwise the controller's own namespace; if both are absent, the binding is skipped rather than defaulted. Exactly one target repository is required; missing, ambiguous, and saturated bindings are skipped and surfaced in the trace's controller limits. |

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
- `HelmRelease` (`helm.toolkit.fluxcd.io`) and `HelmRepository`
  (`source.toolkit.fluxcd.io`) are modeled as typed content entities
  (issue #5483 C1), reachable through `get_entity_context`. The
  `RECONCILES_FROM` edge extends to `FluxHelmRelease` sources through the
  identical resolution tiers, but is reachable only through
  `get_entity_context`, not `list_relationship_edges` (see Known Limitations
  below).
- **Cross-repo lineage** (issue #5483 C2): a `FluxGitRepository`'s `spec.url`
  resolves to a `Repository` node identity via STRICT
  `repositoryidentity.NormalizeRemoteURL` equality against the catalog's
  `RemoteURL` for every indexed repository -- never a fuzzy alias/token
  match. A unique match that names a repository OTHER than the one hosting
  the manifest emits a `DEPLOYS_FROM` evidence fact
  (`go/internal/relationships/flux_evidence.go`), so a deployment trace can
  follow a Flux Kustomization or HelmRelease across repositories. A
  same-repo match is skipped (already covered by `RECONCILES_FROM`); zero or
  2+ matches are honest non-links, tallied by the
  `eshu_dp_flux_cross_repo_url_resolution_total` metric
  (outcome=`linked`\|`unresolved`\|`ambiguous`\|`self`), never guessed.
  `FluxKustomization` and `FluxHelmRelease` are wired into the deployment
  trace's controller-entity surface the same way `ArgoCDApplication` is (see
  Known Limitations below for the target-repo-root matching gap this does
  not yet close).

Not claimed today:

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
  honest non-link -- not the strict-URL cross-repo lineage `DEPLOYS_FROM`
  edge covers, which resolves `spec.url` identity, not a `sourceRef` name
  join, and is deliberately never used to paper over this same-repo-only
  `RECONCILES_FROM` gap (see the Framework And Library Support cross-repo
  lineage note above and the target-repo-root limitation below).
- **Cross-repo target matching is deliberately narrow.**
  `FluxKustomization`/`FluxHelmRelease` target-repo path matching requires an
  exact `GitRepository` effective namespace/name and exactly one evidence-backed target
  repository. Explicit `sourceRef.namespace` wins; otherwise the controller's
  own namespace is used. No namespace means unknown and is never fabricated.
  A missing identity, multiple targets, or a saturated binding
  read is an honest skip; the trace never applies a controller root to every
  repository with a `DEPLOYS_FROM` edge. Other controller kinds retain their
  repo-local matching behavior.
- **HelmRelease resolves through `get_entity_context` only, never
  `list_relationship_edges`.** `HelmRelease.spec.chart.spec.sourceRef`
  (`HelmRepository`/`GitRepository`/`Bucket`) or `spec.chartRef`
  (`OCIRepository` only) resolves through the SAME T1-T4 tiers as
  Kustomization's `sourceRef` (issue #5483 C1), and the whole-graph
  `RECONCILES_FROM` tile count on the relationships catalog includes these
  edges automatically. But the catalog's bounded edge-slice endpoint
  (`list_relationship_edges`) stays anchored on `FluxKustomization`
  (`relationshipVerbCatalog` is verb-keyed, so a second `RECONCILES_FROM`
  entry would clobber the existing one rather than add coverage) -- a
  HelmRelease-sourced edge is honestly reachable only through
  `get_entity_context`, the generic graph-projected-node edge surface.
- **`chartRef.kind: HelmChart` is a deliberate, permanent non-link, not a
  follow-up gap.** Eshu's existing `HelmChart` label models a `Chart.yaml`
  DIRECTORY ((name, path) identity), not the Flux `HelmChart` custom
  resource. Linking a HelmRelease's `chartRef.kind: HelmChart` to that label
  would be a fabricated cross-class join between two unrelated graph
  identities that happen to share a name, so `fluxHelmChartRefKindToLabel`
  omits it on purpose. Never add it without a distinct, dedicated typed
  label for the Flux HelmChart CR first.
- **Exactly-one-of `chart`/`chartRef` is a resolver-side rule, not a parser
  validation.** A HelmRelease manifest that sets BOTH (invalid per the Flux
  API) or NEITHER is captured verbatim by the parser; the edge resolver
  treats either case as an honest non-link, never an arbitrary pick.
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
