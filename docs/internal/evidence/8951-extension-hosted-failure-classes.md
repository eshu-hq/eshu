# #8951 Extension-Hosted Failure Classes Evidence

No-Regression Evidence: the status snapshot's instance read
(`workflowCoordinatorCollectorInstancesQuery` in
`go/internal/storage/postgres/workflow_status.go`) selects one more column,
`configuration::text`, from `collector_instances`. The table holds one row per
configured collector instance, bounded by operator configuration, and the query
already reads `configuration` to return `configuration->'scan_interval'`, so
the new column is the same jsonb value rendered as text. It adds no join, no
predicate, and no index use, and it runs once per status snapshot, not per
work item. The hot terminal-failure read
(`workflowCollectorTerminalFailuresQuery`) is unchanged, so migration 211's
index doctrine and its #8803 measurements still apply. This is an argument from
the query shape. No live PostgreSQL timing was taken for this change.

Observability Evidence: no new metric, span, or log key. The existing gauge
`eshu_runtime_coordinator_collector_terminal_failure_instances` keeps its
labels. `failure_class` stays inside `BoundedFailureClasses` plus `unknown`,
because the hosted marker only moves a pattern-valid class from `unknown` to the
existing `extension_defined` token. `TestStatusMetricsHandlerTerminalFailureGaugeCollapsesHostedFirstPartyClasses`
asserts two hosted `jira` instances share one `extension_defined` series, an
in-tree `jira` instance stays `unknown`, and no class text reaches the scrape.

## Input shape

- Component-activated instance: stored `configuration` carries
  `schema_version=eshu.component.instance.v1` and a `component_id`, the check
  `activation.IsConfiguration` shares with the coordinator scheduler. The reader
  parses the text, keeps the boolean in `InstanceSummary.ExtensionHosted`, and
  drops the text.
- In-tree instance: blank, `{}`, or target configuration. The flag stays false.

## Safety

The flag is never serialized (`json:"-"`) and never a label value. A malformed
activation document counts as extension-hosted, matching the scheduler's
partition, so no instance falls between the two readers.
