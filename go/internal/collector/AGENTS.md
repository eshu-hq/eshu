# AGENTS.md — internal/collector guidance for LLM assistants

## Read first

1. `go/internal/collector/README.md` — pipeline position, lifecycle, exported
   surface, telemetry, and an index of the docs below
2. `go/internal/collector/service.go` — `Service.Run` and `commitWithTelemetry`;
   understand the poll loop before touching concurrency or `AfterBatchDrained`
3. `go/internal/collector/repo/git/source.go` — `GitSource.startStream`, the
   two-lane scheduling design, and the large-repo semaphore lifecycle; read
   `go/internal/collector/SCHEDULING.md` before changing giant-repo or
   parse-partition scheduling
4. `go/internal/collector/repo/git/snapshot_native.go` — `NativeRepositorySnapshotter.SnapshotRepository`;
   the five snapshot stages and the two-phase memory design
5. `go/internal/collector/repo/git/selection_config.go` — `RepoSyncConfig` and
   `LoadRepoSyncConfig`; env var names and defaults
6. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry
7. `go/internal/collector/OPERATIONS.md` — runtime knobs, discovery/streaming
   notes, and per-collector-run telemetry evidence before changing collector
   operational behavior
8. `go/internal/collector/DIAGNOSTICS.md` — the repository basename-collision
   diagnostic (issue #3677) before changing repository identity/dedup logic
9. `eshu-hq/eshu-collector-package-registry` — package-registry evidence
   contracts when editing package/feed collection support (extracted with
   #10620; the in-tree `registry/package` tree is deleted)

## Invariants this package enforces

- **Two-phase streaming** — `ContentFileMeta` carries no body string;
  `streamFacts` re-reads bodies from disk at emit time. Memory per buffered
  generation is `O(1)`, not `O(repo_size)`. Do not store body strings in
  `ContentFileMeta` or `RepositorySnapshot` beyond materialization.
  Enforced by `shapeFiles = nil` and `materialization = content.Materialization{}`
  at `gitrepo/git_snapshot_native.go:230-236`.

- **Absolute paths before sourceRunID** — `resolveRepositories` calls
  `filepath.Abs` on every repo path before computing `sourceRunID`. Fact IDs
  derived from relative paths would diverge on subsequent runs.

- **Large-repo semaphore acquired in select, not in processRepo** — the
  semaphore is acquired inside the worker select loop so workers never block on
  the semaphore while small repos are available. Do not move semaphore
  acquisition inside `processRepo` (`gitrepo/git_source.go:419-431`).

- **Repo-local overrides applied before operator-level overlays** —
  `discoveryOptionsWithRepoDiscoveryConfig` applies `.eshu/discovery.json` and
  `.eshu/vendor-roots.json` before the `ESHU_DISCOVERY_IGNORED_PATH_GLOBS`
  operator overlay. This order is intentional and documented in AGENTS.md.

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
  `gitrepo/git_selection_native_test.go` or a new test file. Do not branch inside
  `GitSource` on source mode.

- **Add a new snapshot stage** → add the stage in
  `NativeRepositorySnapshotter.SnapshotRepository` between the existing stages;
  call `logSnapshotStageTiming` with the new stage name; add the metric record
  if the stage has measurable duration; add a test in
  `gitrepo/git_snapshot_native_test.go`. Why: operators use `stage` log fields to
  identify bottlenecks.

- **Change large-repo concurrency defaults** → edit `largeRepoThreshold` and
  `largeRepoMaxConcurrent` in `gitrepo/git_selection_config.go`; update the tuning
  comments with production data (date + repo counts + fact percentages); add a
  test. Read `eshu_dp_large_repo_semaphore_wait_seconds` guidance in the
  telemetry reference before changing defaults.

- **Add a new discovery advisory field** → add the field to
  `DiscoveryAdvisoryReport` or one of its nested types in
  `gitrepo/discovery_advisory.go`; populate it in `buildDiscoveryAdvisoryReport`; add a
  test in `gitrepo/git_snapshot_native_discovery_test.go`.

- **Add package-registry support** → keep normalization and fact-envelope work
  in `registry/package`; keep live registry clients and runtime claim loops in a
  later bounded collector slice; do not materialize package graph truth from
  collector code.

- **Add a new collector family or service scanner** → create package-local
  `doc.go`, `README.md`, and `AGENTS.md` in the same PR; run
  `scripts/verify-package-docs.sh`. If the collector adds worker claims,
  leases, fanout, batching, queue behavior, or downstream Cypher/materialization
  pressure, also run `scripts/verify-performance-evidence.sh` and add
  Performance Evidence plus Observability Evidence markers to a tracked
  docs/ADR/package note.

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
  counts, then the registry collector trace operation. Context deadlines stay
  `registry_retryable_failure`; `registry_canceled` is reserved for shutdown or
  operator cancellation. Do not add registry hosts, repositories, package names,
  tags, digests, paths, account IDs, or credential references to metric labels
  or status messages.

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

- Two-lane scheduling (smallCh + largeCh) in `gitrepo/git_source.go` — changing this
  to a single-lane design removes the convoy prevention that prevents
  small-repo starvation behind large-repo clusters.
- `factStreamBuffer = 500` without a matching Postgres ingestion batch size
  change — mismatched buffer and batch sizes cause channel backpressure or
  under-utilization.
- `AfterBatchDrained` call semantics — removing or reordering the
  backfill and deployment-mapping reopen calls (wired via `AfterBatchDrained`
  in `cmd/ingester`) breaks the bootstrap phase contract defined in AGENTS.md.
  Empty-batch drain hooks must remain opt-in. They are NOT edge-triggered per
  drain window: before #5852 they were, and a shard owning no repositories
  drained once at startup and then starved every later fleet-barrier epoch,
  blocking every shard that had already arrived. The gate is level-gated on
  the `AfterEmptyBatchDrained` flag, so the hook repeats on every idle poll
  for the lifetime of the process and never stops at the first commit. Do not
  "restore" edge triggering or commit-gating; the former reintroduces #5852
  and the latter reintroduces the #10084 stall. A refused commit stored
  nothing, so it sets no `committedSinceDrain` (#10068): latching it would
  open a barrier epoch for a generation that stored nothing. A shard that has
  committed and is then only refused
  keeps arriving join-only (`false`) on every drain and idle poll (#10084),
  which also closes the documented idle-shard stall: an idle shard joins an
  epoch a peer opened instead of stranding it. A refusal must never report
  `hasCommitted=true`.
  `TestServiceMixedLandedAndRefusedDrainCalls` and
  `TestServiceCommittedThenRefusedKeepsArrivingJoinOnly` pin this.
- `AfterBatchDrained`'s `hasCommitted bool` argument — in the steady state
  this is `committedSinceDrain`, true only when a real commit happened since
  the last drain. `Service.Run` deliberately overrides that for exactly ONE
  call per process: `startupMaintenanceEscapeUsed` latches true on the first
  `AfterBatchDrained` call of any kind, and that one call reports
  `hasCommitted = true` regardless of `committedSinceDrain`; every later call
  on the same shard reports the real status, staying join-only when nothing
  committed.
  Do NOT "simplify" this back to always forwarding `committedSinceDrain` — that
  was tried (the #5852 P1 follow-up) and it silently dropped the one startup
  maintenance pass origin/main always runs (there gated on
  `emptyDrainObserved`, which latches on the first drain of ANY kind, not on
  commit status): a shard that owns no repositories would then never be
  allowed to open a barrier epoch, ever. Do NOT hardcode `hasCommitted` to
  `true` or remove the once-per-process latch either — that reintroduces the
  original codex #5852-follow-up storm: a quiet fleet where nothing has
  committed would open and complete an empty barrier epoch on every idle
  poll, running the corpus-wide maintenance pass against an unchanged corpus
  forever. A barrier caller (the ingester) forwards this value into
  `postgres.DeferredMaintenanceBarrierConfig.HasCommitted`, which stays
  join-only regardless of which of the two true-producing paths set it: a
  shard may join an epoch another shard already opened no matter what
  `HasCommitted` says, but it may open a new epoch only when `HasCommitted`
  is true. See `go/internal/storage/postgres/deferred_maintenance_barrier.go`
  and its README for the epoch-open decision this gates.
