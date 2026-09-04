# #6060 — impact-family seam export (lane B, first PR)

This note covers the seam-export commit on `6060-laneb-contentread`: the
46-file impact move set (`impact_*`, `deployment_trace_*`, `impact.go`,
`prechange_impact.go`, `prechange_impact_request.go`) keeps every file in
place, and `go/internal/query/impact_seam.go` aliases and forwards the 34
production-used symbols the go/types pass found called from outside that
set: 5 type aliases (`K8sResourceResult`,
`ProvisioningRepositoryCandidate`, `DeploymentSourceResult`,
`PreChangeImpactRequest`, `TraceEnrichmentConfig`), 2 constants
(`DeveloperChangePlanCapability`, `ImpactMaxListLimit`), 1 variable
(`ErrAmbiguousTraceWorkloadSelector`), 25 function forwarders
(`AppendUniqueString`, `ContainsString`, `BoundedK8sResourceResult`,
`DistinctSortedInstanceField`, `QueryProvisioningRepositoryCandidates`,
`BoundedTraceEnrichmentLimit`, `CanonicalWorkloadIDCandidate`,
`CompactStringMap`, `DeploymentEvidenceDeliveryPaths`,
`NormalizedDeliveryPathKey`, `NormalizePreChangeImpactRequest`,
`PreChangeGraphTarget`, `PreChangeSummary`, `ImpactRepoIDAllowed`,
`FilterRowsByRepoIDForAccess`,
`FilterProvisioningRepositoryCandidatesForAccess`, `FirstPositiveFloat`,
`NormalizeImpactListLimit`, `TrimImpactRows`,
`LoadProvisioningSourceChainsFromCandidates`,
`LoadConsumerRepositoryEnrichmentFromCandidates`,
`FetchServiceTraceContext`, `UniqueStrings`, `PreChangeImpactErrorStatus`,
`JoinOrNone`), and 1 method forwarder (`ResolvedProfile`; see below).
Four `ImpactHandler` methods are renamed at their declarations
(`PreChangeImpactResponse`, `FetchDeploymentSourceGitOps`,
`FetchDeploymentSourceResult`, `FetchK8sResourceResult`) with all callers
updated; `profile()` is NOT renamed -- it stays unexported at its
declaration in `impact.go`, and `ResolvedProfile()` in `impact_seam.go`
forwards to it, because renaming `profile` rewrote the bodies of
`traceResourceToCode` and `explainDependencyPath` whose digests are frozen
in `go/internal/queryplan/grandfathered_non_hot.go` (fixed in `199067f8c`).
Two test doubles shared with stay-root sweep tests move to `querytestutil`
with identical bodies (`FakeGraphReaderWithSingle`,
`RecordingResourceInvestigationGraph`, `ResourceInvestigationRunCall`),
with unexported delegating root adapters kept alongside their consumers
(see the adapter rule in `go/internal/query/querytestutil/AGENTS.md`). No
method body, Cypher fragment, SQL statement, queue interaction, or handler
route changes in this commit; the diff is aliases, forwarders, renames,
and test repoints.

Review follow-up (same branch): the seam grows by 10 exported names that
cover the deployment-config-influence family's lowercase field uses through
the aliases without touching any pinned body -- `NewTraceEnrichmentConfig`
(the only cross-set construction is `traceEnrichmentConfig{maxDepth: 4}`),
`DeploymentSourceResult` `Rows`/`Limits`/`SetRows`, and `K8sResourceResult`
`Rows`/`Limits`/`ImageRefs`/`Candidates`/`ContentLowerBound`/`SelectCandidatePoolTruncated`
-- proven usable cross-package by `TestImpactSeamCrossPackageAccess`.
The 12 consuming test files revert to unexported root adapters
(`fakeGraphReaderWithSingle`, `recordingResourceInvestigationGraph`) that
delegate to the promoted `querytestutil` doubles, per the adapter rule in
`go/internal/query/querytestutil/AGENTS.md`.

No-Regression Evidence (seam export): `go build ./internal/query/...`,
`go vet ./internal/query/...`, and `go test ./internal/query/... -count=1`
all pass on the branch; `TestImpactSeamExportsForward` asserts every
forwarder returns what its original returns on the same input and every
alias names the same object, so a silent behavior split fails the suite
instead of shipping. The four renamed methods keep their bodies; only
their identifiers and call sites changed, which the compiler checks
exhaustively. `profile()` is the contrast: it was never renamed, so there
is no renamed body to keep -- `ResolvedProfile()` is a forwarder defined in
`impact_seam.go` that delegates to the untouched `profile()`.

No-Observability-Change (seam export): the diff adds no spans, metrics,
structured-log fields, or status surfaces, and touches no telemetry
call sites; `rg telemetry` over the changed files returns only the
pre-existing import in `prechange_impact.go`, which this commit does not
modify.
