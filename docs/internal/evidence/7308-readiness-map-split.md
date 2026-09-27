# #7308: split the golden-corpus readiness map into a label and a tolerated set

## Problem

One map, `readinessDeferredFailureClasses`, did two jobs in
`go/cmd/golden-corpus-gate`:

- **Label.** `formatResidualBreakdown` prints `live=… readiness-deferred=…`
  after a drain has already failed. It never changes a verdict.
- **Control.** `preMaintenanceQuiescence` (`-drain-allow-readiness-deferred`,
  used only by the Ifá pre-maintenance cells) decides whether the pipeline is
  quiet enough to stop the reducer and run maintenance.

Because one map served both jobs, each class had to meet the stricter bar.
#7284 excluded `generation_activation_not_ready` from the control decision.
As a side effect, failure breakdowns labeled it "live", which is wrong.

## Design

- **Label, derived.** `readinessDeferred(class)` calls
  `storagepostgres.IsNonCountingReducerRetryFailureClass`. Every class the
  reducer exempts from its retry budget is labeled readiness-deferred as soon
  as it is registered. `go list -deps ./cmd/golden-corpus-gate` already
  includes `internal/storage/postgres` and `internal/reducer`, so the import
  adds no new dependency.
- **Control, an allow-list.** `preMaintenanceToleratedFailureClasses` maps each
  tolerated class to its reason. The key set equals the 36 keys of the old
  map at `7662789ad`: the sorted key lists were diffed and `diff` returned
  rc=0. A non-counting class that is not listed is labeled readiness-deferred
  but blocks quiescence. The test-only `preMaintenanceBlockingFailureClasses`
  holds `generation_activation_not_ready` and its reason.
- **One classifier.** `classifyResidualRows` returns `residualCounts{live,
  readinessDeferred, preMaintenanceBlocking, deadLetter, failed}`. It checks the
  label first, so a class the reducer no longer exempts counts as live even if
  the allow-list still names it. `preMaintenanceQuiescence` also requires
  `preMaintenanceBlocking == 0`. `formatResidualBreakdown` keeps its four
  printed fields.
- **Runner.** `reverifyPreMaintenanceQuiescence` (`runner.go`) now prints
  `drains: pre-maintenance quiescence refused on re-verification, applying the
  strict verdict: <message>` when the fresh re-verification refuses. Before
  this change it fell through to the strict verdict without printing anything.

## Behavior change

- **Strict drains:** none. They still wait for every residual row.
- **Breakdown:** retrying `generation_activation_not_ready` rows move from
  `live=` to `readiness-deferred=`. Residual risk **R1**: a residual made only
  of those rows now carries the suffix "no live work remained … more drain
  time would not have helped". That can mislead, because the class clears
  without the maintenance pass once the projector acknowledges the generation.
  The message is diagnostic only and prints after a drain has already failed.
- **Pre-maintenance decision:** none. It tolerates the same 36 classes and
  still blocks on `generation_activation_not_ready`. The quiescence message
  gains one field, `readiness-not-tolerated=N`, after `failed=`. The runner
  gains one stderr line when it refuses.

## Reason verification

Each reason in `preMaintenanceToleratedFailureClasses` was checked against
source. The pre cells assert, right after the pre drain:

- the resolver logged "cross-repo resolution gated" and wrote no
  `resolver/cross-repo` edges (`ifa_repo_dependency_live.sh`);
- zero `CORRELATES_DEPLOYABLE_UNIT` edges exist (`ifa_deployable_unit_live.sh`);
- zero workload-owned `DEPENDS_ON` edges (`finalization/workloads`) exist
  (`ifa_workload_dependency_live.sh`);
- the rationale truth holds (`ifa_fault_injection_driver.sh`). The rationale
  lane is a shared-intent lane, and the quiescence check already counts that
  lane strictly.

Classes the review flagged:

- `cross_scope_producer_not_ready`: only `NewProducerNotReadyError`
  (`crossscope/readiness_floor_ledger.go`) constructs it. It is reached only
  through `CheckProducerReadinessBeforeLoadWithLedger`, which has two callers:
  `cicdrun/ci_cd_run_correlation.go` and `supplychain/core/evidence_load.go`.
  No pre cell asserts either family absent. The draft reason holds.
- `workload_materialization_deployment_source_target_not_ready` and
  `shared_edge_target_not_ready`: **the draft reason was not substantiated.**
  The draft said both wait "on a Repository node that the repo_dependency lane
  commits". In fact, the shared-edge target comment says "another scope's
  materialization", and Repository nodes are also written by the canonical
  node upsert. Both classes are returned only after the handler has resolved
  deployment relationships to write:
  - `DeploymentSourceRows` exist only for `DeploymentRepoIDs`, which only
    `applyResolvedDeploymentSources` sets.
  - `shared_edge_target_not_ready` reaches `fact_work_items` only through
    `DeployableUnitCorrelationHandler`, after its readiness fences pass and it
    has admitted rows.

  Before the maintenance pass, cross-repo resolution produces no resolved
  relationships (`ifa_deployable_unit_live.sh` header), so neither class occurs.
  The reason was narrowed to `toleratedResolvedTarget`. Residual risk **R2**:
  if a bug opened that gate before maintenance and the target also raced,
  tolerating these classes would let the zero-edge pre-state assertion pass.
  After maintenance, the strict drain and the exact-set edge assertion still
  catch the edge.
