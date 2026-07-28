# bootstrap-index — Agent Instructions

This file is the LLM-assistant companion to `README.md`. Read this before
touching any file in `go/cmd/bootstrap-index/`.

## Read first

- `go/cmd/bootstrap-index/main.go` — `main`, the shared types
  (`bootstrapCommitter`, `collectorDeps`, `projectorDeps`, …), and `run` (the
  entrypoint that invokes `runPipelined`). The four-phase orchestration is a
  correctness invariant, not a style choice.
- `go/cmd/bootstrap-index/bootstrap_pipeline.go` — `runPipelined` (the
  phase-ordering invariant), `drainProjectorPipelined`, `drainingWorkSource`, and
  `projectionWorkerCount`.
- `go/cmd/bootstrap-index/bootstrap_collector.go` — `drainCollector` and the
  discovery-advisory report writers.
- `go/cmd/bootstrap-index/bootstrap_projector.go` — `drainProjector`,
  `drainProjectorWorkItem`, the projector heartbeat, and the sequential drain.
- `go/cmd/bootstrap-index/bootstrap_db.go` — Postgres/graph open + schema apply.
- `go/cmd/bootstrap-index/graph_schema.go` — graph schema marker check and the
  direct bootstrap-index marker-missing initializer.
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

The six pipeline steps in `runPipelined` (`bootstrap_pipeline.go`) must execute
in order:

1. `drainCollector` + `drainProjectorPipelined` run concurrently.
   `BackfillAllRelationshipEvidence` is called after `drainCollector` returns,
   before the projector goroutine drains.
2. `cd.committer.BackfillAllRelationshipEvidence` (`bootstrap_pipeline.go`) populates
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

### Before adding a domain to the correlation reopen slice

`ReopenSucceededReducerWorkItems` marks a domain's succeeded work items pending
so its intent runs again. That is only safe when the domain's writer is
**generation-authoritative** — after the write, the durable facts for that
`(scope, generation)` are exactly the set the execution just produced.

An `ON CONFLICT (fact_id) DO UPDATE` upsert alone does NOT give you that. Check
what the fact identity is built from before adding a domain here:

- If the identity embeds any **decision-derived** field (an outcome, a finding
  kind, a resolved reference the replay may rewrite), a replay that reaches a
  DIFFERENT answer mints a new `fact_id` and the superseded row stays live for
  the same active generation.
- If the domain can decide "nothing qualifies" on the replay, no row is written
  at all and an upsert overwrites nothing.

Both leave stale rows that the read surfaces serve, since the fact read paths
filter on `is_tombstone` plus the active-generation join and do not pick a
latest row per key. The fix is a retire pass after the insert, deleting the
domain's own `fact_kind` for `(scope_id, generation_id)` minus the fact ids just
written — see `containerImageIdentityRetireQuery` and
`eshuSearchDocumentRetireQuery`. A retire is only correct when one intent covers
the whole scope generation; if any path evaluates a subset, it would delete rows
that are still valid.

A generation-authoritative retire also has to be **fenced**, and the reducer
queue does not fence it for you. The claim batch's in-flight exclusion requires a
LIVE lease, while the base predicate re-admits an item whose lease has already
expired — and lease expiry is exactly the stalled-worker case, since heartbeat
loss is quarantined only after `Handle` returns. So a worker that stalled past
its lease still writes, and an unfenced retire lets it DELETE the rows of the
worker that overtook it: strictly worse than the stale row it was added to
remove. Rank writers by when their evidence was READ (never by write time, which
ranks the stalled worker highest), stamp `fact_records.fencing_token` with that
watermark, and delete only rows at or below it. Three further traps:

- A zero watermark must be a hard error, because `fencing_token <= 0` matches
  everything and the retire runs completely unfenced with nothing saying so.
- The token must be stamped by the INSERT. A row left at the column default `0`
  between the insert and the retire is durable, visible, and deletable by any
  concurrent stalled worker, because `0` is at or below every token.
- Stamp it on the INSERT and NOWHERE ELSE. Re-stamping the keep-set from the
  retire — the `WITH stamped AS (UPDATE ...)` shape — is redundant once the
  insert carries the token, and redundant is not free: Postgres has no in-place
  UPDATE, so a no-op stamp still writes a second row version per row per
  execution (measured on this branch: keep-set `xmin` 879 → 880 with the token
  unchanged), and a statement-counting cost budget sees none of it. It is also an
  ABBA deadlock: the CTE locks the keep-set while the DELETE locks the
  complement, `WITH` specifies no ordering between them, and two concurrent
  same-scope retires with crossed keep/delete sets are exactly the stalled-worker
  shape the fence exists to handle. That deadlock was measured on the
  `5837-drift-reopen` sibling branch, not here — one harness run twice on
  Postgres 16.14, `SQLSTATE 40P01` with a `ShareLock` cycle both ways. It is a
  race, so quote the asymmetry and not a rate: the CTE variant deadlocked in most
  trials of every run, the plain fenced `DELETE` in none of twenty. Ship the
  fenced `DELETE` alone.

