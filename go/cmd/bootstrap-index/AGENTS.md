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

Both leave stale rows when the read surface treats every active-generation fact
as current. This was the `container_image_identity` failure in #5847 before
#5854 and #5740 replaced its outcome-keyed facts with digest-keyed immutable
support sets. The current writer atomically moves one `active_set_id` per scope;
reclassification selects a replacement set, and demotion selects an explicit
empty set. Older sets remain stored but are not current.

The obvious remedy — delete every prior row absent from the replay's output —
has a precondition that is easy to miss and expensive to get wrong. **A retire
deletes on the strength of absence, so it is only correct when a pass can never
see less than the source currently holds.** Before adding one, audit the
collector's error model for paths that produce a smaller generation without any
upstream failure being asserted:

- a soft-failed sub-fetch downgraded to a warning envelope, where the caller
  emits the parent fact anyway with the missing field simply omitted — if that
  field creates references, every reference that existed only through it
  disappears;
- an unpaginated or truncated listing, where a new entry evicts the tail and the
  evicted entry's observation silently vanishes.

`container_image_identity` has both (`ociruntime/config_provenance.go` returns
nil labels plus a warning envelope on a `GetBlob` failure;
`ociruntime/source_references.go` bounds the tag list). Its current planner
carries only the affected prior supports while the warning remains and retires
them after a complete scan clears the warning. A new domain needs an equivalent,
evidence-specific hold; do not treat an activated generation as complete merely
because the collector returned no error.

The replacement or retire must also be **fenced**, and the reducer queue does not
fence it for you. The claim batch's in-flight exclusion requires a live lease,
while the base predicate re-admits an item whose lease has expired. A worker that
stalled past its lease can still reach the write path after another worker has
published fresher truth. The current container-image writer checks the exact
work-item claim epoch and the scope activation epoch in the same statement that
moves `active_set_id`; a stale worker cannot publish or retire current support.

The earlier v2 `fact_records` writer used an evidence-read watermark and a
guarded upsert. Its three implementation traps remain useful for another domain
that cannot use active-set replacement; they are recorded in
`docs/internal/evidence/5847-container-image-identity-retire.md`:

- A zero watermark must be a hard error. It is what rows carry by table default,
  so a domain that forgets it looks fenced and behaves unfenced.
- The token must be stamped by the INSERT, not by a later statement. A row left
  at `0` in between is durable, visible, and — to a retire — deletable by any
  concurrent stalled worker.
- Stamp it on the INSERT and nowhere else. Re-stamping the keep-set from the
  retire (the `WITH stamped AS (UPDATE ...)` shape) is redundant once the insert
  carries the token, and redundant is not free: Postgres has no in-place UPDATE,
  so a no-op stamp still writes a second row version per row per execution
  (measured: keep-set `xmin` 879 → 880 with the token unchanged), and a
  statement-counting cost budget sees none of it. It is also an ABBA deadlock —
  the CTE locks the keep-set while the DELETE locks the complement, `WITH`
  specifies no ordering between them, and two concurrent same-scope retires with
  crossed keep/delete sets are exactly the stalled-worker shape. Measured on the
  `5837-drift-reopen` branch, one harness run twice on Postgres 16.14,
  `SQLSTATE 40P01` with a `ShareLock` cycle both ways: the CTE variant deadlocked
  in most trials of every run, the plain fenced `DELETE` in none of twenty.

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
