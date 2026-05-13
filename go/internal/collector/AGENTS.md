# AGENTS.md — internal/collector guidance for LLM assistants

## Read first

1. `go/internal/collector/README.md` — pipeline position, lifecycle, exported
   surface, and telemetry
2. `go/internal/collector/service.go` — `Service.Run` and `commitWithTelemetry`;
   understand the poll loop before touching concurrency or `AfterBatchDrained`
3. `go/internal/collector/git_source.go` — `GitSource.startStream`, the
   two-lane scheduling design, and the large-repo semaphore lifecycle
4. `go/internal/collector/git_snapshot_native.go` — `NativeRepositorySnapshotter.SnapshotRepository`;
   the five snapshot stages and the two-phase memory design
5. `go/internal/collector/git_selection_config.go` — `RepoSyncConfig` and
   `LoadRepoSyncConfig`; env var names and defaults
6. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry
7. `go/internal/collector/packageregistry/README.md` — package-registry
   evidence contracts when editing package/feed collection support

## Invariants this package enforces

- **Two-phase streaming** — `ContentFileMeta` carries no body string;
  `streamFacts` re-reads bodies from disk at emit time. Memory per buffered
  generation is `O(1)`, not `O(repo_size)`. Do not store body strings in
  `ContentFileMeta` or `RepositorySnapshot` beyond materialization.
  Enforced by `shapeFiles = nil` and `materialization = content.Materialization{}`
  at `git_snapshot_native.go:230-236`.

- **Absolute paths before sourceRunID** — `resolveRepositories` calls
  `filepath.Abs` on every repo path before computing `sourceRunID`. Fact IDs
  derived from relative paths would diverge on subsequent runs.

- **Large-repo semaphore acquired in select, not in processRepo** — the
  semaphore is acquired inside the worker select loop so workers never block on
  the semaphore while small repos are available. Do not move semaphore
  acquisition inside `processRepo` (`git_source.go:419-431`).

- **Repo-local overrides applied before operator-level overlays** —
  `discoveryOptionsWithRepoDiscoveryConfig` applies `.eshu/discovery.json` and
  `.eshu/vendor-roots.json` before the `ESHU_DISCOVERY_IGNORED_PATH_GLOBS`
  operator overlay. This order is intentional and documented in CLAUDE.md.

- **Filesystem manifests describe effective input** — `fingerprintTree` includes
  `.gitignore` and `.eshuignore` rule files but skips files those rules exclude.
  Local watch mode depends on this to avoid publishing newer generations for
  ignored logs, build output, or editor scratch files.

- **Source is best-effort** — `doc.go` states collection is best-effort over
  remote and local filesystems. `partial-snapshot` and `discovery-skip`
  outcomes must be handled explicitly by callers.

- **Collector observe spans cover source and commit work** —
  `Service.Run` starts `SpanCollectorObserve` before `Source.Next` and ends it
  after `commitWithTelemetry`, so slow source reads and durable writes are in
  one trace. Sources that implement `ObservedSource` must start the span only
  for real collection attempts, not drained or idle polls.

- **Facts channel buffer matches Postgres batch size** — `factStreamBuffer = 500`
  matches the Postgres ingestion batch INSERT size so the channel drains at the
  same rate the producer fills it. Do not change either without adjusting both.

## Common changes and how to scope them

- **Add a new repository source mode** → add a new `RepositorySelector`
  implementation in a new file; wire it in `git_selection_*.go`; add an env
  var to `RepoSyncConfig` and `LoadRepoSyncConfig`; add a test case in
  `git_selection_native_test.go` or a new test file. Do not branch inside
  `GitSource` on source mode.

- **Add a new snapshot stage** → add the stage in
  `NativeRepositorySnapshotter.SnapshotRepository` between the existing stages;
  call `logSnapshotStageTiming` with the new stage name; add the metric record
  if the stage has measurable duration; add a test in
  `git_snapshot_native_test.go`. Why: operators use `stage` log fields to
  identify bottlenecks.

- **Change large-repo concurrency defaults** → edit `largeRepoThreshold` and
  `largeRepoMaxConcurrent` in `git_selection_config.go`; update the tuning
  comments with production data (date + repo counts + fact percentages); add a
  test. Read `eshu_dp_large_repo_semaphore_wait_seconds` guidance in the
  telemetry reference before changing defaults.

