# Collector Operational Notes

This file carries collector runtime knobs, performance evidence, and
operator-facing observability notes that are too detailed for the package
README.

## Runtime knobs

- `ESHU_SNAPSHOT_WORKERS` (default `min(NumCPU,8)`) controls concurrent
  per-repo snapshotting. Raising this value beyond CPU capacity increases
  context-switching without reducing wall time.
- `ESHU_PARSE_WORKERS` partitions parser-supported files by stable repository
  subtree before concurrent native parsing. Partitions are balanced by total
  on-disk bytes, not file count, so a subtree dominated by a few heavy files
  does not pin one parse worker. Subtrees heavier than one worker's byte target
  are split into deterministic byte-balanced chunks; lighter subtrees stay whole
  so a single large monorepo can keep multiple parse workers busy while the
  composed snapshot is sorted back to the original file order. See
  `SCHEDULING.md` for the parse-partition byte-balancing evidence.
- `ESHU_REPO_SHARD_COUNT` and `ESHU_REPO_SHARD_INDEX` deterministically filter
  discovered repository IDs before filesystem or Git sync begins. The shard hash
  uses only the normalized repository ID; shard IDs are not part of repository,
  file, entity, or fact identity. The existing `sourceRunID` still reflects the
  selected batch for that process. Helm does not enable horizontal ingester
  replicas until the global deferred-maintenance hook has a fleet-wide drain
  barrier.
- `ESHU_LARGE_REPO_FILE_THRESHOLD` (default `1000`) classifies repositories for
  the large-repo semaphore. The classification is a fast pre-scan that exits
  early once the threshold is exceeded.
- `SCIP_WORKERS` (default `4`) bounds concurrent SCIP language/subtree indexer
  processes across the ingester snapshotter, including concurrent repository
  snapshots. Set it to `1` only for memory-constrained hosts; keep it aligned
  with CPU and memory headroom because each slot may run a compiler-grade
  indexer.
- Repo-local `.eshu/discovery.json` and `.eshu/vendor-roots.json` override
  default discovery options before the operator-level
  `ESHU_DISCOVERY_IGNORED_PATH_GLOBS` overlay is applied.
- `ClaimedService.MaxAttempts` is the bounded retry budget per work item. Wire
  per-collector commands to `workflow.DefaultClaimMaxAttempts()` unless a
  deployment-specific override is required.

## Discovery and streaming

- Default discovery prunes generated dependency/cache directories by precise
  names such as `node_modules`, `vendor`, `.gradle`, and `.m2`, but it does not
  prune a generic `packages` directory. npm, pnpm, Yarn, and many polyglot
  monorepos use `packages/<workspace>` for authored source, manifests, and
  lockfiles. Generated package caches under that name need repo-local
  `.eshuignore`, `.eshu/discovery.json`, or operator ignored-path globs so the
  exclusion is visible in discovery stats.
- Legacy vendored-library pruning is intentionally signature- or path-family
  specific. It skips known third-party libraries such as Zend, PEAR, FPDF,
  Plupload, Aurigma uploaders, phpCAS, Minify, FusionCharts, Scriptaculous, and
  legacy Microsoft map-control assets while preserving authored files with
  similar names. Do not broaden this to generic `framework`, `library`, `tests`,
  or `public` roots without repository-scale proof that authored source is not
  lost.
- Filesystem manifest fingerprints include `.gitignore` and `.eshuignore` rule
  files but exclude paths filtered by those rules. Changing an ignore rule
  reselects the repository; changing only ignored output does not.
- Two-phase streaming: `ContentFileMeta` carries no body; `streamFacts`
  re-reads file bodies from disk at emit time. The OS page cache keeps re-reads
  fast. Do not change this design to in-memory bodies without accounting for
  `O(repo_size)` memory growth on large repositories.
