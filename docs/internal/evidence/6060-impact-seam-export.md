# #6060 — impact-family seam export (lane B, first PR)

This note covers the seam-export commit on `6060-laneb-contentread`: the
46-file impact move set (`impact_*`, `deployment_trace_*`, `impact.go`,
`prechange_impact.go`, `prechange_impact_request.go`) keeps every file in
place, and `go/internal/query/impact_seam.go` aliases and forwards the 39
production-used symbols the go/types pass found called from outside that
set. Six `ImpactHandler` methods are renamed at their declarations
(`profile` to `ResolvedProfile`, `preChangeImpactResponse`,
`fetchDeploymentSourceGitOps`, `fetchDeploymentSourceResult`,
`fetchK8sResourceResult`) with all callers updated; two test doubles shared
with stay-root sweep tests move to `querytestutil` with identical bodies
(`FakeGraphReaderWithSingle`, `RecordingResourceInvestigationGraph`,
`ResourceInvestigationRunCall`). No method body, Cypher fragment, SQL
statement, queue interaction, or handler route changes in this commit; the
diff is aliases, forwarders, renames, and test repoints.

No-Regression Evidence (seam export): `go build ./internal/query/...`,
`go vet ./internal/query/...`, and `go test ./internal/query/... -count=1`
all pass on the branch; `TestImpactSeamExportsForward` asserts every
forwarder returns what its original returns on the same input and every
alias names the same object, so a silent behavior split fails the suite
instead of shipping. The renamed methods keep their bodies; only their
identifiers and call sites changed, which the compiler checks exhaustively.

No-Observability-Change (seam export): the diff adds no spans, metrics,
structured-log fields, or status surfaces, and touches no telemetry
call sites; `rg telemetry` over the changed files returns only the
pre-existing import in `prechange_impact.go`, which this commit does not
modify.
