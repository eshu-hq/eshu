# Commit-Scoped Workflow-Image Correlation Evidence (#5424)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Commit-Scoped Workflow-Image Correlation (#5424)

`attachWorkflowImagesToRuns` (`ci_cd_run_correlation_workflow_image.go`)
previously fanned every static `ci.workflow_image_evidence` fact out to every run
sharing the repository_id, so a workflow file declared on one branch lent a
false-confident image correlation to a run built from another branch. The git
collector now stamps the scanned file's commit onto the evidence (new optional
`commit_sha` on `sdk/go/factschema/cicdrun/v1/workflow_image_evidence.go`), and
the join prefers, per run, the workflow file extracted at that run's commit. A
run only takes the commit-blind repository-wide fallback when no workflow file
matched its commit, and that fallback correlates as `derived` rather than `exact`
(`classifyCICDWorkflowImageEvidence`).

Behavior proof (failing-then-green): `go test ./internal/reducer -run
'PrefersCommitMatchedWorkflowImage|RepositoryWideFallbackDerived' -count=1` — two
branches with different declared images correlate per run, and a
commit-mismatched fallback lands derived.

Performance (same shared-repo workflow-image corpus,
`BenchmarkBuildCICDRunCorrelationDecisionsSharedRepoWorkflowImages`, `-count=3`):
~1.35 ms/op before to ~1.23 ms/op after (9824 to 9821 allocs/op). The per-run
selection replaces the full fan-out, so it is a small improvement, not a
regression.

No-Observability-Change: the change adds no metric, span, log field, queue, or
runtime knob. The existing by-outcome `eshu_dp_ci_cd_run_correlations_total`
counter already captures the exact-to-derived shift for repository-wide
fallbacks, and reducer run spans, `eshu_dp_reducer_executions_total`, and the
durable `reducer_ci_cd_run_correlation` payload (which records the correlation
outcome, kind, and reason) remain the operator-visible signals for this path.