- Repo-hosted service-catalog manifests are detected by exact descriptor
  filename (`catalog-info.yaml`/`.yml`, `opslevel.yml`/`.yaml`,
  `cortex.yaml`/`.yml`) during the same content streaming pass. Ordinary YAML
  files and Cortex scorecard descriptors stay ordinary content until a
  dedicated runtime slice opens that contract.

## Evidence

- Performance Evidence: On 2026-07-02, a collector-discovered remote profile of
  one large legacy PHP/JavaScript repository on `codex/4515-prescan-parse-lanes`
  with `ESHU_PARSE_WORKERS=16` and NornicDB PR #230 bits showed parser input
  drop from 5,953 to 5,661 files and parser wall time drop from 4.077s to
  2.638s after the vendored-library filters were added. A bounded production
  profile with the same `GOMAXPROCS=16`, parse/snapshot worker settings, and
  graph backend showed the heavy repository parse stage drop from 96.332s to
  82.497s, and the first 85 parse samples drop from 947.914s total to 868.837s
  total. The same run showed pre-scan remained the next bottleneck; this change
  is a parse/input-shape win, not end-to-end closure.
- No-Observability-Change: The vendored-library filters only add discovery skip
  reasons under the existing `FilesSkippedByContent` stats and
  `eshu_dp_discovery_files_skipped_total` metric. They add no worker, queue,
  graph write, span, metric name, runtime knob, or status field.
- No-Regression Evidence: `AfterEmptyBatchDrained` behavior is covered by
  `go test ./internal/collector -run
  'TestServiceRun(CallsAfterBatchDrainedOnceAfterCommittedBatch|SkipsAfterBatchDrainedOnEmptyBatchByDefault|CallsAfterBatchDrainedForConfiguredEmptyBatch|CallsEmptyBatchDrainHookOnceWhileIdle)'
  -count=1`, which proves the default hook remains commit-gated and the
  empty-batch hook is opt-in and not an idle timer.
- No-Observability-Change: `AfterEmptyBatchDrained` only changes whether the
  caller-supplied drain hook runs once for an exhausted empty batch. It adds no
  metric, span, status field, worker, queue, graph write, or runtime label.
- No-Regression Evidence: `go test ./internal/collector ./internal/doctruth ./internal/query ./internal/mcp ./internal/storage/postgres -count=1` covers DOCX, CSV/TSV, XLSX, PPTX, ZIP packet summaries, deterministic diagrams, claim hints, repository fact readback, and MCP routing.
- No-Observability-Change: documentation extraction stays inside existing `collector.observe`, body-free snapshot metadata, and stream-time re-reads. It adds no worker, queue, graph write, metric label, runtime knob, or deployment profile.
- No-Regression Evidence: delta generation handling is covered by `go test ./internal/collector -run
  'Test(NativeRepositorySnapshotterCarriesDeletedOnlyDeltaMetadata|NativeRepositorySnapshotterDeltaTargetsKeepFullPreScanContext|NativeRepositorySnapshotterPreservesDeltaMetadataPathWhitespace|UpdateRepositoryReturnsChangedAndDeletedFileTargets|BuildSelectedRepositoriesCarriesGitDeltaFileTargets|BuildStreamingGenerationEmitsDeltaMetadataAndDeletedTombstones|BuildStreamingGenerationPreservesDeltaPathWhitespace|BuildStreamingGenerationDeltaChangedFileFactsMatchFullSnapshot|BuildStreamingGenerationSkipsRepoWideReducerFollowupsForDelta)'
  -count=1`, which proves Git delta parsing, selector propagation, deleted-only
  snapshot metadata, full-context pre-scan for targeted deltas, symlink-normalized
  path metadata with legal whitespace preserved, tombstone emission, changed-file
  fact payload parity against full snapshots, fact count agreement, and
  suppression of unsafe repo-wide reducer follow-ups for delta generations.
- Performance Evidence: `go test ./internal/collector -run '^$' -bench
  'BenchmarkNativeRepositorySnapshotter(FullFixture|DeltaSingleFileFixture)$'
  -benchtime=1x -count=1` on an Apple M4 Pro measured a generated 400-file
  fixture full snapshot at `107796250 ns/op` and a one-file delta snapshot at
  `34240667 ns/op`.
