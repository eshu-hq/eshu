# AGENTS.md - internal/collector

`internal/collector` owns repository selection, repository snapshotting,
discovery, parser input staging, fact streaming, and collector telemetry. It
does not own graph truth, reducer truth, or query-time interpretation.

## Read First

1. `README.md` - pipeline position, lifecycle, exported surface, and telemetry.
2. `service.go` - `Service.Run`, `commitWithTelemetry`, the poll loop, and
   `AfterBatchDrained` timing.
3. `git_source.go` - `GitSource.startStream`, two-lane scheduling, and the
   large-repo semaphore lifecycle.
4. `git_snapshot_native.go` - snapshot stages and two-phase memory design.
5. `git_selection_config.go` - `RepoSyncConfig`, env vars, and defaults.
6. `go/internal/telemetry/instruments.go` and `contract.go` before changing
   metrics, spans, or log keys.
7. `packageregistry/README.md` before changing package/feed collection.

## Mandatory Invariants

- `ContentFileMeta` MUST NOT carry file body strings. `streamFacts` re-reads
  bodies from disk at emit time, so buffered generation memory stays `O(1)`.
- `RepositorySnapshot` MUST NOT retain materialized bodies after snapshot
  emission. Keep `shapeFiles = nil` and `materialization = content.Materialization{}`
  after content materialization.
- `resolveRepositories` MUST convert repo paths to absolute paths before
  computing `sourceRunID`.
- The large-repo semaphore MUST be acquired in the worker select loop, not
  inside `processRepo`, so large repos cannot starve small repos.
- Repo-local discovery overrides MUST apply before operator-level overlays.
- Filesystem fingerprints MUST include rule files and skip files excluded by
  `.gitignore`, `.eshuignore`, or `.eshu/discovery.json`.
- Collection is best-effort. Callers MUST handle `partial-snapshot` and
  `discovery-skip` outcomes explicitly.
- Collector observe spans MUST cover both source read and durable commit work.
- `factStreamBuffer` MUST stay aligned with the Postgres ingestion batch size.

## Change Routing

- New repository source mode: add a new `RepositorySelector`, wire
  `git_selection_*`, add env config and tests, and avoid branching inside
  `GitSource` on source mode.
- New snapshot stage: add the stage in `SnapshotRepository`, record stage
  timing, add duration metrics when useful, and add snapshot tests.
- Large-repo default change: edit `git_selection_config.go`, update tuning
  comments with dated production evidence, add tests, and review
  `eshu_dp_large_repo_semaphore_wait_seconds`.
- Discovery advisory field: update `DiscoveryAdvisoryReport`, populate it in
  `buildDiscoveryAdvisoryReport`, and add discovery tests.
- Package-registry support: keep normalization and fact envelopes in
  `packageregistry`; do not materialize package graph truth in collector code.
- New collector family or scanner: add package-local `doc.go`, `README.md`, and
  `AGENTS.md`; run package-doc verification; add performance and observability
  evidence if it adds workers, leases, fanout, batching, queues, or downstream
  graph pressure.

## Debug Signals

- Rising `eshu_dp_repos_snapshotted_total{status="failed"}`: inspect snapshot
  stage logs, workspace disk, and git credentials.
- Rising `eshu_dp_large_repo_semaphore_wait_seconds`: inspect large-repo slots,
  memory pressure, and `eshu_dp_gomemlimit_bytes` before raising concurrency.
- `eshu_dp_facts_committed_total` lagging emitted facts: inspect Postgres query
  duration and connection-pool pressure.
- Registry `failure_class` values MUST stay bounded to the documented registry
  transport/auth classes. Do not expose hosts, repos, tags, account IDs, paths,
  or credential references in metric labels or status messages.
- `stream_snapshot_failure`: inspect the first failing repo path and error.
- Empty `RepoFileSet.Files`: run `eshu index --discovery-report` and inspect
  advisory skip breakdown.
- Local watch mode superseding unchanged repos: compare `fingerprintTree` with
  `shouldSkipFilesystemEntry` before changing worker counts.

## Anti-Patterns

- Do not store body strings in `ContentFileMeta` or `RepositorySnapshot`.
- Do not call `filepath.Rel` on `RepoFileSet.Files` at collector level; those
  paths are absolute by contract.
- Do not import graph, query, projector, reducer, or storage/cypher packages.
- Do not block while holding the large-repo semaphore after snapshot completion.
- Do not materialize graph ownership or runtime truth from collector evidence.
- Do not add high-cardinality metric labels for paths, repos, packages,
  accounts, ARNs, tags, or credentials.

## Do Not Change Without A Design Record

- Two-lane small/large repository scheduling.
- `factStreamBuffer` without the matching Postgres ingestion batch size.
- `AfterBatchDrained` call semantics for backfill and deployment-map reopen.
- The collector boundary that emits facts but does not project graph truth.

## Required Proof

- Run focused tests for the changed selector, snapshot stage, advisory, scanner,
  or registry path.
- Run `go test ./internal/collector -count=1`.
- Run `go run ./cmd/eshu docs verify ../go/internal/collector --limit 1200 --fail-on contradicted,missing_evidence`
  for docs changes.
- Worker, queue, fanout, batching, or runtime-pressure changes require tracked
  performance and observability evidence.
