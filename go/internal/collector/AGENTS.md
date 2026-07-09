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

- Two-lane scheduling (smallCh + largeCh) in `git_source.go` — changing this
  to a single-lane design removes the convoy prevention that prevents
  small-repo starvation behind large-repo clusters.
- `factStreamBuffer = 500` without a matching Postgres ingestion batch size
  change — mismatched buffer and batch sizes cause channel backpressure or
  under-utilization.
- `AfterBatchDrained` call semantics — removing or reordering the
  backfill and deployment-mapping reopen calls (wired via `AfterBatchDrained`
  in `cmd/ingester`) breaks the bootstrap phase contract defined in CLAUDE.md.
  Empty-batch drain hooks must remain opt-in and edge-triggered so idle
  collectors do not run global maintenance on every poll.

## Evidence

### Thread FileWithSize from discovery to remove os.Stat re-walk (#4850)

Performance Evidence: For a 5000-file repo, this reduces stat-family
  syscalls from 2N to N per repo (one `os.Lstat` per file harvested from the
  existing symlink-classification check in `classifyPath`, zero in partition
  building). Included symlinks (rare) get one additional `os.Stat` follow for
  the target size to preserve byte-identical partition balancing. The
  output-equivalence test (`TestBuildParseSubtreePartitions_FileWithSize_MatchesStatPath`)
  covers regular files, included symlinks (target size ≠ symlink size), and
  the unstattable-file sentinel: old and new paths produce identical partition
  keys, file indexes, and grouping. The size-harvest test
  (`TestCollectSupportedFilesHarvestsSizeFromLstat`) proves discovery
  populates non-zero sizes from the existing Lstat without an extra
  `entry.Info()` call.
No-Observability-Change: No new metric instrument, label, span, structured
  log field, status field, queue domain, worker count, batch size, or runtime
  knob is added. Operators still diagnose parse behavior through the existing
  `eshu_dp_file_parse_duration_seconds` histogram and `collector snapshot stage
  completed` logs. The change is a pure structural optimization — same
  partitions, same parse output, fewer syscalls.

### Carry the sync-resolved SHA to skip a redundant rev-parse HEAD (#4880)