- No-Observability-Change: delta parsing reuses hosted git sync logs, snapshot
  stage logs, `collector.observe`, fact emission counts, and projector/reducer
  queues. It adds no metric name or label and does not log file paths in sync
  progress messages.
- No-Regression Evidence: `go test ./internal/collector -run
  'Test(BuildDataflowFunctionsReadsParserBucket|DataflowFunctionFactEmittedAndCounted)'
  -count=1` covers the dataflow function fact-mapping path. The baseline input is
  the existing gate-off/no-`dataflow_functions` snapshot shape; the after input is
  one parser-emitted function row with one CFG block and one def-use edge. The
  terminal row delta is exactly one `code_dataflow_function` fact, and
  `FactCount == len(envelopes)` remains true. Backend/version: Postgres
  `fact_records` receives one additional active-generation fact per parser row;
  no graph write, queue, worker, or provider call is added.
- No-Observability-Change: the dataflow function mapping is covered by existing
  `eshu_dp_collector_snapshot_stage_duration_seconds`, `eshu_dp_facts_emitted_total`,
  and `eshu_dp_generation_fact_count` signals. The `dataflow_function_count`
  snapshot log attribute lets operators correlate row volume with fact-count
  changes without adding a new metric or label.
- No-Regression Evidence: `go test ./internal/collector -run 'FunctionSummary|FunctionSource' -count=1`,
  `go test ./internal/storage/postgres -run 'FunctionSource' -count=1`, and
  `go test ./internal/reducer -run 'CodeFunctionSummary' -count=1` prove
  `buildFunctionSummaries`/`buildFunctionSources` read the `dataflow_summaries` and
  `dataflow_sources` buckets into per-function snapshots; that `streamFacts` emits
  one `code_function_summary` fact per function and one `code_function_source` fact
  per source, counted in `FactCount`, keyed idempotently; that the function-summary
  reducer handler persists the summaries (and, when wired, the sources) to the
  durable Postgres stores; and that the new `function_sources` bootstrap schema is
  ordered and mirrored on disk. It is one extra fact per summarized function/source
  only when the off-by-default value-flow gate is on; no new Cypher, graph write,
  worker, queue, or batch. The `contentFactEnvelope`/`contentEntityFactEnvelope`
  move into `git_content_fact_envelopes.go` is a pure extraction (no behavior
  change) to keep `git_fact_builder.go` under the file-size cap.
- No-Observability-Change: the `code_function_summary`/`code_function_source`
  facts flow through the existing `streamFacts` channel and Postgres fact
  persistence; they add no metric instrument, metric label, span, worker, queue
  domain, lease, runtime knob, or log key. Operators diagnose the path through
  the existing fact-stream counters.
- No-Regression Evidence: `go test ./internal/collector -run 'DataflowScanned' -count=1`
  and `go test ./internal/projector -run 'Marker|QueuesBoth' -count=1` prove the
  `code_dataflow_scanned` marker is emitted (and counted in `FactCount`) only when
  `DataflowScanned` is set, is absent when the gate is off, and that the projector
  queues both the `code_taint_evidence` and `code_interproc_evidence` retraction
  intents from the marker alone. The marker is one extra fact per generation only
  when the off-by-default gate is on; no new Cypher, graph write, worker, queue, or
  batch.
- No-Observability-Change: the `code_dataflow_scanned` marker flows through the
  existing `streamFacts` channel and Postgres fact persistence; it adds no metric
  instrument, metric label, span, worker, queue domain, lease, runtime knob, or log
  key. Operators diagnose it through the existing fact-stream counters and the
  reducer claim/execute spans for the value-flow evidence domains.

- No-Regression Evidence: nested npm workspace package manifests and lockfiles
  under `packages/<workspace>` remain discoverable by default. The focused gate
  is
  `go test ./internal/collector -run TestResolveNativeSnapshotFileSetKeepsNestedNPMWorkspaceManifests -count=1`,
  which proves root and nested `package.json` / `package-lock.json` files land
  in discovery while `packages` is not counted as a pruned directory.
