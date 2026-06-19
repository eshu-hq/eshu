# Collector Operational Notes

This file carries collector runtime knobs, performance evidence, and
operator-facing observability notes that are too detailed for the package
README.

## Runtime knobs

- `ESHU_SNAPSHOT_WORKERS` (default `min(NumCPU,8)`) controls concurrent
  per-repo snapshotting. Raising this value beyond CPU capacity increases
  context-switching without reducing wall time.
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
- No-Regression Evidence: SCIP subtree worker fan-out preserves native fallback
  and SCIP supplement behavior. `go test ./internal/collector -run
  'Test(LoadSnapshotSCIPConfigParsesWorkers|SCIPLanguageSubtreesRunWithBoundedWorkers|SCIPWorkersCapConcurrentSnapshots|SCIPSnapshotRuns|SCIPSnapshotSameLanguage|SCIPSnapshotLanguageSubtree|SCIPSnapshotConcurrentParseMergesSCIPSupplement|SCIPSnapshotFallback)'
  -count=1` proves the env contract, bounded concurrent subtree execution,
  cross-snapshot process limiting, and existing fallback semantics.
- Performance Evidence: focused local SCIP worker benchmark command:
  `go test ./internal/collector -run '^$' -bench BenchmarkSCIPLanguageSubtreeWorkers -benchtime=1x -benchmem -count=1`.
  On 2026-06-19 on Apple M4 Pro, the four-subtree synthetic SCIP fixture
  measured `workers_1` at 25.367 ms/op, 7.44 KB/op, 85 allocs/op and
  `workers_4` at 6.388 ms/op, 11.56 KB/op, 103 allocs/op. The bounded #2998
  slice keeps SCIP inside the repository snapshot parse stage but removes the
  serial default for language/package-root indexer runs.
- Observability Evidence: SCIP worker fan-out reuses
  `eshu_dp_scip_snapshot_attempts_total{language,result}` and bounded fallback
  logs. It adds no repository path, subtree, or process ID metric labels.
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
- SCIP indexing defaults on for `python,typescript,javascript,go,rust,java,cpp,c`
  when the matching `scip-*` binary is available, with `SCIP_WORKERS=4` for
  bounded language/subtree fan-out across concurrent repository snapshots. Set
  `SCIP_INDEXER=false`, `0`, `no`, or `off` for native-only parsing, set
  `SCIP_LANGUAGES` to narrow the SCIP language, or set `SCIP_WORKERS=1` for
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
