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
rewrites accepted repo/run units while preserving repo-wide retraction
semantics. By default it runs one partition and one worker. When configured
with multiple code-call partitions and workers, it may process distinct
file-scoped delta partitions concurrently, but whole-scope or legacy rows fence
same-repository file and whole-scope partitions by pending-row order. Very
large accepted units are processed in capped chunks: the first chunk retracts
when prior durable history exists, and later chunks from the same source run
skip retraction only when the same partition has already completed, so other
file partitions still retract their owned delta file paths. In
local-authoritative NornicDB runs it can receive a `ReducerGraphDrain`; when
active reducer graph domains remain, the runner records a blocked cycle and
waits before claiming a code-call partition. The gate only schedules work. It
does not change which rows become `CALLS`, `REFERENCES`, or `USES_METACLASS`.

No-Regression Evidence: `go test ./internal/reducer ./internal/storage/postgres
-run 'TestCodeCallProjectionRunnerWholeScopeBlocksLaterWholeScope|TestCodeCallProjectionRunnerRetractsForDifferentCurrentRunPartition|TestCodeCallProjectionRunnerSkipsRetractForCurrentRunChunkAfterFirstChunk|TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunPartitionDomainIntents'
-count=1` failed before same-repository whole/legacy rows were mutually fenced
and current-run history was partition-scoped, then passed.
`go test ./internal/reducer -run
'TestCodeCallProjectionRunner(LaterWholeRefreshDoesNotBlockEarlierFilePartition|ScansAcceptanceUnitForCoveringRefreshBeyondDomainPage)'
-count=1` failed before later whole refreshes stopped fencing earlier file rows
and file refresh fences scanned the selected acceptance unit beyond the current
domain page, then passed. The fix preserves useful concurrency: earlier file
rows can drain before later whole refresh rows, while covered file rows wait for
same-run file-scoped refreshes even when the refresh sorts outside the current
`ListPendingDomainIntents` page.

Observability Evidence: no new telemetry surface is needed. The code-call lane
continues to expose partition lease timing, selection timing, blocked
readiness, processed/retracted/upserted row counts, code-call runner timing,
shared-intent backlog/status rows, and edge writer counters for diagnosing
contention, skipped work, retries, and progress.

Configuration via `LoadSharedProjectionConfig` reads
`ESHU_SHARED_PROJECTION_*` env vars; see `cmd/reducer/README.md`.

## SQL and inheritance domains

`InheritanceMaterializationHandler` and `SQLRelationshipMaterializationHandler`
load only the `content_entity` rows whose `entity_type` can participate in
their domains. The filters are correctness filters, not sampling: every allowed
type is still processed, and unsupported types stay invisible to those domain
reducers.

`InheritanceMaterializationHandler` no longer writes canonical edges directly.
It emits durable shared-projection intents (`inheritance_intents.go`): one
whole-scope per-repo refresh intent that owns the single retract, plus one
write-only per-edge intent per inheritance edge under a file-scoped,
edge-unique partition key. The generic partitioned worker projects them and the
#2898 refresh fence holds every per-edge write until the refresh's retract has
committed, so a repo whose edges span partitions no longer loses edges to a
per-partition repo-wide retract (#2867/#2910). The retract the refresh owns is
repo-wide on `child.repo_id` by default and file-scoped on `child.path` when the
generation is a delta — the refresh carries `delta_projection` and the repo's
changed `delta_file_paths`, matching the inheritance edge writer's delta
dispatch. The per-edge partition key hashes `(repo, child_path, edge identity)`:
the edge identity is required because the worker's
`LatestIntentsByRepoAndPartition` deduplicates by partition key, so two edges
sharing one file-only key would collapse and silently drop an edge.

The refresh fence is generation-scoped (#5554). A completed refresh opens a
per-edge write only for the same `(scope, acceptance unit, source run,
generation, refresh partition, domain)`, so a reused `source_run_id` cannot
carry the fence across generations. Exact same-generation delivery is
idempotent: `BuildSharedProjectionIntent` derives the same intent IDs and the
Postgres upsert preserves `completed_at`, so completed refresh and edge rows are
not re-fired. Unrelated partitions retain their existing leases and worker
concurrency; only edges awaiting their own generation's refresh are deferred.

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
- Refresh completion is generation-local. An exact same-generation retry is a
  durable no-op; a later generation must complete its own refresh.
- The code-call runner's drain gate is scheduling only. It must not change
  admitted graph truth.
- SQL trigger `EXECUTES` edges protect stored routine reachability. Removing
  them can create false dead-code candidates.
