# Repository artifacts package

## Purpose

`repositoryartifacts` holds the file-content artifact readers for the
repository handler family (Issue #6060, lane B): config artifacts (Ansible,
Compose, HCL, Kustomize), deployment artifacts, runtime artifacts
(Dockerfile), controller artifacts, CloudFormation artifacts, and workflow
artifacts (GitHub Actions), plus the shared candidate-file hydration and
the config/deployment/workflow artifact loaders behind the repository story
responses.

## Ownership boundary

This package owns content-derived artifact reads, not handler orchestration
and not graph reads. It imports only the standard library, `querycontract`,
`content/artifacts` and content-parsing libraries. It never imports `repository`,
`repository/readmodel`, or the query root. The `repository` package and a
handful of staying root stayers consume its exported loaders and
predicates.

## Exported surface

The exported surface is described in [doc.go](doc.go). Loaders and
predicates the `repository` package or staying root stayers call are
exported; per-family artifact shaping stays unexported. Cross-package test
pins go through `testutil` or `querycontract`.

The static workflow evidence and the repository-scoped CI/CD evidence have
listing entry points (`StaticWorkflowArtifactEvidence`,
`LoadRepositoryScopedCICDEvidence`) and `...FromFiles` entry points that take
the file list the caller already read. The repository story uses the latter so
one `ListRepoFiles` read serves the semantic overview and every later stage
(#7126); the two forms return identical evidence for the same files. The listing
entry points read `RepositorySemanticEntityLimit+1` rows and clip to the limit;
the `...FromFiles` entry points take the clipped list plus a `filesTruncated`
flag carrying the caller's sentinel (#7619). The candidate pool is unknown
exactly when that sentinel row exists, so a repository with exactly 5,000 files
is a complete scan. A truncated scan carries
`candidate_pool_status=unknown_at_limit`: no observed workflows
means `state=unknown`, while positive evidence keeps its observed count. The
20-path display cap and 50-file image hydration cap remain separate. Stories
preserve the candidate marker and add `static_workflow_coverage_unknown` to
`missing_evidence` only for a capped scan; this does not backfill the typed
API's other historical missing-evidence entries into uncapped stories.

## Config artifacts live in `content/artifacts`

The config-artifact extractors (Kustomize, Docker Compose, Terraform and
Terragrunt HCL with its locals walk, Ansible), the needed-content hydration
plan, the per-repository function `LoadConfigPaths`, the total row order and the
shared `content_files` statements moved to `go/internal/content/artifacts`
(#7126 PR 3a), so a derive in `storage/postgres` can run the same code without
importing a query package. This package keeps:

- the loader (`LoadSharedRepositoryConfigArtifacts`,
  `LoadDeploymentArtifactOverview`): the related-repository graph read, the
  grant filter, the sort by RepoID, the per-request candidate budget, and one
  merge across repositories (`MergeConfigPaths`);
- the adapter (`repository_config_artifacts.go`): `BuildRepositoryConfigArtifacts`,
  `CleanRepositoryRelativePath` and `NormalizeConfigArtifactExpression` under
  the names the handlers call, the `querycontract.FileContent` conversion, and
  `contentStoreReader`, which presents a `ContentStore` as the neutral
  `Reader`. The statement counting wrapper (`countingContentStore`) sits under
  the adapter, so every read is counted;
- `HydrateRepositoryCandidateFiles`, the one-file-per-statement full pass the
  anchor repository's controller, runtime, workflow and compose loaders use.
  Its hydration set comes from `contentartifacts.SelectHydrationIndexes` at
  the allowance `RepositoryArtifactHydrationLimit` (the 50-candidate floor), so
  the order is the config-artifact load's. These anchor-only loaders keep
  their silent 50 cap until the follow-up of #7951 moves them to the chunked
  reader;
- `ConfigArtifactReadStats`, which sums the per-repository `ReadStats` of the
  neutral package with the stage's statement and byte counts.

## Candidate floor and request budget (#7951)

The deployment-artifact load walks the anchor repository first, then the
related repositories the grant filter kept, sorted by RepoID so the body does
not depend on the graph backend's row order. Each repository's allowance is
`HydrationFloor` (50) plus what is left of `CandidateHydrationBudget`
(12,000, provisional until the deployed read); a repository that takes more
than the floor spends the difference. The floor keeps every repository's old
selection, so the rows are a superset of the pre-#7951 rows.

`ConfigArtifactReadStats` sums the candidate counts after the grant filter:
`CandidateCount`, `CandidatesHydrated`, `CandidatesUnhydrated`,
`RepositoriesBudgetCapped`, `RepositoriesListTruncated`, `HydrationBudget`,
`HydrationFloor`. `BoundReasons` names what the response must carry:
`CandidatesBudgetReachedReason` when any candidate inside a window was left
unread, and `RelatedFileListTruncatedReason` when a related list returned the
sentinel row. `AnchorFileListTruncatedReason` is the service routes' reason for
a cut service-repository list. The handlers in `repository` and `service`
attach the reasons, log the counts and add them to
`eshu_dp_config_artifact_candidates_total`; this leaf package emits no
telemetry. The budget is a struct field of `configArtifactLoader`, the test
seam, never an environment setting.

The content-fetch rules, the HCL scan prefilter and their proofs are in
[content/artifacts](../../content/artifacts/README.md).

## Dependencies

Standard library, `querycontract`, `content/artifacts` (the neutral
config-artifact package and its `prefilter` subpackage), and content-parsing
libraries, plus `testutil` in tests. No handler packages, no
graph drivers.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root
`query` suites. Artifact predicates feed access decisions: keep the
fail-closed behavior and bounded limits exactly as they are.
