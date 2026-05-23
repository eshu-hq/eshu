# Reducer Shared Projection

This guide holds the durable shared-projection runner details that are too
long for the package README. Keep runner identity, readiness gates, and
domain-specific edge semantics here.

## Runner contract

`SharedProjectionRunner` (`shared_projection_runner.go:95`) iterates all
shared-projection domains and all partitions each cycle, calling
`ProcessPartitionOnce` for each domain/partition pair. Domains processed:
`platform_infra`, `workload_dependency`, `inheritance_edges`,
`sql_relationships`.

The runner uses exponential back-off, doubling each empty cycle and capped at
`5s`, to avoid sustained high-frequency polling during idle periods. When
intents are blocked on a readiness phase (`BlockedReadiness > 0`), it
re-polls at the base interval without backing off.

`CodeCallProjectionRunner` owns the `code_calls` domain separately because it
rewrites one accepted repo/run unit at a time while preserving repo-wide
retraction semantics. Very large accepted units are processed in capped chunks:
the first chunk retracts when prior durable history exists, and later chunks
from the same source run skip retraction so earlier chunk writes stay
graph-visible. In local-authoritative NornicDB runs it can receive a
`ReducerGraphDrain`; when active reducer graph domains remain, the runner
records a blocked cycle and waits before claiming the code-call partition. The
gate only schedules work. It does not change which rows become `CALLS`,
`REFERENCES`, or `USES_METACLASS`.

Configuration via `LoadSharedProjectionConfig` reads
`ESHU_SHARED_PROJECTION_*` env vars; see `cmd/reducer/README.md`.

## SQL and inheritance domains

`InheritanceMaterializationHandler` and `SQLRelationshipMaterializationHandler`
load only the `content_entity` rows whose `entity_type` can participate in
their domains (`inheritance_materialization.go:69-77`,
`sql_relationship_materialization.go:60-69`). The filters are correctness
filters, not sampling: every allowed type is still processed, and unsupported
types stay invisible to those domain reducers.

SQL relationship materialization writes trigger-to-table `TRIGGERS` edges and
trigger-to-function `EXECUTES` edges from the same `SqlTrigger` entity when the
parser proves both targets. The `EXECUTES` row is part of code dead-code
reachability for `SqlFunction` routines, so removing it can turn trigger-bound
stored procedures into false cleanup candidates. The helper code in
`sql_relationship_names.go` indexes both qualified names and trailing
unqualified aliases, then `resolveSQLRelationshipTarget` prefers the same
repository and relative path before falling back only when the SQL name is
unique in the repository; ambiguous cross-file names stay unresolved rather
than creating false reachability.

## Gotchas

- Shared projection is readiness-gated; edge domains must wait for the phase
  that proves their node endpoints exist.
- The code-call runner's drain gate is scheduling only. It must not change
  admitted graph truth.
- SQL trigger `EXECUTES` edges protect stored routine reachability. Removing
  them can create false dead-code candidates.
