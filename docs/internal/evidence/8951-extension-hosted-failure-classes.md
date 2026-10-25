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

- Extension-hosted instance: any stored `configuration` the generic planner
  claims, which `activation.IsConfiguration` shares with the coordinator
  scheduler. That is a valid activation document
  (`schema_version=eshu.component.instance.v1` plus a `component_id`), and also
  a malformed or incomplete one, a non-object jsonb value (`[]`, `"x"`, `5`),
  a wrong-typed field (`{"component_id":5}`), or a `schema_version` with no
  `component_id`. The reader parses the text, keeps the boolean in
  `InstanceSummary.ExtensionHosted`, and drops the text.
- In-tree instance: blank, `{}`, or target configuration. The flag stays false.

## Reader cost

The parse the reader runs per instance, per status snapshot or scrape, costs
O(configuration bytes). A reviewer reported a single local sample of 4.74 ms/op
with 1.0 MB/op and 6 allocs/op for a 1 MiB in-tree configuration, and 0.82 us/op
with 10 KB/op for a small one. The command, hardware, and run count were not
recorded, so read these as an illustration of the scaling, not as evidence.
Before this change only the `scan_interval` value crossed the wire. The
instance count is bounded by operator configuration. No live PostgreSQL timing
was taken.

## Admin surfaces

The failed-scope list (`ListTerminalFailures`) and the acknowledgement target
(`resolveTarget`, also used by `Acknowledge`) already read the instance
registry, so passing the matched instance's `ExtensionHosted` flag adds no
query. `ListTerminalFailureAcks` reads stored records across instances, so a
non-empty listing now makes one more registry read (the same query the status
snapshot runs, bounded by operator configuration), after the record rows are
closed; an empty listing makes none. A record whose instance is gone reads the
kind-only form. Operators see these reads in the existing Postgres query spans
and `eshu_dp_postgres_query_duration_seconds`; there is no new metric or log
key. No live PostgreSQL timing was taken.

## Safety

The flag is never serialized (`json:"-"`) and never a label value. A malformed
activation document counts as extension-hosted, matching the scheduler's
partition, so no instance falls between the two readers.
