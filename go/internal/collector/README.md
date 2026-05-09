# Collector

## Purpose

`internal/collector` owns git collection, filesystem-direct collection,
repository discovery, snapshot capture, and parser input shaping for Eshu
indexing runs. It turns source repositories into the inputs required by fact
emission: cloned snapshots, native snapshots, discovery reports, file
selections, and entity metadata. It does not make graph projection or
query-time truth decisions — those belong to the projector, reducer, storage,
and query packages.

## Where this fits in the pipeline

```mermaid
flowchart LR
  A["git source\n(remote or filesystem)"] --> B["internal/collector\nGitSource + NativeRepositorySnapshotter"]
  B --> C["internal/collector/discovery\nResolveRepositoryFileSetsWithStats"]
  C --> D["internal/parser\nEngine.ParsePath + PreScanRepositoryPathsWithWorkers"]
  D --> E["internal/facts\nfacts.Envelope channel"]
  E --> F["Postgres fact store\nCommitter.CommitScopeGeneration"]
  F --> G["Projector queue\n(downstream)"]
```

## Internal flow

```mermaid
flowchart TB
  A["Service.Run\npoll Source.Next"] --> B{"generation\navailable?"}
  B -- no --> C["AfterBatchDrained?\nwait PollInterval"]
  B -- yes --> D["commitWithTelemetry\nSpanCollectorObserve"]
  D --> E["Committer.CommitScopeGeneration\ndurable Postgres write"]
  A2["GitSource.Next\nstartStream on first call"] --> F["discoverRepositories\nSelector.SelectRepositories\nSpanScopeAssign"]
  F --> G["resolveRepositories\nabsolute paths + stable sourceRunID"]
  G --> H["two-lane workers\nsmallCh + largeCh + largeSem"]
  H --> I["NativeRepositorySnapshotter.SnapshotRepository\ndiscovery → pre-scan → parse → materialize"]
  I --> J["buildStreamingGeneration\nfactCh + streamFacts goroutine"]
  J --> K["stream send\ncollected.Facts channel"]
```

## Lifecycle / workflow

`Service.Run` is the poll-and-dispatch loop. It calls `Source.Next` to receive
one `CollectedGeneration` at a time. When no generation is ready, it calls
`AfterBatchDrained` if at least one generation was committed since the last
drain, then waits `PollInterval` (1 second in `cmd/ingester`). On receipt of a
generation it calls `Committer.CommitScopeGeneration` with the `facts.Envelope`
channel and records `CollectorObserveDuration`, `FactsEmitted`,
`GenerationFactCount`, and `FactsCommitted`.

`GitSource.Next` manages a per-batch streaming lifecycle. On the first call per
batch it launches `startStream`, which:

1. Calls `Selector.SelectRepositories` to discover the current repository list
   (span: `SpanScopeAssign`).
2. Resolves all paths to absolute form and computes a stable `sourceRunID` via
   `facts.StableID`.
3. Classifies repositories into `smallCh` and `largeCh` by file count via
   `isLargeRepository` (skips `.git`, `node_modules`, `vendor`, `.venv`,
   `__pycache__`).
4. Launches `s.SnapshotWorkers` goroutines (default 8). Workers prefer small
   repos; large repos acquire a `largeSem` semaphore (capacity
   `LargeRepoMaxConcurrent`) before snapshotting so at most N large parses run
   concurrently.
5. A coordinator goroutine closes `s.stream` when all workers finish.

Subsequent `Next` calls read one generation from `s.stream`. When the stream
channel closes, `Next` returns `ok=false` and resets for the next discovery
cycle.

For filesystem sources, `NativeRepositorySelector.SelectRepositories` uses a
manifest under the managed repository cache to avoid reselecting unchanged
workspaces. The manifest hashes the files the collector can actually use:
`.gitignore` and `.eshuignore` rule files are included, while files excluded by
those rules are skipped. This keeps local watch mode from creating new
generations for ignored logs, build outputs, or editor scratch files.

`NativeRepositorySnapshotter.SnapshotRepository` runs four sequential stages
per repository:

1. **Discovery** — `resolveNativeSnapshotFileSet` calls
   `discovery.ResolveRepositoryFileSetsWithStats` with repo-local overrides from
   `.eshu/discovery.json`, `.eshu/vendor-roots.json`, `.gitignore`, and
   `.eshuignore` applied before parsing.
2. **Pre-scan** — `engine.PreScanRepositoryPathsWithWorkers` builds the import
   map concurrently.
