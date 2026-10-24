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

## Performance and observability (#7951)

Performance Evidence (#7951): the floor of 50 candidates per repository, the
12,000-candidate request budget and the 500-path blank-check chunk were
measured on a read-only shim of this algorithm (floor 50, E = 12,000, RepoID
walk, reads of 10 paths per chunk, at most 8 chunks in flight) over the
census seed: 317 related
repositories, 15,695 candidates, 70 repositories over the old cap with 7,774
candidates hidden by it. Baseline A is the stop-at-50 body, arm B is the
floor plus budget; 9 ABBA rounds on a load-checked host, PostgreSQL 18.
Arm B costs +0.487 CPU-s as the paired median (range +0.212 to +0.570) and
+232 chunk statements, with blank checks unchanged at 298; B is a row-for-row
superset of A (+11,185 rows, 0 lost), and A's digest is unchanged. Peak heap
growth is 6.4 MiB at most on the census repositories, 1.5 MiB on the 36.7 MB
stress repository and 34.7 MiB on the earlier 3,678-candidate repository, at
most 0.42% of the 8 GiB API limit. The plan shim found no sequential scan at
any size: the generic plan is an Index Scan on
`content_files_repo_path_pattern_idx` from 10 to 4,950 paths, and the custom
plan flips to a Bitmap Heap Scan through the same index at 1,171 paths on the
census statistics (between 1,300 and 1,400 on the first seed), which is why
the blank check is chunked at 500, where both plans are an Index Scan. This
code differs from the shim in one way: the blank check is chunked at 500 (the
shim sent one per repository, so at most +18 statements on the census seed).
Both walk the anchor first. Not measured: a deployed image (CPU, wall time and
heap of the built binary, and the plan the production statement cache
produces), the heap of a kustomization that pulls many large YAML resources,
and E on real traffic; 12,000 stays provisional until the deployed read.

Observability Evidence (#7951): the story's `deployment_artifacts` stage line,
the repository context's `content_infrastructure_overview` stage line and the
service routes' `deployment_evidence` stage line carry `candidate_count`,
`candidates_hydrated`, `candidates_unhydrated`, `repositories_budget_capped`,
`repositories_list_truncated`, `hydration_budget`, `hydration_floor`,
`blank_check_statements` and, on the service routes, `anchor_list_truncated`.
`eshu_dp_config_artifact_candidates_total{outcome}` counts hydrated and
unhydrated candidates and, as list_truncated, the related repositories whose
file list was cut. A response that is a lower bound names the bound in
`limitations` or `partial_reasons`, and sets `truncated` (story) or
`result_limits.truncated` (service and workload routes); the repository context
has no `truncated` field.

### Anchor-loader whole-window hydration (follow-up of #7951)

Benchmark Evidence (#7951 anchor follow-up):
`BenchmarkAnchorHydrationWholeWindow` hydrates 500 workflow candidates through
the chunked anchor pass on the in-memory `fakeContentStore` (go1.26.9
linux/amd64, AMD EPYC 9R14): 50 `GetFileContents` statements, ~1.75 ms/op
median over 3 runs of ~687 iters, 500 of 500 rows hydrated. Baseline is the
pre-follow-up pass: at most 50 one-file `GetFileContent` statements covering
only the first 50 candidates, so any window with more than 50 candidates
returned an incomplete body. After the change the statement count is
ceil(candidates/50) (#7009) and the body is the whole window: 60 candidates
cost 2 statements for 60 rows instead of 50 statements for 50 rows. The
5,000-file window bounds the worst case at 100 statements; anchor candidates (compose,
workflow, controller files) are rare per repository, so the usual cost is 0-2
statements. The in-flight bound stays 8, the bound the per-file reader used,
and row semantics are unchanged (rows by `RelativePath`, missing row keeps
the list row, first chunk error fails the pass), so the change is safe.

No-Observability-Change: no new metric, span, log field, or stage-line field.
The `candidate_count`/`candidates_hydrated` stage lines and
`eshu_dp_config_artifact_candidates_total` already cover the config load; the
anchor pass adds no per-loader counts of its own.
The metric and log fields are in
[Telemetry metrics](../../../../docs/public/reference/telemetry/metrics.md) and
[Telemetry logs](../../../../docs/public/reference/telemetry/logs.md).

### Canceled-context hydration (#8889)

No-Regression Evidence (#8889): the pass now checks the context before each
chunk send and before each chunk read, and returns the parent context error
after the workers drain. `BenchmarkAnchorHydrationWholeWindow` (500
candidates, 10 `GetFileContents` statements, go1.26.10 linux/arm64, Neoverse-V3,
16 CPUs, `-benchtime=2s`, three alternating runs) measured 1.318, 1.342 and
1.340 ms/op on `origin/main` and 1.311, 1.336 and 1.314 ms/op with the change,
so the extra `ctx.Err()` loads are inside run-to-run noise. The statement
count is unchanged, the concurrency bound stays 8, and a pre-canceled context
now issues 0 statements. `go test -race -count=200 -cpu 1,16` over the three
tests in `repository_artifact_hydration_cancel_test.go` (pre-canceled,
mid-flight, worker-error-first) passed 600 of 600 runs at each `-cpu` setting
with the change; without it the pre-canceled and mid-flight tests failed 200
of 200 runs at each setting.

No-Observability-Change: no new metric, span, log field, or stage-line field.
A canceled request surfaces as the context error to the caller; no signal is
added by this package.

### Hydration chunk width ten to fifty (#7009)

Benchmark Evidence (#7009): the hydrate pass reads one `GetFileContents`
statement per fifty paths instead of ten. Baseline is the width-10 tree with
the same candidate budgets; after the change 60 candidates cost 2 statements
for 60 rows instead of 6, pinned by
`TestAnchorHydrationReadsWholeCandidateWindow`, and the config-artifact
fan-out keeps the row and whole digests byte-identical while
`BatchStatements` moves 28 to 6 and `BatchChunkSize` 10 to 50, pinned by
`TestConfigArtifactFanOutMatchesSequentialDigests`. Statement shape is proved
on the in-memory `fakeContentStore` and `meetingStore` fakes (`go test`, no
live backend); the #7009 shim behind `ReadChunkSize` (120 repositories, 60
two-KiB consumed files each) drops the hydrate wall from 6.8 ms to 3.0 ms per
repository while a 21 MB twenty-file repository reads in one 58 ms statement.
The 5,000-file window bounds the worst case at 100 statements, the 8-in-flight
bound is unchanged (pinned by
`TestChunkedReadKeepsAtMostEightChunksInFlight` at 400 paths and peak 8), and
row semantics are unchanged, so the change is safe.

No-Observability-Change (#7009): no new metric, span, log field, or
stage-line field. Statement counts move inside the existing
`BatchStatements`/`BatchChunkSize` stats the stage lines already carry.

### Anchor fallback truncation flag (#8157)

No-Regression Evidence (#8157): the flag adds one bool to existing returns
and changes no statement shape. The fallback still reads
`anchorCandidateListLimit+1` rows in one `ListRepoFiles` call, pinned by
`TestAnchorLoaderListsReadWholeWindowWithSentinel` on all three loaders, and
hydration still costs ceil(candidates/50) `GetFileContents` statements with 8
in flight, pinned at 2 statements for 60 candidates by
`TestAnchorHydrationReadsWholeCandidateWindow`. Baseline is the pre-#8157
tree with the same limits and chunking. After the change:
`go test ./internal/query/repositoryartifacts/ -count=1` is ok,
`-race` on the anchor tests is ok, and the `query`, `query/service`,
`query/cicd`, and `content/artifacts` suites are all ok.

No-Observability-Change (#8157): no new metric, span, log field, or
stage-line field. The flag travels in return values only; the service routes
keep disclosing cuts through the existing `AnchorFileListTruncatedReason`
path.

## Dependencies

Standard library, `querycontract`, `content/artifacts` (the neutral
config-artifact package and its `prefilter` subpackage), and content-parsing
libraries, plus `testutil` in tests. No handler packages, no
graph drivers.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root
`query` suites. Artifact predicates feed access decisions: keep the
fail-closed behavior and bounded limits exactly as they are.
