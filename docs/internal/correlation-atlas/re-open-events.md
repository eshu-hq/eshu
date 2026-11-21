# Re-Open Events

Part of the [Correlation Atlas](README.md). Read from code at `7c2948825d`.

Each row is one event that makes a finished or parked run start again. "Selects
runs by" is the key that decides which runs are touched. A wide key re-opens
more work than changed. A narrow key can miss a run that needed it.

A re-opened run goes from `succeeded` back to `pending`. What happens to a run
that is claimed or running when the event arrives depends on the path:

- The completion announcement, the holder fan-out, and the service-catalog
  re-trigger flag it to replay after it finishes.
- The maintenance re-open and the producer-activation settle skip it
  (`ReopenSucceeded` in `storage/postgres/reducer_queue_replay.go` touches
  `succeeded` rows only).

## Events

| ID | Event | Re-opens | Selects runs by | Code |
|---|---|---|---|---|
| E1 | A `ci_cd_run_correlation` run finishes | `supply_chain_impact` | Every impact run at its scope's active snapshot, in any scope. No filter on the producer's scope | `storage/postgres/scope/completion/fanout.go`<br>`reducer/crossscope/dependencies.go` |
| E2 | A `container_image_identity` run finishes | `ci_cd_run_correlation` and `supply_chain_impact` | Same as E1. Announced unless the run reports an unchanged support set; announced when that signal is absent (`ShouldEmitIdentityCompletion`) | `reducer/containerimage/container_image_identity_publication.go`<br>`storage/postgres/reducer_queue_ack.go` |
| E3 | One of the five value-flow producers finishes | `code_value_flow_refresh` | The one shared run. Announced only when the producer's affected-repository gate passes | `storage/postgres/value_flow_refresh_ack.go` |
| E4 | A `workload_materialization` run finishes | `service_catalog_correlation` | Finished service-catalog runs whose committed decision names the repository. Every claimed or running service-catalog run at the active snapshot is flagged to replay | `storage/postgres/service_catalog_runtime_retrigger.go` |
| E5 | A snapshot becomes active and the producer-activation consumer settles it. Off unless `ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED` is set | `container_image_identity`, `kubernetes_correlation_materialization`, `aws_cloud_runtime_drift` | Image keys (digests, repository and tag pairs) or resource ARNs shared with the new snapshot | `storage/postgres/ingestion_producer_activation.go`<br>`go/cmd/reducer/producer_activation_wiring.go` |
| E6 | `deployment_mapping` resolves relationship rows | `deployable_unit_correlation`, `workload_materialization`, `service_catalog_correlation` | The repositories the batch added rows for, and only when the correlation's fingerprint is missing or differs | `storage/postgres/relationship_store_completion_wake.go` |
| E7 | Maintenance re-open after an ingest pass | The six correlations in the [maintenance table](#maintenance-re-open-per-correlation) | Per correlation; see that table | `storage/postgres/ingestion_reopen_correlation.go` |
| E8 | The bootstrap index's correlation re-open phase | Same six | Same as E7 | `go/cmd/bootstrap-index/bootstrap_pipeline.go` |
| E9 | A relationship snapshot becomes active | `deployable_unit_correlation`, `workload_materialization` | Runs of the same scope snapshot that were dead-lettered waiting for their producer | `storage/postgres/relationship_store_resolved.go` |
| E10 | A `supply_chain_impact` run changed or retracted a finding | `supply_chain_impact` in other scopes | Scopes that hold an active finding with the same vulnerability id, advisory id, or advisory-group member, plus scopes whose impact run is claimed or running | `reducer/supplychain/core/fanout.go`<br>`storage/postgres/supply_chain_impact_fanout.go` |
| E11 | A git snapshot's dependency marker | `supply_chain_impact` in other scopes | Scopes whose advisories or alerts name a changed package, plus scopes whose impact run is in flight. Scopes whose findings name the repository, when the earlier base is missing, degraded, or pruned | `projector/supply/chain/impact/manifest_trigger.go`<br>`reducer/supplychain/core/manifest_trigger.go` |
| E12 | An operator suppression record changed | `supply_chain_impact` in other scopes | Vulnerability ids in the difference between adjacent suppression snapshots | `reducer/supplychain/core/suppression_fanout_wire.go` |
| E13 | A `supply_chain_impact` write touched advisory groups | `advisory_group_projection` | The touched group ids | `reducer/supplychain/core/group_projection_enqueue.go` |
| E14 | A parked wait's condition is met | The run that parked | The scope and snapshot, the producer that finished, or the winners watermark the wait named | `storage/postgres/reducer_waiting.go` |
| E15 | The `sbom_attestation_attachment` run for a scope snapshot finishes | The parked `supply_chain_impact` run of the same scope snapshot | The same scope and snapshot. This is a parked wait, not a completion announcement | `reducer/supplychain/core/attachment_wait.go`<br>`storage/postgres/reducer_waiting.go` |
| E16 | `workload_materialization` records a repository-readiness repair | The graph-projection phase repair queue | The repositories of the workload run | `reducer/workload_materialization_repo_phase.go`<br>`reducer/intents/phase/repair/runner.go` |
| E17 | A repository-dependency acceptance cycle completes | `workload_materialization` for one repository | Scope, snapshot, entity key, repository, and fence | `reducer/repo_dependency_projection_acceptance_cycle.go`<br>`storage/postgres/reducer_queue_replay.go` |
| E18 | A `secrets_iam_trust_chain` run finishes | `secrets_iam_graph_projection` | The same scope | `storage/postgres/secrets_iam_graph_projection_followup.go` |
| E19 | A `state_snapshot:` scope's snapshot becomes active | `config_state_drift` | That state scope | `storage/postgres/projector_queue_config_state_drift_trigger_hook.go` |
| E20 | The config-state-drift catch-up sweep, every 5 minutes | `config_state_drift` | Active state scopes that have no run | `projector/config_state_drift_catchup_sweeper.go` |
| E21 | An operator request through the admin API | Replay: dead-lettered or failed runs. Reopen: finished work of `repo_dependency` (its completed projection rows), `workload_materialization`, or `submodule_pin` only | Replay: work item ids, scope, stage, or failure class. Reopen: the named correlation | `query/admin/store/replay.go`<br>`query/admin/store/reopen.go` |

## The cross-scope dependency catalog

`dependencyCatalog` in `reducer/crossscope/dependencies.go` is the single list of
producer and consumer pairs for the completion announcement (E1 to E3).

| Consumer | Producers it depends on |
|---|---|
| `ci_cd_run_correlation` | `container_image_identity` |
| `supply_chain_impact` | `container_image_identity`, `ci_cd_run_correlation` |
| `code_value_flow_refresh` | `workload_materialization`, `workload_cloud_relationship_materialization`, `iam_can_perform_materialization`, `aws_resource_materialization`, `code_function_summary` |

A consumer that starts while a producer's snapshot is still activating defers
without spending an attempt, and proceeds on the current answer after 30 minutes
(`ProducerReadinessMaxWait` in `reducer/crossscope/readiness_floor.go`). Only two
producers have a collector kind mapped for that check: `container_image_identity`
and `ci_cd_run_correlation` (`reducer/crossscope/producer_kinds.go`).

## Maintenance re-open, per correlation

The list is `crossScopeCorrelationReopenDomains` in
`storage/postgres/ingestion_reopen_correlation.go`. It runs after an ingest pass
and in the bootstrap index, and it touches finished runs only.

| Correlation | Re-opened when |
|---|---|
| `deployable_unit_correlation` | Its fingerprint row is missing, the logic version moved, or the input fingerprint differs |
| `workload_materialization` | Same gate |
| `service_catalog_correlation` | Same gate |
| `aws_cloud_runtime_drift` | Its fingerprint row is missing, the logic version moved, the `aws_resource` fingerprint differs, or the last audit is older than 24 hours |
| `container_image_identity` | The fleet identity epoch changed, the logic version moved, or no epoch is recorded. Any change re-opens every recorded run |
| `kubernetes_correlation_materialization` | Always |

`deployment_mapping` and `code_import_repo_edge` have separate passes with the
same shape (`storage/postgres/ingestion_reopen_deployment_mapping.go`,
`storage/postgres/ingestion_reopen_code_import.go`).