- `aws_cloud_runtime_drift_write_superseded` is an admission race on the drift
  writer's watermark, not a readiness gate. Its reason says the family writes
  only runtime-drift findings.

## Tests

- `go/cmd/golden-corpus-gate/drains_readiness_classes_test.go::TestEveryNonCountingFailureClassIsLabeledReadinessDeferred`
- `go/cmd/golden-corpus-gate/drains_readiness_classes_test.go::TestPreMaintenanceToleratedClassesAreNonCounting`
- `go/cmd/golden-corpus-gate/drains_readiness_classes_test.go::TestEveryNonCountingFailureClassHasPreMaintenanceDecision`
- `go/cmd/golden-corpus-gate/drains_readiness_classes_test.go::TestPreMaintenanceQuiescenceFollowsTheDecision`
- `go/cmd/golden-corpus-gate/drains_pre_maintenance_test.go::TestPreMaintenanceBlocksOnGenerationActivationDeferral`
- `go/cmd/golden-corpus-gate/drains_pre_maintenance_test.go::TestReverifyPreMaintenanceQuiescenceNamesTheRefusal`
- `go/cmd/golden-corpus-gate/drains_residual_breakdown_test.go::TestClassifyResidualRowsSplitsToleratedFromBlockingReadiness`

## RED / GREEN

RED on `7662789ad`. The labeling assertion was written as a throwaway probe
test against the existing four-value classifier, then deleted:

```text
--- FAIL: TestRedProbeEveryNonCountingClassLabeledReadinessDeferred (0.00s)
    red_probe_test.go:21: non-counting classes not labeled readiness-deferred: [generation_activation_not_ready]
FAIL
rc=1
```

GREEN after the change: `go test ./cmd/golden-corpus-gate -count=1` gives
`ok`, rc=0.

## Seeded mutations

Each mutation was applied alone to the committed tree. The package tests then
ran, the file was restored with `git checkout`, and `git diff --quiet HEAD`
confirmed the restore. Every mutation went RED (rc=1):

| # | Mutation | Failure (excerpt) |
| --- | --- | --- |
| M01 | label drops `generation_activation_not_ready` | `not labeled readiness-deferred: [generation_activation_not_ready]` |
| M02 | label admits `graph_write_timeout` | `counting class "graph_write_timeout" = {live:0 readinessDeferred:1 …}, want live 1` |
| M03 | tolerated set lists `graph_write_timeout` | `lists classes the reducer queue counts toward the retry budget: [graph_write_timeout]` |
| M04 | tolerated reason blank | `entries with a blank reason: [cloud_admission_not_ready]` |
| M05 | tolerated set emptied | `preMaintenanceToleratedFailureClasses is empty; the subset guard would pass vacuously` |
| M06 | new non-counting class `seeded_fake_not_ready` | `no pre-maintenance decision: [seeded_fake_not_ready]` |
| M07 | tolerated entry `s3_logs_to_nodes_not_ready` deleted | `no pre-maintenance decision: [s3_logs_to_nodes_not_ready]` |
| M08 | `rds_posture_nodes_not_ready` in both sets | `classes both tolerated and blocking: [rds_posture_nodes_not_ready]` |
| M09 | blocking reason blank | `preMaintenanceBlockingFailureClasses entries with a blank reason: [generation_activation_not_ready]` |
| M10 | blocking set lists `graph_write_timeout` | `lists classes the reducer queue no longer exempts: [graph_write_timeout]` |
| M11 | accessor returns an empty slice | `NonCountingReducerRetryFailureClasses returned no classes; every guard over the set would pass vacuously` |
| M12 | predicate ignores the blocking count | `generation_activation_not_ready must block pre-maintenance quiescence` |
| M13 | classifier never counts blocking rows | `generation_activation_not_ready must block pre-maintenance quiescence` |
| M14 | `generation_activation_not_ready` tolerated | `classes both tolerated and blocking: [generation_activation_not_ready]` |
| M15 | a claimed row counted as deferred | `claimed row retaining readiness metadata must remain live` |
| M16 | runner refusal line removed | `stderr "" must contain "pre-maintenance quiescence refused on re-verification"` |

No-Regression Evidence: the change touches no SQL, queue claim, lease, worker,
or graph write. `nonCountingReducerRetryFailureClasses` and both claim paths
derived from it are unchanged; only the accessor's doc comment changed. The
drain SQL is unchanged. Strict drains read the same rows and wait on the same
residual. Pre-maintenance quiescence tolerates the same 36 classes as
`7662789ad`, proven by the key-set diff, and blocks the same
`generation_activation_not_ready` rows. The classifier does one set lookup per
residual group, as before.

No-Observability-Change: no metric, span, or log key is added or removed. The
pre-maintenance quiescence message gains the `readiness-not-tolerated=` field,
and the runner prints one existing-format `drains:` stderr line when
re-verification refuses. In failure breakdowns, `generation_activation_not_ready`
rows appear under `readiness-deferred=` instead of `live=`.
