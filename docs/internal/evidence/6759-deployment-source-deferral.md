# #6759: deployment-source target deferral — non-counting, bounded, fail-closed

Issue #6759 reported a missing `DEPLOYMENT_SOURCE` edge in the Ifá
`failgraphwritedeployableunit` cell. The stated mechanism, a
`deployable_unit_correlation` replace dropping the edge, was wrong. The
correlation Cypher only touches `CORRELATES_DEPLOYABLE_UNIT`. The workload
materializer's `DEPLOYMENT_SOURCE` statement MATCHed both endpoints and then
MERGEd. When the deploy `Repository` node did not exist yet, that statement
was a silent no-op, while the pass still counted the row as written.

#6730 fixed that with the fail-closed target probe
`checkDeploymentSourceTargets`. The golden-corpus gate on NornicDB reported 2
missing-edge mismatches in about 52 runs before #6730 and 0 in 455 after.
Those are reported gate figures, not re-run here. The mechanism was reproduced
as a diagnosis on the gate's NornicDB image using standard
MATCH-miss-then-MERGE Cypher. This change adds no live-graph proof.

This change closes three gaps left on that guard's path.

1. `deploymentSourceTargetMissingError` now self-classifies as
   `workload_materialization_deployment_source_target_not_ready`, and that
   class is enrolled in `nonCountingReducerRetryFailureClasses`. Previously a
   slow repo_dependency lane dead-lettered the intent, and a dead letter is
   never reopened.
2. The non-counting wait is bounded by elapsed time since
   `crossscope.ReadinessCycleAnchor(intent)`, using
   `crossscope.ProducerReadinessMaxWait` (30 minutes). That is the same bound
   the cross-scope floor and the #6785 waits use. Past the bound the handler
   returns a classless counting error, so the ordinary attempt budget
   dead-letters it loudly.
3. A probe error now fails the pass closed with `deploymentSourceProbeError`,
   which is retryable and counting. It no longer writes without verification.

No-Regression Evidence: the happy path is unchanged. The target probe, the
MERGE statements and their batching are untouched, so no statement, round trip
or lock is added when targets are present. The new code runs only on error
paths:

- On a target miss it does one `errors.As` and one time subtraction against
  the intent's cycle anchor.
- On a probe error it returns a different error type in place of writing.

On a miss, queue cost matches the sibling non-counting readiness classes: one
claim plus one retry UPDATE per retry delay while the target is absent. That
cost is now capped at 30 minutes of wall time, where before the item stopped
after 3 counted passes.

The proof is the focused Go and fake-Postgres suite, run on go1.27.1
darwin/arm64 against the branch rebased onto b298c32a1. Both commands pass:

- `go test ./internal/reducer/ ./cmd/reducer/ ./internal/storage/cypher/ ./cmd/golden-corpus-gate/`
- `go test ./internal/storage/postgres/ -run 'Readiness|DeploymentSource|GenerationActivation'`

The RED/GREEN pairs are:

- `TestReducerQueueFailDefersDeploymentSourceTargetMissPastAttemptBudget`
- `TestEveryReadinessFailureClassIsEnrolled`
- `TestWorkloadMaterializerDeploymentSourceProbeErrorFailsClosed`
- `TestWorkloadMaterializationDeploymentSourceDeferralIsBoundedByElapsedTime`

Observability Evidence: a deferral shows up as
`eshu_dp_reducer_retry_surge_total{failure_class="workload_materialization_deployment_source_target_not_ready"}`
and in `fact_work_items.failure_class` on the retrying row.
`eshu_dp_shared_edge_target_miss_total{domain="workload_materialization"}`
counts each detected miss. The guard logs these WARN lines:

- `deployment-source batch target absent, deferring pass`, with sample
  instance and repository ids.
- `deployment-source target probe failed, failing the pass closed`, on a probe
  error.

Past the 30-minute bound, the counting error names the elapsed wait. The row
then ends in the ordinary dead-letter status, which carries the failure class.