- No-Observability-Change: keeping authored `packages/<workspace>` trees in
  discovery uses the existing discovery stats, `collector snapshot stage
  completed` logs, `collector.observe`, `collector.stream`,
  `eshu_dp_repos_snapshotted_total`, `eshu_dp_file_parse_duration_seconds`,
  and generation/fact counters. It adds no new runtime, worker, queue, graph
  write, span, metric label, or status field.
- Performance Evidence: On 2026-05-15, pprof from the remote full-corpus
  Compose run showed bootstrap startup CPU in filesystem repository copy and
  ignore matching before graph projection began. A focused local benchmark for
  literal ignore patterns improved from 2.35-2.44 us/op, 656 B/op, and 10
  allocs/op at `4d31617` to 1.11-1.13 us/op, 96 B/op, and 1 alloc/op after
  routing non-glob `.gitignore` and `.eshuignore` rules through literal
  matching.
- Performance Evidence: baseline unset `SCIP_INDEXER` could enter the shared
  SCIP process limiter and launch an external indexer when an allowed language
  and matching `scip-*` binary were present. After this slice, unset
  `SCIP_INDEXER` returns `Enabled=false`, records one
  `eshu_dp_scip_snapshot_attempts_total{language="unknown",result="disabled"}`
  attempt, and returns to native parser workers without binary lookup, process
  wait, indexer execution, protobuf parsing, queue work, graph writes, or extra
  rows. Backend/version: local Go test runtime with fixture Python and mixed
  Python/Go repositories, no Postgres/NornicDB/Neo4j/provider required. After
  measurement: `go test ./internal/collector -run
  'Test(LoadSnapshotSCIPConfig|SCIPSnapshot|SCIPLanguage|SCIPWorkers)'
  -count=1` passed and proves default-off, explicit-on, binary fallback,
  subtree fan-out, missing-index fallback, and worker-limit behavior.
- No-Observability-Change: the SCIP default-off gate adds no metric name, metric
  label, span, status field, log field, worker, queue, graph write, runtime
  endpoint, deployment profile, or provider configuration.
- No-Regression Evidence: SCIP subtree worker fan-out preserves native fallback
  and SCIP supplement behavior. `go test ./internal/collector -run
  'Test(LoadSnapshotSCIPConfigParsesWorkers|SCIPLanguageSubtreesRunWithBoundedWorkers|SCIPWorkersCapConcurrentSnapshots|SCIPWorkersRecordLimiterWaitDuration|SCIPSnapshotRuns|SCIPSnapshotSameLanguage|SCIPSnapshotLanguageSubtree|SCIPSnapshotConcurrentParseMergesSCIPSupplement|SCIPSnapshotFallback)'
  -count=1` proves the env contract, bounded concurrent subtree execution,
  cross-snapshot process limiting, limiter wait telemetry, and existing fallback
  semantics.
- Performance Evidence: focused local SCIP worker benchmark command:
  `go test ./internal/collector -run '^$' -bench BenchmarkSCIPLanguageSubtreeWorkers -benchtime=1x -benchmem -count=1`.
  On 2026-06-19 on Apple M4 Pro, the four-subtree synthetic SCIP fixture
  measured `workers_1` at 25.367 ms/op, 7.44 KB/op, 85 allocs/op and
  `workers_4` at 6.388 ms/op, 11.56 KB/op, 103 allocs/op. The bounded #2998
  slice keeps SCIP inside the repository snapshot parse stage but removes the
  serial default for language/package-root indexer runs.
- Observability Evidence: SCIP worker fan-out reuses
  `eshu_dp_scip_snapshot_attempts_total{language,result}`, adds
  `eshu_dp_scip_process_wait_seconds{language}` for shared process-slot
  contention, and emits bounded fallback and process-slot logs. It adds no
  repository path, subtree, or process ID metric labels.