Performance Evidence: git-sync mode removes one `git rev-parse HEAD` subprocess
per selected repository per cycle by carrying the SHA that the sync already
resolved through `checkoutRemoteBranch`. Sync resolves the remote head via
`gitRevParse("refs/remotes/origin/"+branch)` and then `git checkout -B <branch>
refs/remotes/origin/<branch>`, so `git rev-parse HEAD` in the snapshot equals
that already-known SHA. Measured before/after: `TestSnapshotHeadCommitSubprocessCount`
counts `git rev-parse HEAD` invocations in the snapshot path through the
`gitCommitSHAFn` seam and asserts 0 invocations when `SourceCommitSHA` is carried
(sync mode) versus exactly 1 on the empty fallback path — a reduction of one
subprocess per sync-mode repository per collection cycle. The carried
`SourceCommitSHA` on `SelectedRepository` is empty for non-sync selectors
(filesystem, clone, reconcile, or any path that did not run
`checkoutRemoteBranch`), which keep the `gitCommitSHA` shell-out fallback
unchanged. The first-time clone path intentionally does not carry the SHA: a
fresh clone has no pre-resolved SHA to harvest (unlike sync's `remoteSHA`), so
populating one would add an extra `git rev-parse HEAD` rather than remove one;
that rare cold path keeps the fallback. Correctness proof:
`go test ./internal/collector -run
'Test(CheckoutRemoteBranchEquivalence|SnapshotUsesSourceCommitSHA|SnapshotFallsBackToGitCommitSHA|SnapshotHeadCommitSubprocessCount)'
-count=1`.

No-Observability-Change: the `logGitSyncCompleted` structured log, the git-sync
operation span, and the snapshot stage telemetry (`collector snapshot stage
completed` logs, `eshu_dp_collector_snapshot_stage_duration_seconds`) already
diagnose the sync-and-snapshot path. No new metrics, spans, logs, or status
fields were added; the `HeadCommitSHA` snapshot field is unchanged.
### Eliminate double file read via shared pre-prime (#4851)

Performance Evidence: For a 5000-file repo, the `parseRepositoryFile` path
  previously issued 2N physical reads per snapshot call: one inside
  `Engine.ParsePath` (which primes the shared single-read cache from disk) and
  one explicit `os.ReadFile` in the collector for `shapeFileFromParsed`. The
  fix reads the body once upfront, calls `shared.PrimeSource(absPath, body)` to
  pre-seed the parser's shared single-read cache, defers `shared.ClearSource`,
  and calls `engine.ParsePath` — which internally finds the primed entry via
  `shared.ReadSource` and performs zero additional physical disk reads. The
  same body is reused for `shapeFileFromParsed`. Total physical reads per file
  drops from 2 to 1. The read-count proof
  (`TestParseRepositoryFilePrePrimeEliminatesDoubleRead`) counts
  `shared.ReadSource` physical hits via `SetReadSourceHookForTest`: 0 hits
  during the pre-primed `ParsePath`, versus 1 without the pre-prime. The
  cache-release proof (`TestPrePrimeCacheIsReleasedAfterParse`) confirms the
  collector's `ClearSource` balances correctly: after the parse, a subsequent
  `shared.ReadSource` triggers a fresh physical read (hook fires). The
  concurrency proof
  (`TestParseRepositoryFilesConcurrentPrePrimeRaceFree`, `-race`, 32 files,
  8 workers) exercises `buildParsedRepositoryFilesConcurrent` — no data races,
  all files parsed without skips, correct per-file output. The
  `TestPartitionedConcurrentParseMatchesSequentialComposition` regression test
  stays green (byte-identity proof: concurrent = sequential output). The
  shape-body proof (`TestShapeBodyMatchesFileSystemAfterPrePrime`) confirms the
  reused body matches a fresh `os.ReadFile`.
No-Observability-Change: No new metric instrument, label, span, structured
  log field, status field, queue domain, worker count, batch size, or runtime
  knob is added. Operators still diagnose parse behavior through the existing
  `eshu_dp_file_parse_duration_seconds` histogram and `collector snapshot stage
  completed` logs. The change primes the parser's existing shared cache from
  the collector side; no parser source files change, and parse payloads, shape
  files, and SCIP merges are byte-identical.

### Build value-flow entity-lookup maps once instead of 5× (#4879)

Performance Evidence: The five value-flow builders previously rebuilt two
  lookup structures from the same `materialization.Entities` slice per
  snapshot: an entity-lookup map (path, type, name, line → uid) built three
  times and a function-UID resolver (path, receiver, name → uid) built twice.
  The change builds each structure once in the value-flow stage and passes
  them as read-only parameters. On a fixture of 10 entities across 3 files,
  the build-count drops from 5 to 2 (3 fewer internal builds per value-flow
  stage). Benchmarking `buildEntityUIDLookup` + `newFunctionUIDResolver` shows
  1,404 ns/op + 2,256 B/31 allocs at n=10, 12,939 ns/op + 20,003 B/218 allocs
  at n=100, 155,244 ns/op + 315,743 B/2,931 allocs at n=1,000, and 1,385,300
  ns/op + 2,721,751 B/30,018 allocs at n=10,000 (Apple M5 Max). Formerly those
  costs were paid up to 5×; now they are paid once. Correctness proof:
  `TestValueFlowFullOutputEquivalence` produces byte-identical parsed-files
  annotations, taint evidence, interproc taint evidence, function summaries,
  and dataflow functions whether the shared maps or per-builder independent
  builds are used. UID resolution is identical under both paths
  (`TestSharedEntityUIDLookupEqualsPerBuilder`,
  `TestSharedFunctionUIDResolverEqualsPerBuilder`). No-mutation proof:
  `TestEntityUIDLookupIsNotMutated` and `TestFunctionUIDResolverIsNotMutated`
  confirm the shared structures are unchanged after all consumers run.
  Benchmark: `BenchmarkValueFlowLookupBuild` with `-race` clean:
  `go test ./internal/collector -bench 'BenchmarkValueFlowLookupBuild' -benchtime=1s -count=1`.
No-Observability-Change: The same `collector snapshot stage completed` log
  and `eshu_dp_collector_snapshot_stage_duration_seconds` histogram diagnose
  the value-flow stage. No new metric instrument, label, span, log field,
  status field, queue domain, worker count, batch size, or runtime knob is
  added. The five builders return byte-identical output; snapshot shape is
  unchanged.
### Derive FactCount from the emitted stream to drop the pre-stream body re-read pass (#4877)

Performance Evidence: Eliminates three pre-stream body-re-reading count passes
  — `serviceCatalogFactCount` (reads service-catalog file bodies via
  `os.ReadFile`), `gitDocumentationFactCount` (reads documentation/API-contract
  candidate bodies via `os.Open`/`io.ReadAll`), and
  `workflowImageEvidenceFactCount` (reads workflow bodies via `os.ReadFile`).
  Before this change, a repository with service-catalog, documentation, and
  workflow files incurred 2× body reads per candidate file: once in the
  pre-stream count pass and once in the streaming emit pass. After this change,
  each body is read exactly once (at emit time). The pre-stream `FactCount`
  estimate now uses metadata-only counts (file data, content entities,
  tombstones, follow-ups) plus an `*atomic.Int64` counter that `streamFacts`
  increments per emitted envelope. After the Facts channel drains,
  `CollectedGeneration.FactCount()` returns the exact streamed count. On a
  fixture exercising all three body-reading categories, pre-change body read
  count was 2× (count + emit), post-change is 1× (emit only). The functions
  `serviceCatalogFactCount`, `gitDocumentationFactCount`,
  `workflowImageEvidenceFactCount`, `serviceCatalogFactCountForFile`,
  `workflowImageEvidenceFactCountForFile`, `readDocumentationCandidateBody`,
  and `isDocumentationPathOrStructuredAPIContractCandidate` are removed as
  unused.
No-Observability-Change: The `fact_count` structured log attribute (service.go)
  and `eshu_dp_workflow_claim_facts_emitted_total` metric
  (claimed_service_run_metrics.go) are unchanged in meaning — they now reflect
  an exact streamed count rather than a pre-stream estimate, which is strictly
  more accurate. The `eshu_dp_workflow_claim_facts_emitted_total` metric
  description in `instruments.go` already documents it as an "estimated total";
  the exact count is at least as good. The `FactsEmitted` and `FactsCommitted`
  counters in `service.go` and bootstrap use `FactCount()` which after drain
  returns the exact total.
