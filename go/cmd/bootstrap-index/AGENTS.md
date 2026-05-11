# bootstrap-index — Agent Instructions

This file is the LLM-assistant companion to `README.md`. Read this before
touching any file in `go/cmd/bootstrap-index/`.

## Read first

- `go/cmd/bootstrap-index/main.go` — the four-phase orchestrator; the phase
  ordering is a correctness invariant, not a style choice.
- `go/cmd/bootstrap-index/wiring.go` — collector and projector wiring.
- `go/cmd/bootstrap-index/nornicdb_wiring.go` — NornicDB-specific executor
  chain (phase-group chunking, timeout, instrumentation, retry).
- `CLAUDE.md` section "Facts-First Bootstrap Ordering" — describes the four
  phases in prose; `main.go` is the implementation.
- `go/internal/storage/postgres/ingestion.go` — owns `SkipRelationshipBackfill`,
  `BackfillAllRelationshipEvidence`, `ReopenDeploymentMappingWorkItems`, and
  `MaterializeIaCReachability` (the `bootstrapCommitter` methods).
- `go/internal/storage/postgres/drift_enqueue.go` — owns
  `EnqueueConfigStateDriftIntents` (the Phase 3.5 trigger added for chunk
  #163).

## Phase-ordering invariant

The six pipeline steps in `runPipelined` (`main.go:213`) must execute in
order:

1. `drainCollector` + `drainProjectorPipelined` run concurrently.
   `BackfillAllRelationshipEvidence` is called after `drainCollector` returns,
   before the projector goroutine drains.
2. `cd.committer.BackfillAllRelationshipEvidence` (`main.go:259`) populates
   `relationship_evidence_facts` and publishes `backward_evidence_committed`.
3. `projectorErr := <-errc` waits for `drainProjectorPipelined` to exit before
   the reopen call. This prevents `deployment_mapping` items emitted after
   the reopen pass from missing reopening.
4. `cd.committer.MaterializeIaCReachability` runs after projector drain.
5. `cd.committer.ReopenDeploymentMappingWorkItems` runs after IaC reachability.
6. `cd.committer.EnqueueConfigStateDriftIntents` runs last (Phase 3.5 trigger
   for the config_state_drift domain; depends on Phase 3 reopen completing
   first).

**Do not reorder or merge these calls.** Swapping Phase 2 and Phase 3 or
calling `ReopenDeploymentMappingWorkItems` before the projector drains creates
E2E-only bugs: deployment-mapping items that succeed before relationship
evidence exists produce incomplete graph truth.

## Common changes

### Add a new post-collection pass

1. Add the method to `bootstrapCommitter` (`main.go:43`) alongside existing
   methods such as `BackfillAllRelationshipEvidence`.
2. Implement it on `postgres.IngestionStore` (own the logic there, not here).
3. Add the call in `runPipelined` after `projectorErr := <-errc`, using the
   same fatal-error + `FailureClassAttr` pattern as existing calls.
4. Add a failure-class constant in `go/internal/telemetry/contract.go`.
5. Write a test in `main_test.go` proving the ordering: the new pass must not
   run before the projector drains.

### Add a domain that consumes `resolved_relationships`

If the new domain depends on `resolved_relationships`, it needs a reopen or
re-trigger mechanism after Phase 4. Add it to `ReopenDeploymentMappingWorkItems`
or create a new method on `bootstrapCommitter` and wire it after
`ReopenDeploymentMappingWorkItems`.

### Change NornicDB batch sizes or phase-group tuning

All NornicDB knobs are in `nornicdb_wiring.go`. Add or change a constant in the
`const` block, read the env var via `nornicDBPositiveIntEnv`, and pass the value
through `bootstrapNornicDBPhaseGroupExecutor`. Update
`docs/docs/reference/nornicdb-tuning.md` and the active NornicDB ADR in the
same PR.

### Change projection worker count behavior

`projectionWorkerCount` (`main.go:412`) reads `ESHU_PROJECTION_WORKERS` and
defaults to `min(NumCPU, 8)`. If you change the cap or the default, update the
concurrency reference table in `docs/docs/reference/local-testing.md` and
`docs/docs/deployment/service-runtimes.md`.

## Failure modes

| Failure | Symptom | Check |
| --- | --- | --- |
| Phase 2 backfill stalls | Binary hangs after collection completes | OTEL traces for `BackfillAllRelationshipEvidence`; check `go/internal/storage/postgres/ingestion.go` for the SQL path |
| Projector drain never exits | Binary hangs after Phase 2 | `drainingWorkSource.Claim` at `main.go:377` wraps `ProjectorWorkSource`; confirm `collectorDone` is closed; check `maxEmptyPolls` logic |
| Phase 4 reopen skips stragglers | Reducer finds no `deployment_mapping` to process after bootstrap | Expected for items that succeeded in the Phase 2→4 window; use `/admin/replay` |
| NornicDB timeout on graph write | `ESHU_CANONICAL_WRITE_TIMEOUT` exceeded | Lower `ESHU_NORNICDB_ENTITY_BATCH_SIZE` or `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS`; check `go/cmd/bootstrap-index/nornicdb_wiring.go` defaults |
| Heartbeat failure | `lease_heartbeat_failure` log + worker exits | Check `bootstrapIndexConnectionTimeout` and Postgres connectivity; heartbeat interval is `leaseDuration/3` capped at 1 minute |
| Superseded projector work | `status=superseded` log and worker continues | Expected when `ProjectorWorkHeartbeater` returns `projector.ErrWorkSuperseded`; do not ack or fail the stale generation |

## Anti-patterns

- **Do not skip `SkipRelationshipBackfill=true`.** Without it, every
  `CommitScopeGeneration` call runs a full per-repo backfill. On 800+ repos
  this is quadratically expensive and defeats the deferred-backfill design.
- **Do not call `ReopenDeploymentMappingWorkItems` before the projector drains.**
  The comment at `main.go:285` explains why; `MaterializeIaCReachability` must
  also not run before the drain. Any refactor that merges or reorders these
  calls requires re-reading the ADR at
  `docs/docs/adrs/2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`.
- **Do not add signal handling without also adding a cleanup path for all
  phases.** The binary currently has no signal handlers by design (one-shot).
  If you add `SIGTERM` handling, you must decide what partial-phase state means
  for correctness and document it.
- **Do not enable `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true` in production
  without running the conformance gate** (the grouped-write safety probe and
  rollback conformance tests). See `CLAUDE.md` section
  "NornicDB Compatibility Workflow".
- **Do not treat `errProjectorDrained` as an error.** It is a sentinel
  (`main.go:657`) emitted after the `PhaseProjection` drain loop exhausts the
  queue. Worker goroutines return on it; do not propagate it through error
  channels.
- **Do not treat `projector.ErrWorkSuperseded` as a bootstrap failure.** The
  queue has already moved the stale generation out of the live backlog. The
  worker must return to the claim loop so the newer generation can run.

## What NOT to change without an ADR

- The four-phase ordering in `runPipelined`.
- The `bootstrapCommitter` interface — adding a method changes the contract with
  `postgres.IngestionStore` and the ingester's deferred-maintenance path.
- The `SkipRelationshipBackfill` flag default on `postgres.IngestionStore`.
- NornicDB grouped-writes default (`false`).

## Verification gates

```bash
cd go && go test ./cmd/bootstrap-index -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./internal/storage/postgres -count=1
cd go && golangci-lint run ./cmd/bootstrap-index/...
```

For docs-only changes:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