- Observability Evidence: The existing `collector snapshot stage completed`
  logs, `SpanScopeAssign`, `SpanCollectorStream`, and pprof profiles expose the
  selector/copy window separately from per-repository discovery, pre-scan,
  parse, materialize, commit, and projection stages.
- Collector Performance Evidence: declared Prometheus/Mimir, Loki, and Tempo
  source facts reuse the existing repository parse and fact-stream pass. The
  focused proof is
  `go test ./internal/parser/yaml ./internal/parser/hcl ./internal/collector ./internal/facts -count=1`;
  it covers bounded metadata rows for Prometheus Operator resources, Helm
  values, OTel metric and log routes, OTel Prometheus receiver scrape configs,
  Promtail client routes, Loki gateway values, Grafana Loki datasource
  references, OTel trace routes, Tempo gateway values, Grafana Tempo datasource
  links, and Git fact emission without adding provider calls, queue workers,
  graph writes, or reducer stages.
- Collector Observability Evidence: declared Prometheus/Mimir, Loki, and Tempo
  facts use the existing Git collector telemetry listed in the package README:
  `collector.observe`, `collector.stream`, `fact.emit`,
  `eshu_dp_file_parse_duration_seconds`, `eshu_dp_generation_fact_count`,
  `eshu_dp_facts_emitted_total`, and `eshu_dp_facts_committed_total`.
- No-Observability-Change: this slice adds parser buckets and fact mappings
  only. It adds no metrics, spans, logs, status fields, or metric labels.
- Collector Deployment Evidence: no hosted Deployment, Service, ServiceMonitor,
  Helm values, or Docker Compose path changes. Declared
  Prometheus/Mimir/Loki/Tempo extraction runs inside the existing Git
  repository collector and remains separate from future live provider
  collectors.
- No-Regression Evidence: collector generation dead-letter recording is covered
  by
  `go test ./internal/collector -run 'TestServiceRunRecordsGenerationDeadLetterWhenCommitFails|TestServiceRunPropagatesDurableCommitErrors' -count=1`.
- Observability Evidence: commit failures still surface through the existing
  collector commit error path and `collector.observe` span. The Postgres sink
  exposes `/admin/status`, hosted readiness, and
  `eshu_runtime_collector_generation_*` count/age gauges.

## Parser and collector invariants

- Parser variable scope is part of performance and truth. Java defaults to
  module-level variables during native snapshots because dead-code candidates
  and Java call inference do not need every method-local declaration as a
  canonical `Variable` node. Keep JS/TS/Python local-variable coverage intact
  unless their query contracts change.
- SCIP indexing is opt-in for `python,typescript,javascript,go,rust,java,cpp,c`.
  Set `SCIP_INDEXER=1`, `true`, `yes`, or `on` to enable it when the matching
  `scip-*` binary is available, with `SCIP_WORKERS=4` for bounded
  language/subtree fan-out across concurrent repository snapshots. Unset,
  unrecognized, `false`, `0`, `no`, and `off` values keep native-only parsing.
  Set `SCIP_LANGUAGES` to narrow the SCIP language, or set `SCIP_WORKERS=1` for
  memory-constrained serial fallback.
  Missing binaries, indexer/parser failures, or selected files absent from
  `index.scip` fall back to native parser output. No-Regression Evidence:
  `TestSCIPSnapshotKeepsSelectedFilesMissingFromIndex`.
  Observability Evidence: bounded SCIP fallback logs name language, reason, and
  failure class; parse logs, metrics, and fact counters diagnose fallback.
- Terraform-state ingestion currently uses explicit sources and Git-observed
  backend facts. The #140 target design adds repo-local `.tfstate` candidates
  as advisory metadata, but those candidates must not route raw state through
  Git content persistence or parse state as normal repository content.
- Terraform-state claim processing records `eshu_dp_tfstate_claim_wait_seconds`
  and uses `tfstate.collector.claim.process` around the claimed work boundary.
