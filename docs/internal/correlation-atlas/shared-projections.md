# Shared Projections And Maintenance Runs

Part of the [Correlation Atlas](README.md). Read from code at `7c2948825d`.

A collector's snapshot does not queue these directly. Most follow another
correlation's run. The timed names run on their own cadence under a lease.

| Way | What happens | Members |
|---|---|---|
| Queued rows | The producer's run rewrites rows in `shared_projection_intents` (`UpsertIntents`), and a projection runner writes them to the graph | `code_calls`, `handles_route`, `runs_in`, `invokes_cloud_action`, `inheritance_edges`, `rationale_edges`, `shell_exec`, `sql_relationships`, `repo_dependency` |
| Direct edges | The current producer writes the edges itself through the edge writer (`WriteEdges`) and queues nothing | `codeowners_ownership_edges`, `deployable_unit_edges`, `documentation_edges`, `submodule_pin_edges`, `workload_dependency` |
| Timed | A maintainer runs on a cadence under a lease. The name is the lease partition, not a queued correlation | `collector_evidence_summary`, `supply_chain_impact_winners`, `supply_chain_impact_winners_audit` |

A queued or direct projection is as fresh as its producer. The producer's row
on its group page says what re-runs it.

## Projections and maintenance names

| Correlation | Starts when | Starts again when another source changes | Code |
|---|---|---|---|
| `code_calls` | Queued rows written by the `code_call_materialization` run | None of its own. The `code_call_materialization` run rewrites its queued rows, and a dedicated runner (`reducer/code/call/projection`) projects them | `reducer/code/call/materialization/handler.go` |
| `codeowners_ownership_edges` | Edges written by the `codeowners_ownership` run | None of its own. Written directly by the `codeowners_ownership` run through the edge writer, so it is as fresh as that run | `reducer/code/owners/handler.go` |
| `collector_evidence_summary` | A maintainer at startup and on a fixed cadence. This is a lease name, not a queued correlation | Timed only | `reducer/maintenance/evidence/maintainer.go` |
| `deployable_unit_edges` | Edges written by the `deployable_unit_correlation` run | None of its own. Written directly by the `deployable_unit_correlation` run through the edge writer, so it is as fresh as that run | `reducer/deployable_unit_correlation_edges.go` |
| `documentation_edges` | No production trigger found. A `documentation_materialization` run would write these edges directly, and nothing queues that run | None found | `reducer/documentation_edge_materialization.go` |
| `workload_dependency` | Edges written by the `workload_materialization` run | None of its own. Written directly by the `workload_materialization` run through the edge writer, so it is as fresh as that run | `reducer/workload_materialization_handler.go`<br>`reducer/workload_dependency_reconciliation.go` |
| `handles_route` | Queued rows written by the `code_call_materialization` run, on a full snapshot | None of its own. The `code_call_materialization` run rewrites its queued rows on a full repository snapshot only. A delta snapshot leaves them untouched until the next full one | `reducer/code/call/materialization/routes.go`<br>`reducer/code/call/materialization/refresh.go` |
| `inheritance_edges` | Queued rows written by the `inheritance_materialization` run | None of its own. The `inheritance_materialization` run rewrites its queued rows, and the shared projection worker (`reducer/intents/shared/worker`) projects them | `reducer/inheritance/materialization.go`<br>`reducer/inheritance/delta_scope.go` |
| `invokes_cloud_action` | Queued rows written by the `code_call_materialization` run, on a full snapshot | None of its own. The `code_call_materialization` run rewrites its queued rows on a full repository snapshot only. A delta snapshot leaves them untouched until the next full one | `reducer/code/call/materialization/cloud_actions.go`<br>`reducer/code/call/materialization/refresh.go` |
| `rationale_edges` | Queued rows written by the `rationale_materialization` run | None of its own. The `rationale_materialization` run rewrites its queued rows, and the shared projection worker (`reducer/intents/shared/worker`) projects them | `reducer/rationale_edge_materialization.go`<br>`reducer/rationale_delta_scope.go` |
| `repo_dependency` | Queued rows written by `deployment_mapping`, package correlation, and `code_import_repo_edge` runs | None of its own. Its producers rewrite the queued rows, and a dedicated runner (`reducer/repo_dependency_projection_runner.go`) projects them | `reducer/crossrepo/cross_repo_resolution.go`<br>`reducer/packages/correlation/source_handler.go`<br>`reducer/code_import_repo_edge_handler.go` |
| `runs_in` | Queued rows written by the `code_call_materialization` run, on a full snapshot | None of its own. The `code_call_materialization` run rewrites its queued rows on a full repository snapshot only. A delta snapshot leaves them untouched until the next full one | `reducer/code/call/materialization/workloads.go`<br>`reducer/code/call/materialization/refresh.go` |
| `shell_exec` | Queued rows written by the `shell_exec_materialization` run | None of its own. The `shell_exec_materialization` run rewrites its queued rows, and the shared projection worker (`reducer/intents/shared/worker`) projects them | `reducer/code/shell/handler.go` |
| `sql_relationships` | Queued rows written by the `sql_relationship_materialization` run | None of its own. The `sql_relationship_materialization` run rewrites its queued rows, and the shared projection worker (`reducer/intents/shared/worker`) projects them | `reducer/sqlrelationship/sql_relationship_materialization.go` |
| `submodule_pin_edges` | Edges written by the `submodule_pin` run | None of its own. Written directly by the `submodule_pin` run through the edge writer, so it is as fresh as that run | `reducer/submodule_pin_materialization.go` |
| `supply_chain_impact_winners` | The winners maintainer on a timer. This is a lease name, not a queued correlation | Timed only. A parked `advisory_group_projection` run is woken when the winners watermark advances | `reducer/supplychain/core/winners_maintainer.go`<br>`storage/postgres/reducer_waiting_winners_wake.go` |
| `supply_chain_impact_winners_audit` | `runAuditLoop` calls the winners audit every audit interval (default 10m, `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL`), after the startup full resweep, under a single-owner lease. This is a lease name, not a queued correlation | Timed only | `reducer/supplychain/core/winners_keyed_maintainer.go`<br>`go/cmd/reducer/winners_wiring.go` |

## Related runners

- The graph-projection phase repair queue
  (`storage/postgres/graph_projection_phase_repair_queue.go`) is read by
  `reducer/intents/phase/repair/runner.go`.
- `RetouchWorkItem` is called from
  `storage/postgres/supply_chain_impact_advisory_group_enqueue.go`.
