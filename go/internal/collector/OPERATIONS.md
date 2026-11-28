# Collector Operational Notes

This file carries collector runtime knobs, discovery invariants, and
operator-facing telemetry notes that are too detailed for the package README.

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

## Documentation-only extraction lane

A documentation-only lane normalizes repo-hosted Markdown, lightweight text,
HTML, API contracts, notebooks, spreadsheets, DOCX/XLSX/PPTX summaries,
bounded ZIP/TAR packets, and deterministic diagrams into source-neutral facts
with repository target refs. Deterministic diagram document and section
facts carry `incident_media_source_class=diagram_label` so later correlation
work can preserve the media evidence boundary.

Office annotations and hidden content stay metadata-only while visible
content still emits facts. External relationships, embedded objects, macro
content, malformed containers, unsafe paths, resource limits, and
compression hazards block Office extraction; legacy `.xls` cell bytes stay
metadata-only. Archive packets preflight first, preserve member path/hash
provenance, skip unsupported/nested/credential-like members, and block
unsafe or resource-hazard archives from emitting contained sections.

Default-off helper packages may build OCR or media transcript documentation
facts from reviewed local engine output after preflight, but those helpers
do not enable repository media discovery, hosted runtime paths, or truth
promotion. These claims remain document evidence only; projector, reducer,
and query stages own correlation, drift, and truth decisions.

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
  `index.scip` fall back to native parser output and log language, reason, and
  failure class.
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

## Per-collector run telemetry

`ClaimedService.processClaimed` in `claimed_service.go` is the single chokepoint
where every collector's claimed work begins and ends. Two new instruments are
recorded there:

**`eshu_dp_workflow_claim_run_duration_seconds`** (Float64Histogram)
- Labels: `collector_kind` (bounded `scope.CollectorKind` constant, e.g. `git`,
  `terraform_state`, `discovery`), `source_system` (e.g. `git`), `outcome`
  (bounded: `success`, `refused`, `unchanged`, `released`, `fail_retryable`,
  `fail_terminal`). `refused` means the claim completed but the committer stored
  nothing (#10037).
- Recorded via `defer` at the top of `processClaimed` so every return path
  (success, refused, unchanged, released, retryable fail, terminal fail) emits a
  data point.
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