- `AfterBatchDrained` is a batch boundary hook, not a timer callback. Use it for
  work that should follow committed collection, and keep idle-poll behavior in
  `Source.Next` or the coordinator layer.
- Unclaimed collector services should wire `DeadLetters` when their commit path
  can fail before projector work items exist. Replay is source-level after the
  operator fixes the commit failure; dead-letter metadata cannot reconstruct a
  consumed fact stream. Successful later commits mark unresolved rows
  `replayed`; claim-driven services still use workflow claims for requeue.

## Per-collector run telemetry (issue #3680)

### Observability Evidence

`ClaimedService.processClaimed` in `claimed_service.go` is the single chokepoint
where every collector's claimed work begins and ends. Two new instruments are
recorded there:

**`eshu_dp_workflow_claim_run_duration_seconds`** (Float64Histogram)
- Labels: `collector_kind` (bounded `scope.CollectorKind` constant, e.g. `git`,
  `terraform_state`, `discovery`), `source_system` (e.g. `git`), `outcome`
  (bounded: `success`, `unchanged`, `released`, `fail_retryable`, `fail_terminal`).
- Recorded via `defer` at the top of `processClaimed` so every return path
  (success, unchanged, released, retryable fail, terminal fail) emits a data point.
- **To find the per-collector long pole:** `topk(5, sum by (collector_kind) of
  rate(eshu_dp_workflow_claim_run_duration_seconds_sum[5m]) / sum by
  (collector_kind) of
  rate(eshu_dp_workflow_claim_run_duration_seconds_count[5m]))` gives mean run
  duration per collector family. For a corpus run, `max_over_time` on the
  histogram p95 shows the worst single run.
- **Joins #3678's per-stage metrics:** both use `collector_kind` as the shared
  label so `eshu_dp_workflow_claim_run_duration_seconds` (per-collector wall
  time) and `eshu_dp_bootstrap_pipeline_phase_seconds` (per-phase wall time)
  compose cleanly.

**`eshu_dp_workflow_claim_facts_emitted_total`** (Int64Counter)
- Labels: `collector_kind`, `source_system`.
- Recorded only on the success path, using `CollectedGeneration.FactCount`
  already populated by every collector — no extra IO introduced.
- **To find volume per collector:** `sum by (collector_kind) of
  rate(eshu_dp_workflow_claim_facts_emitted_total[5m])` gives facts/second
  per collector. A collector with high run duration and low fact count is
  spending time on IO, not emission.
- Joins `eshu_dp_content_entity_emitted_total` (from #3678, labeled
  `source_file_kind`) for a per-collector AND per-file-kind volume breakdown.

Both metrics are surfaced on the existing metrics port (no new endpoint).

### No-Regression Evidence

- The timing wrapper (`runStartedAt := s.now()` + `defer recordClaimRunDuration`)
  wraps only existing work already performed by `processClaimed`. No extra IO,
  network, or storage operations are introduced.
- `CollectedGeneration.FactCount` is an integer already populated before the
  seam is reached; `recordClaimFactsEmitted` reads it with a single `int64()`
  cast.
- Concurrency safety: `metric.Float64Histogram.Record` and
  `metric.Int64Counter.Add` are safe for concurrent callers per the OTEL Go SDK
  specification. `runStartedAt` and `runOutcome` are stack-local to each
  `processClaimed` call frame; N concurrent workers never share them.
- No behavior changes: all existing claim, heartbeat, commit, complete, fail,
  and release paths are preserved. The `runOutcome` assignments shadow the
  pessimistic default (`fail_terminal`) with the correct outcome on each arm;
  if a future arm is added without an explicit assignment it falls back to
  `fail_terminal`, which is conservative rather than incorrect.
- Tests verify all five outcome values (success, unchanged, released,
  fail_retryable, fail_terminal) under race detection
  (`go test -race ./internal/collector -run 'ClaimedService|Service|Collector|Metric|Outcome|Released'`):
  81 tests passed, 0 races detected.