- **Add a new discovery advisory field** → add the field to
  `DiscoveryAdvisoryReport` or one of its nested types in
  `discovery_advisory.go`; populate it in `buildDiscoveryAdvisoryReport`; add a
  test in `git_snapshot_native_discovery_test.go`.

- **Add package-registry support** → keep normalization and fact-envelope work
  in `packageregistry`; keep live registry clients and runtime claim loops in a
  later bounded collector slice; do not materialize package graph truth from
  collector code.

## Failure modes and how to debug

- Symptom: `eshu_dp_repos_snapshotted_total{status="failed"}` rising →
  likely cause: git clone failure, `discovery` stage error, or `parse` stage
  error → check `collector snapshot stage completed` logs for the failing
  `stage` and `error` fields; check workspace disk and git credentials.

- Symptom: `eshu_dp_large_repo_semaphore_wait_seconds` rising →
  likely cause: `ESHU_LARGE_REPO_MAX_CONCURRENT` slots saturated →
  raise the limit cautiously and watch `eshu_dp_gomemlimit_bytes`; profile
  memory per large-repo parse before committing to a higher value.

- Symptom: `eshu_dp_facts_committed_total` lagging behind `eshu_dp_facts_emitted_total` →
  likely cause: Postgres ingestion write pressure →
  check `eshu_dp_postgres_query_duration_seconds`; check Postgres connection
  pool saturation.

- Symptom: registry collector status shows `failure_class=registry_auth_denied`,
  `registry_not_found`, `registry_rate_limited`, `registry_retryable_failure`,
  `registry_canceled`, or `registry_terminal_failure` → likely cause: a bounded
  OCI or package-registry HTTP/transport failure → check `registry_collectors`
  counts, then the registry collector trace operation. Do not add registry
  hosts, repositories, package names, tags, digests, paths, account IDs, or
  credential references to metric labels or status messages.

- Symptom: `collector stream failed` log with `stream_snapshot_failure` →
  likely cause: first non-nil worker error → the first failing repo path and
  error are in the log; fix the repo or add a `.eshu/discovery.json` exclusion.

- Symptom: discovery produces empty `RepoFileSet.Files` for a repo →
  likely cause: all files matched an ignored dir, ignored extension, or
  `.eshu/discovery.json` rule → run `eshu index --discovery-report` on the repo;
  check the discovery advisory skip breakdown from `eshu index --discovery-report`.

- Symptom: local watch mode keeps superseding projector work for an unchanged
  repository → likely cause: the filesystem manifest is hashing a generated file
  the snapshot path later ignores → compare `fingerprintTree` and
  `shouldSkipFilesystemEntry` behavior before changing worker counts.

## Anti-patterns specific to this package

- **Storing body strings in ContentFileMeta** — breaks the two-phase memory
  design; `streamFacts` re-reads from disk precisely to avoid holding bodies.

- **Calling `filepath.Rel` on `RepoFileSet.Files` at the collector level** —
  `Files` are absolute paths; any consumer that needs relative paths must
  rebase them explicitly. Storing relative paths in `RepositorySnapshot` breaks
  `streamFacts` which uses absolute paths to read file bodies.

- **Adding graph or query imports to this package** — `doc.go` states the
  package does not make graph projection or query-time truth decisions. Imports
  of `internal/projector`, `internal/reducer`, `internal/query`, or
  `internal/storage/cypher` do not belong here.

- **Blocking inside processRepo while holding the large-repo semaphore** —
  the semaphore is released via `afterSnapshot` callback before the
  potentially-blocking stream send. Do not move the release to after the
  stream send.

## What NOT to change without an ADR

- Two-lane scheduling (smallCh + largeCh) in `git_source.go` — changing this
  to a single-lane design removes the convoy prevention that prevents
  small-repo starvation behind large-repo clusters.
- `factStreamBuffer = 500` without a matching Postgres ingestion batch size
  change — mismatched buffer and batch sizes cause channel backpressure or
  under-utilization.
- `AfterBatchDrained` call semantics — removing or reordering the
  backfill and deployment-mapping reopen calls (wired via `AfterBatchDrained`
  in `cmd/ingester`) breaks the bootstrap phase contract defined in CLAUDE.md.
