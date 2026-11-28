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

Dockerfile runtime artifact rows report each stage's base image and tag after
the parser expanded ARG defaults declared before the first `FROM`. A row adds
`base_ref_template` (the `FROM` reference as written) when it held a variable.
When a value is only known at build time the row keeps the placeholder in
`base_tag`, sets `base_ref_unresolved: true`, lists `base_ref_unresolved_args` (variable
names, or one of the parser's fixed markers `$(`, `${`, `$`, `<over-bound>` for a
construct with no name), and carries `base_ref_arg_defaults` as evidence. That evidence is bounded by the
parser (256 bytes per default, 16 per stage): `base_ref_arg_defaults_truncated`
names a default whose text is a prefix and `base_ref_arg_defaults_omitted: true`
says a default was left out. A consumer must not treat a `base_ref_unresolved`
tag as a real version, or a cut default as the whole one.

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
- `HydrateRepositoryCandidateFiles`, the whole-window chunked full pass the
  anchor repository's controller, runtime, workflow and compose loaders use
  (the #7951 follow-up). Its hydration set comes from
  `contentartifacts.SelectHydrationIndexes` at the whole-window allowance, so
  the selection and order match the config-artifact load's; one
  `GetFileContents` statement per fifty paths (#7009) replaced the old
  one-file reads capped at `RepositoryArtifactHydrationLimit` (the
  50-candidate floor, kept exported for #6060). The loader fallback lists read
  `RepositorySemanticEntityLimit+1` rows, clip to the window, and report the
  clip: `listAnchorCandidateFiles` returns a truncated flag alongside the
  clipped list (#8157), and each anchor-only loader propagates it, so a
  direct nil caller can disclose the cut via `AnchorFileListTruncatedReason`
  the same way production callers do. A canceled context starts no read
  after the pass observes it and returns the context error with nil rows
  once the in-flight reads drain (#8889); a chunk failure still wins over
  the context error, and neither case returns a partly hydrated slice;
- `ConfigArtifactReadStats`, which sums the per-repository `ReadStats` of the
  neutral package with the stage's statement and byte counts;
- the shadow compare (`shadow.go`, #8155 PR3c): after the live load, an
  enabled `ShadowConfig` reads the related repositories' stored rows through
  the `StoredReader` port, compares each against its live rows, and reports a
  `ShadowReport` without changing the response. Disabled (the zero value) it
  issues no stored statement;
- the stored load (`stored.go`, #8155 PR3c stored cutover): with an enabled
  `StoredConfig` the anchor loads live, every trusted related repository
  serves its stored rows converted through `namedRowsToMaps` (beside its
  inverse `namedLiveRows`; the neutral package's name-scope guard forbids a
  generic row-map builder there), and every other related repository falls
  back to the live load with its reason counted. One stored statement answers
  the whole related set; a failed read serves everything live. Stored
  repositories spend no request budget and never emit the budget reason;
  their 5,000-window reason comes from the state's `list_truncated`.
  `ConfigureProductionShadowReads`, `ConfigureProductionStoredReads` and
  `ResetProductionReads` (tests only) own the production loader's mode.

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
a cut service-repository list. The three strings are defined in
`querycontract` (`DeploymentArtifactBoundReasons`), which folds them into
`result_limits.truncated` on the service and workload context routes; the
constants here alias them. The handlers in `repository` and `service`
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

### Dockerfile install declarations (#9303)

A Dockerfile stage artifact carries `installs` when its `RUN` or `ADD` source
declares supported packages, PHP extensions, Apache modules or downloads. Each
row has `manager`, `name`, optional `version`, `line_number` and `stage`. These
are source declarations and do not prove the built image contains the package.
The parser reports rejected commands and credential-bearing URLs separately;
query readers never display the rejected source text.
