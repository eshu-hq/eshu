# AGENTS.md - internal/collector

This scoped file is for collector changes only. Use `README.md` and `doc.go`
for the package contract; keep this file focused on rules an agent must not
miss while editing collector code or docs.

## Read First

1. `README.md` and `doc.go` for ownership, exported surface, telemetry, and
   focused tests.
2. `service.go` for `Service.Run`, commits, poll timing, and
   `AfterBatchDrained`.
3. `git_source.go`, `git_snapshot_native.go`, and `git_fact_builder.go` for
   source selection, two-lane scheduling, memory ownership, and fact streaming.
4. `git_selection_config.go` and `discovery_advisory.go` before changing
   source-mode, limits, env config, or advisory output.
5. `go/internal/telemetry/instruments.go` and `contract.go` before changing
   metrics, spans, or structured log keys.
6. `packageregistry/README.md`, `terraformstate/README.md`, or
   `awscloud/README.md` before touching those collector families.

## Mandatory Invariants

- Collectors emit durable source evidence. They do not project graph truth,
  reducer truth, or query interpretation.
- `ContentFileMeta` and `RepositorySnapshot` MUST NOT retain file bodies after
  snapshot emission. `streamFacts` re-reads bodies at emit time to keep buffered
  generation memory bounded.
- `resolveRepositories` MUST convert repo paths to absolute paths before
  computing `sourceRunID`.
- The large-repo semaphore MUST be acquired in the worker select loop and
  released after snapshot completion. Do not serialize collectors to mask
  contention or memory pressure.
- Repo-local discovery overrides apply before operator overlays. Discovery
  skips and partial snapshots are valid outcomes callers must handle.
- Claim-aware commits MUST preserve fencing tokens so stale collectors cannot
  overwrite newer generations.
- `factStreamBuffer` stays aligned with the Postgres ingestion batch size.
- High-cardinality repos, paths, packages, accounts, ARNs, tags, and
  credentials stay out of metric labels.

## Change Routing

- New source mode: add a `RepositorySelector`, config/env tests, and selection
  tests; do not branch source-mode behavior inside `GitSource`.
- Snapshot or discovery changes: update stage timing, advisory/report structs,
  discovery stats, and focused tests together.
- Parser output shape changes: update content shaping, fact contracts,
  reducer/projector consumers, fixtures, and docs in the same slice.
- New collector family or scanner: add package-local `doc.go`, `README.md`,
  and `AGENTS.md`, then run package-doc verification.
- Worker, queue, fanout, batching, or runtime-pressure changes need tracked
  performance and observability evidence before merge.

## Forbidden Without Architecture-Owner Approval

- Two-lane small/large repository scheduling.
- `factStreamBuffer` without the matching Postgres ingestion batch size.
- `AfterBatchDrained` semantics used by backfill and deployment-map reopen.
- The collector boundary that emits facts but never projects graph truth.

## Required Proof

- Run focused tests for the changed selector, snapshot stage, advisory, scanner,
  or registry path.
- Run `cd go && go test ./internal/collector -count=1`.
- For docs-only edits, run
  `go run ./cmd/eshu docs verify ../go/internal/collector --limit 1400 --fail-on contradicted,missing_evidence`
  from `go/`.
- Run `scripts/verify-package-docs.sh` and `git diff --check` from repo root.
