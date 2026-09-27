# #7284: golden-corpus readiness map enrollment and reverse guard

## Problem

`readinessDeferredFailureClasses` in `go/cmd/golden-corpus-gate` is read only by
`classifyResidualRows`, for rows with `status = 'retrying'`. It has two roles:

- For a strict drain it is a diagnostic label. The drain still waits for the
  row either way; the map only decides whether the timeout message calls it
  live or readiness-deferred.
- For `preMaintenanceQuiescence` (`-drain-allow-readiness-deferred`, used by the
  Ifá pre-maintenance cells, each followed by the maintenance pass and a strict
  drain) it is a control decision. An enrolled class is tolerated; a missing
  class counts as live and holds quiescence open.

The existing guard, `TestReadinessDeferredFailureClassesAreNonCounting`, checked
only one direction: every map entry must be in the reducer queue's
`nonCountingReducerRetryFailureClasses`. Nothing required the reverse, and 20
non-counting classes had no gate decision.

## Decision per class

Enrolled (19):

| Class | Reason |
| --- | --- |
| `aws_cloud_image_nodes_not_ready`, `aws_relationship_nodes_not_ready`, `azure_relationship_nodes_not_ready`, `ec2_block_device_kms_posture_nodes_not_ready`, `ec2_internet_exposure_nodes_not_ready`, `ec2_uses_profile_nodes_not_ready`, `iam_can_assume_nodes_not_ready`, `iam_can_perform_nodes_not_ready`, `iam_escalation_nodes_not_ready`, `iam_instance_profile_role_nodes_not_ready`, `observability_coverage_nodes_not_ready`, `rds_posture_nodes_not_ready`, `s3_external_principal_grant_nodes_not_ready`, `s3_internet_exposure_nodes_not_ready`, `s3_logs_to_nodes_not_ready`, `security_group_reachability_nodes_not_ready`, `workload_cloud_relationship_nodes_not_ready` | Each waits on the canonical-nodes phase for its own scope and generation. No pre-cell corpus can enqueue one, and none writes a family the pre cells assert absent. |
| `cloud_admission_not_ready` (#6887) | The blocker is another scope's `cloud_inventory_admission` item, which the same snapshot already counts as live or dead_letter. |
| `value_flow_inputs_not_ready` (#6923) | A singleton that writes only value-flow evidence. Its unenrolled wait (up to 30 minutes) would time pre drains out, the same shape #6785 enrolled `workload_cloud_relationship_instances_not_ready` for. |

Excluded (1), in `readinessLiveByDesignFailureClasses`:

| Class | Reason |
| --- | --- |
| `generation_activation_not_ready` (#6686) | Attaches to any reducer domain and resolves without the maintenance pass. The generation check runs in front of every handler, so the class can sit on deployable_unit_correlation, workload_materialization, or repo-dependency rows, which are the families pre cells assert absent. Tolerating it would let pre-maintenance quiescence pass before a gated family's intent has evaluated its gate. Its blocker, a projector-stage row, is already counted live. |

The map moved out of `drains.go` (495 lines) into
`drains_readiness_classes.go` to stay under the 500-line cap. It stays a set of
string literals, so the gate binary takes no production import of the reducer.

> Superseded by #7308 ([7308-readiness-map-split.md](7308-readiness-map-split.md)).
> The single map is now two sets. The label is derived from
> `storagepostgres.IsNonCountingReducerRetryFailureClass`, so the gate binary
> now imports storage/postgres, and `generation_activation_not_ready` is
> labeled readiness-deferred. The 19 enrolled classes above, with the earlier
> 17, form `preMaintenanceToleratedFailureClasses`. The exclusion moved to
> `preMaintenanceBlockingFailureClasses`, which still blocks quiescence.

## Guard

- `storagepostgres.NonCountingReducerRetryFailureClasses()` returns a clone of
  the set; storage/postgres remains its only owner.
- `TestEveryNonCountingFailureClassIsEnrolledOrExcluded`
  (`drains_readiness_classes_test.go`) fails when the accessor returns nothing,
  a non-counting class is in neither map, a class is in both, an exclusion has a
  blank reason, or an exclusion is no longer non-counting.
- `TestClassifyResidualRowsSplitsEnrolledFromExcludedReadiness` runs the
  production classifier: a retrying `generation_activation_not_ready` row
  counts live and a retrying `value_flow_inputs_not_ready` row counts deferred.

> Superseded by #7308. `TestEveryNonCountingFailureClassIsEnrolledOrExcluded` is
> now `TestEveryNonCountingFailureClassHasPreMaintenanceDecision`, and
> `TestClassifyResidualRowsSplitsEnrolledFromExcludedReadiness` is now
> `TestClassifyResidualRowsSplitsToleratedFromBlockingReadiness`. A retrying
> `generation_activation_not_ready` row now counts as readiness-deferred and
> pre-maintenance blocking, not live.

## RED / GREEN

RED, with the new test and accessor in place, the map untouched, and the
exclusion map emptied:

```text
--- FAIL: TestEveryNonCountingFailureClassIsEnrolledOrExcluded
    non-counting reducer retry classes with no gate decision: [aws_cloud_image_nodes_not_ready
    aws_relationship_nodes_not_ready azure_relationship_nodes_not_ready cloud_admission_not_ready
    ec2_block_device_kms_posture_nodes_not_ready ec2_internet_exposure_nodes_not_ready
    ec2_uses_profile_nodes_not_ready generation_activation_not_ready iam_can_assume_nodes_not_ready
    iam_can_perform_nodes_not_ready iam_escalation_nodes_not_ready
    iam_instance_profile_role_nodes_not_ready observability_coverage_nodes_not_ready
    rds_posture_nodes_not_ready s3_external_principal_grant_nodes_not_ready
    s3_internet_exposure_nodes_not_ready s3_logs_to_nodes_not_ready
    security_group_reachability_nodes_not_ready value_flow_inputs_not_ready
    workload_cloud_relationship_nodes_not_ready]
rc=1
```

GREEN after enrolling the 19 and excluding the 1:
`go test ./cmd/golden-corpus-gate -run 'NonCounting|Readiness' -count=1` gives
`ok`, rc=0.

Seeded mutations, each applied alone and then restored (the files were checked
byte-equal to the pre-mutation copies with `cmp`). Each went RED (rc=1) and
named the seeded class:

| Mutation | Failure |
| --- | --- |
| append `seeded_fake_not_ready` to the non-counting set | `no gate decision: [seeded_fake_not_ready]` |
| delete `s3_logs_to_nodes_not_ready` from the map | `no gate decision: [s3_logs_to_nodes_not_ready]` |
| exclude `rds_posture_nodes_not_ready` too | `classes both enrolled and excluded: [rds_posture_nodes_not_ready]` |
| blank the exclusion reason | `entries with a blank reason: [generation_activation_not_ready]` |
| exclude `graph_write_timeout` | `classes the reducer queue no longer exempts: [graph_write_timeout]` |
| remove `value_flow_inputs_not_ready` from the map | classifier test: `live 5 deferred 0`, want `live 3 deferred 2` |

No-Regression Evidence: the change touches no SQL, queue claim, lease, worker,
or graph write. `nonCountingReducerRetryFailureClasses` and both claim paths
derived from it are unchanged; the new accessor only returns a copy of the set
and is called only from gate tests. The gate's drain SQL is unchanged. The only
runtime effect is in `classifyResidualRows`: retrying rows in the 19 enrolled
classes now count as readiness-deferred instead of live. Strict drains still
wait for them. Pre-maintenance quiescence now tolerates them until the
maintenance pass and the strict drain after it.

No-Observability-Change: no metric, span, or log key is added or removed. The
drain breakdown line keeps its field set; the enrolled classes now appear under
`readiness-deferred=` rather than `live=` in the existing pre-maintenance and
timeout messages, which is the intended correction.