### Change NornicDB batch sizes or phase-group tuning

All NornicDB knobs are in `nornicdb_wiring.go`. Add or change a constant in the
`const` block, read the env var via `nornicDBPositiveIntEnv`, and pass the value
through `bootstrapNornicDBPhaseGroupExecutor`. Update
`docs/public/reference/nornicdb-tuning.md` and the active NornicDB ADR in the
same PR.

### Change projection worker count behavior

`projectionWorkerCount` (`bootstrap_pipeline.go`) reads `ESHU_PROJECTION_WORKERS` and
defaults to `min(NumCPU, 8)`. If you change the cap or the default, update the
concurrency reference table in `docs/public/reference/local-testing.md` and
`docs/public/deployment/service-runtimes.md`.

## Failure modes

| Failure | Symptom | Check |
| --- | --- | --- |
| Phase 2 backfill stalls | Binary hangs after collection completes | OTEL traces for `BackfillAllRelationshipEvidence`; check `go/internal/storage/postgres/ingestion.go` for the SQL path |
| Projector drain never exits | Binary hangs after Phase 2 | `drainingWorkSource.Claim` at `bootstrap_pipeline.go` wraps `ProjectorWorkSource`; confirm `collectorDone` is closed; check `maxEmptyPolls` logic |
| Phase 4 reopen skips stragglers | Reducer finds no `deployment_mapping` to process after bootstrap | Expected for items that succeeded in the Phase 2→4 window; use `/admin/replay` |
| Missing graph schema marker | Direct bootstrap-index run starts before schema marker exists | `graph_schema.go` applies strict graph schema and writes the marker; incompatible markers still fail closed |
| NornicDB timeout on graph write | `ESHU_CANONICAL_WRITE_TIMEOUT` exceeded | Lower `ESHU_NORNICDB_ENTITY_BATCH_SIZE` or `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS`; check `go/cmd/bootstrap-index/nornicdb_wiring.go` defaults |
| Heartbeat failure | `lease_heartbeat_failure` log + worker exits | Check `bootstrapIndexConnectionTimeout` and Postgres connectivity; heartbeat interval is `leaseDuration/3` capped at 1 minute |
| Superseded projector work | `status=superseded` log and worker continues | Expected when `ProjectorWorkHeartbeater` returns `projector.ErrWorkSuperseded`; do not ack or fail the stale generation |

## Anti-patterns

- **Do not skip `SkipRelationshipBackfill=true`.** Without it, every
  `CommitScopeGeneration` call runs a full per-repo backfill. On 800+ repos
  this is quadratically expensive and defeats the deferred-backfill design.
- **Do not call `ReopenDeploymentMappingWorkItems` before the projector drains.**
  The comment at `bootstrap_pipeline.go` explains why; `MaterializeIaCReachability` must
  also not run before the drain. Any refactor that merges or reorders these
  calls requires re-reading the ADR at
  `docs/public/services/bootstrap-index.md`.
- **Do not add signal handling without also adding a cleanup path for all
  phases.** The binary currently has no signal handlers by design (one-shot).
  If you add `SIGTERM` handling, you must decide what partial-phase state means
  for correctness and document it.
- **`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` is a conformance-only toggle.** On
  NornicDB the bootstrap canonical writer commits per dependency phase in both
  states — whole-materialization atomic canonical writes are unsupported because
  an UNWIND-driven MATCH cannot see a same-transaction MERGE and would silently
  drop nested files (#4027). Do not enable it expecting a single grouped
  canonical transaction on NornicDB; that path is valid only for a
  same-transaction read-your-writes backend (Neo4j). See `CLAUDE.md` section
  "NornicDB Compatibility Workflow".
- **Do not treat `errProjectorDrained` as an error.** It is a sentinel
  (`bootstrap_collector.go`) emitted after the `PhaseProjection` drain loop exhausts the
  queue. Worker goroutines return on it; do not propagate it through error
  channels.
- **Do not treat `projector.ErrWorkSuperseded` as a bootstrap failure.** The
  queue has already moved the stale generation out of the live backlog. The
  worker must return to the claim loop so the newer generation can run.
- **Do not skip the graph schema marker check.** Direct bootstrap-index runs may
  initialize a missing marker, but incompatible latest markers must stop before
  canonical graph writers open.

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