3. **Parse** — `buildParsedRepositoryFiles` parses each file through the
   `parser.Engine` worker pool; each parsed file becomes a `map[string]any`
   entry in `snapshot.FileData` and may carry semantic metadata such as
   dead-code root evidence. `snapshotParserOptions` keeps language-specific
   variable scope close to query needs: Java uses module-level variables so
   method locals do not flood canonical graph projection, while dynamic
   languages that rely on local-variable evidence still parse with
   `VariableScope=all`.
4. **Materialize** — `shape.Materialize` turns parsed files into
   `ContentFileMeta` records and `ContentEntitySnapshot` rows. Body strings are
   released after materialization; `streamFacts` re-reads them from disk at emit
   time so snapshot memory is `O(single_file)`.

`buildStreamingGeneration` launches a background goroutine that streams
`facts.Envelope` values through a buffered channel (`factStreamBuffer = 500`).

## Exported surface

- `Service` — poll-and-dispatch loop; wire `Source`, `Committer`,
  `PollInterval`, and optionally `AfterBatchDrained`, `Tracer`,
  `Instruments`, `Logger`
- `Source` — interface: `Next(context.Context) (CollectedGeneration, bool, error)`
- `Committer` — interface: `CommitScopeGeneration(ctx, scope, generation, <-chan facts.Envelope) error`
- `CollectedGeneration` — `Scope`, `Generation`, `Facts` channel, `FactCount`,
  optional `DiscoveryAdvisory`
- `GitSource` — implements `Source`; fields include `Selector`,
  `Snapshotter`, `SnapshotWorkers`, `LargeRepoThreshold`,
  `LargeRepoMaxConcurrent`, `StreamBuffer`
- `NativeRepositorySnapshotter` — implements `RepositorySnapshotter`; fields
  include `Engine`, `Registry`, `DiscoveryOptions`, `SCIP`, `ParseWorkers`
- `RepositorySelector` — interface: `SelectRepositories(context.Context) (SelectionBatch, error)`
- `RepositorySnapshotter` — interface: `SnapshotRepository(context.Context, SelectedRepository) (RepositorySnapshot, error)`
- `SelectionBatch` — `ObservedAt` + `[]SelectedRepository`
- `SelectedRepository` — `RepoPath`, `RemoteURL`, `IsDependency`, `DisplayName`,
  `Language`, `FileTargets`
- `RepositorySnapshot` — `RepoPath`, `RemoteURL`, `FileCount`, `ImportsMap`,
  `FileData`, `ContentFileMetas`, `ContentEntities`, `DiscoveryAdvisory`
- `ContentFileSnapshot`, `ContentFileMeta`, `ContentEntitySnapshot` — portable
  file and entity records; `ContentFileMeta` carries no body string
- `RepoSyncConfig` — all env-driven sync configuration; populated by
  `LoadRepoSyncConfig`
- `LoadRepoSyncConfig(component, getenv)` — parses the repo-sync env contract
- `LoadDiscoveryOptionsFromEnv(getenv)` — parses `ESHU_DISCOVERY_IGNORED_PATH_GLOBS`
  and `ESHU_DISCOVERY_PRESERVED_PATH_GLOBS`
- `LoadSnapshotSCIPConfig(getenv)` — parses the SCIP env contract
- `SnapshotSCIPConfig` — `Enabled`, `Languages`, `Indexer`, `Parser`
- `DiscoveryAdvisoryReport` — operator-facing JSON summary of discovery and
  materialization shape per snapshot run
- `ClaimedService` — wraps `Service` with a `ClaimControlStore` for
  workflow-coordinator-gated collection
- `FactsFromSlice` — test helper: builds a `CollectedGeneration` from a
  pre-built `[]facts.Envelope` slice
- `terraformstate` subpackage — exact Terraform-state source readers and
  streaming parser primitives that emit redacted Terraform-state facts

## Dependencies

- `internal/collector/discovery` — `ResolveRepositoryFileSetsWithStats`,
  `Options`, `RepoFileSet`, `DiscoveryStats`
- `internal/parser` — `Engine`, `Registry`, `Options`, `DefaultEngine`,
  `DefaultRegistry`, `SCIPIndexer`, `SCIPIndexParser`, `SCIPParseResult`
- `internal/facts` — `facts.Envelope`, `facts.StableID`
- `internal/scope` — `scope.IngestionScope`, `scope.ScopeGeneration`
- `internal/content/shape` — `shape.Materialize`, `shape.Input`
- `internal/repositoryidentity` — `MetadataFor`
- `internal/telemetry` — spans, metrics, structured logging

## Telemetry

- Spans: `SpanCollectorObserve` (`collector.observe`) wraps each commit cycle;
  `SpanCollectorStream` (`collector.stream`) wraps the full stream lifecycle;
  `SpanScopeAssign` (`scope.assign`) wraps repository discovery;
  `SpanFactEmit` (`fact.emit`) wraps per-repo snapshotting
- Metrics: `eshu_dp_collector_observe_duration_seconds`,
  `eshu_dp_scope_assign_duration_seconds`, `eshu_dp_fact_emit_duration_seconds`,
  `eshu_dp_repo_snapshot_duration_seconds`, `eshu_dp_file_parse_duration_seconds`,
  `eshu_dp_repos_snapshotted_total` (labeled `status=succeeded/failed`),
  `eshu_dp_facts_emitted_total`, `eshu_dp_facts_committed_total`,
  `eshu_dp_fact_batches_committed_total`, `eshu_dp_generation_fact_count`,
  `eshu_dp_discovery_files_skipped_total` (labeled `skip_reason`),
  `eshu_dp_large_repo_classifications_total` (labeled `repo_size_tier`),
  `eshu_dp_large_repo_semaphore_wait_seconds`
- Log events: `collector stream started`, `collector snapshot stage completed`
  (stages: `discovery`, `pre_scan`, `parse`, `materialize`), `collector snapshot
  completed`, `collector commit succeeded / failed`, `collector stream completed /
  failed`, `large repository queued`, `large repo semaphore acquired / released`

## Operational notes

- `ESHU_SNAPSHOT_WORKERS` (default `min(NumCPU,8)`) controls concurrent
  per-repo snapshotting. Raising this value beyond CPU capacity increases
  context-switching without reducing wall time.
- `ESHU_LARGE_REPO_FILE_THRESHOLD` (default `1000`) classifies repositories for
  the large-repo semaphore. The classification is a fast pre-scan that exits
  early once the threshold is exceeded.
- Repo-local `.eshu/discovery.json` and `.eshu/vendor-roots.json` override default
  discovery options before the operator-level `ESHU_DISCOVERY_IGNORED_PATH_GLOBS`
  overlay is applied.
- Filesystem manifest fingerprints include `.gitignore` and `.eshuignore` rule
  files but exclude paths filtered by those rules. Changing an ignore rule
  reselects the repository; changing only ignored output does not.
- Two-phase streaming: `ContentFileMeta` carries no body; `streamFacts`
  re-reads file bodies from disk at emit time. The OS page cache keeps re-reads
  fast. Do not change this design to in-memory bodies without accounting for
  `O(repo_size)` memory growth on large repositories.
- Parser variable scope is part of performance and truth. Java defaults to
  module-level variables during native snapshots because dead-code candidates
  and Java call inference do not need every method-local declaration as a
  canonical `Variable` node. Keep JS/TS/Python local-variable coverage intact
  unless their query contracts change.
- Terraform-state sources are explicit-source only in the reader stack. Do not
  route repo-local `.tfstate` through Git content persistence or broad
  repository discovery.

## Extension points

- `RepositorySelector` — replace `NativeRepositorySelector` with any
  implementation to change how repositories are discovered
- `RepositorySnapshotter` — replace `NativeRepositorySnapshotter` with any
  implementation to change how repositories are snapshotted
- `Source` / `Committer` — both are interfaces; test implementations substitute
  recording or controlled-error variants
- `SnapshotSCIPConfig.Indexer` and `.Parser` — injectable seams for testing SCIP
  paths without external binaries

## Gotchas / invariants

- `GitSource.startStream` performs synchronous discovery before launching
  snapshot workers. A slow `Selector.SelectRepositories` (e.g. slow GitHub API
  response) blocks the entire stream start.
- Large-repo semaphore is acquired inside the worker select loop, not inside
  `processRepo`. This means a worker never blocks waiting for the semaphore while
  small repos are available (`git_source.go:419-431`).
- `streamErr` is written by the coordinator goroutine and read by `Next` only
  after the stream channel closes. The happens-before guarantee is that
  `close(s.stream)` happens-before the receive in `Next` that returns
  `ok=false`.
- Absolute paths: `resolveRepositories` calls `filepath.Abs` on every selected
  repo path before building the `sourceRunID` hash. Do not pass relative paths
  to `NativeRepositorySnapshotter.SnapshotRepository` — it calls
  `filepath.Abs` again but the fact IDs would differ.
- Filesystem manifests must stay aligned with copy/direct snapshot filtering.
  If `fingerprintTree` starts hashing ignored generated files, local watch mode
  can keep publishing newer generations and supersede projector work before the
  graph settles.

## Related docs

- `docs/docs/architecture.md` — collector ownership
- `docs/docs/deployment/service-runtimes.md` — concurrency tuning env vars
- `docs/docs/reference/local-testing.md` — local verification gates
- `docs/docs/reference/telemetry/index.md` — metric and span reference
- `go/internal/collector/discovery/README.md` — file enumeration detail
- `go/internal/parser/README.md` — language adapter and registry detail
