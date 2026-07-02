# #4464 pipeline wedge: per-call timeout isolation and lease reclaim

This note records the evidence for the two residual fixes in issue #4464
(the 3-bug pipeline wedge cascade) that landed after PR #4474 fixed the
stage-level shared-context cancellation in `bootstrap_projector.go`. See
that README section
(`go/cmd/bootstrap-index/README.md#per-item-projection-failure-isolation-4464`)
for the stage-level fix this note builds on. Bug 3 (slow canonical MERGE
writes) is out of scope here; the Helm `REFERENCES` retract instance was
already fixed in PR #4505 (dedicated `HELM_VALUE_REFERENCE` edge type).

## Bug 1 residual: intra-materialization entity-phase timeout isolation

PR #4474 isolated per-work-item failures across the 8 projector-stage
workers (one work item = one repo's materialization). It left the identical
defect class one level down: **inside** a single materialization's canonical
write, the entity-phase chunk fan-out shared one `context.WithCancel(ctx)`
across its whole worker pool and canceled it on the first chunk error —
including a `GraphWriteTimeoutError` from `TimeoutExecutor`. Because
`TimeoutExecutor` derives its own per-call deadline from that same shared
context (`context.WithTimeout(runCtx, e.Timeout)`), canceling the parent
tore down every sibling chunk's in-flight `ExecuteGroup` call too — even
chunks that would otherwise have committed successfully.

Three call sites had this shape:

- `go/cmd/bootstrap-index/nornicdb_entity_phase_group_concurrent.go`
  (`executeGroupedChunksConcurrently`) — the live bootstrap-index production
  path (`ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY` defaults to
  `min(NumCPU, 16)` on multi-core hosts, so `entityPhaseConcurrency > 1` is
  the common case).
- `go/cmd/ingester/wiring_nornicdb_phase_group_concurrent.go`
  (`executeGroupedChunksConcurrentlyObserved`) — reachable from the legacy
  per-flush `executeEntityPhaseGroup` path.
- `go/cmd/ingester/wiring_nornicdb_phase_group_streaming.go`
  (`executeEntityPhaseGroupStreaming`) — the persistent worker pool that
  `ExecutePhaseGroup` routes to for entity phases when
  `entityPhaseConcurrency > 1` (the ingester's live production default).

### Fix

Split the cancelable context into two roles in each of the three functions:

- A **dispatch-control** context (`dispatchCtx`/`poolCtx`, still
  `context.WithCancel(ctx)`) gates admission of *new* chunks only — the
  dispatch loop's `select` arm and each worker's pre-pull `Err()` check.
- Each in-flight chunk's `ge.ExecuteGroup` call now runs against `ctx` (the
  caller's context) directly, never the dispatch-control context, so
  canceling dispatch stops new work from being scheduled but cannot reach
  into a sibling's already-running write.

### Performance Evidence (No-Regression Evidence)

The happy path is unchanged: chunk dispatch order, chunk sizing, and worker
count are untouched. Only the context threaded into `ExecuteGroup` changed
from the cancelable dispatch context to the caller's context — a
zero-cost change (no new allocation, no new goroutine, no new lock).

```
cd go && GOCACHE=<worktree>/.gocache go test ./cmd/bootstrap-index ./cmd/ingester -race -count=1
# 69 bootstrap-index + 153 ingester = 222 tests, all pass
```

Regression tests reproduce the exact cascade with a fake `GroupExecutor`
that fails one chunk immediately with a `GraphWriteTimeoutError` while a
sibling chunk blocks on a release gate, confirmed to fail before the fix
and pass after:

- `TestExecuteGroupedChunksConcurrentlyIsolatesPerChunkTimeout`
  (`go/cmd/bootstrap-index/nornicdb_entity_phase_group_concurrent_isolation_test.go`)
- `TestExecuteGroupedChunksConcurrentlyObservedIsolatesPerChunkTimeout`
  (`go/cmd/ingester/wiring_nornicdb_phase_group_concurrent_isolation_test.go`)
- `TestExecuteEntityPhaseGroupStreamingIsolatesPerChunkTimeout`
  (`go/cmd/ingester/wiring_nornicdb_phase_group_streaming_test.go`)

Before-fix reproduction (all three, same failure signature):

```
healthy sibling chunk observed ctx error = context canceled, want nil —
a sibling chunk's timeout must not cancel this chunk's in-flight
ExecuteGroup call (#4464 Bug 1)
```

### Observability Evidence (No-Observability-Change)

The existing `nornicdb phase-group chunk completed` /
`bootstrap nornicdb phase-group chunk completed` structured logs and the
chunk-level error wrapping (`phase-group chunk %d/%d ...: %w`) already
attribute a failure to its own chunk. This fix only stops a sibling's
*unrelated* success from being misreported as a cancellation, so no new
signal is needed to diagnose it.

## Bug 2: orphaned expired-lease reclaim

PR #4474 routes a per-item timeout to `WorkSink.Fail` instead of crashing
`bootstrap-index`, closing the *application-level* path to an orphaned
claim. It does not close the *process-level* one: if `bootstrap-index`
itself dies (OOM-kill, panic, container crash) while holding
`source_local` projector claims, the leases expire but nothing ever calls
`Claim()` again.

Confirmed from the runtime topology (`docker-compose.yaml`):

- `ingester` has `depends_on: bootstrap-index: condition:
  service_completed_successfully`, and `bootstrap-index` has no
  `restart_policy`. A non-zero `bootstrap-index` exit means `ingester`
  never starts, so it never claims `source_local` work either.
- `resolution-engine` (the `eshu-reducer` binary) does **not** depend on
  `bootstrap-index` and runs `GenerationLivenessRunner` continuously — but
  its `RecoverWedgedGenerations` in-flight guard excluded **any**
  `source_local` projector row with `status IN ('pending', 'claimed',
  'running', 'retrying')`, treating an expired-lease `claimed`/`running`
  row as still "in flight" forever.

The claim SQL (`claimProjectorWorkQuery`) already ranks an expired-lease
`claimed`/`running` row first and reclaims it directly — but only when
something calls `Claim()` again. Nothing in the runtime topology does, once
the sole `source_local` claimer process has died. Permanent wedge, zero
dead-letters, matching the issue's reproduction exactly.

### Fix

`recoverWedgedActiveGenerationsQuery` and `countActiveGenerationsByAgeQuery`
(`go/internal/storage/postgres/generation_liveness_sql.go`) now exclude
`pending`/`retrying` rows unconditionally (no active lease owner, but still
legitimately queued for a live claimer) and exclude `claimed`/`running`
rows only while `claim_until > now` (the lease is still live). A
`claimed`/`running` row whose lease has already expired no longer blocks
re-drive; the liveness sweep re-enqueues it (`ON CONFLICT DO UPDATE` on the
same deterministic `work_item_id`, so the orphaned row itself flips to
`pending` rather than a duplicate being created) the next time it polls
(default 5-minute interval, `cmd/reducer`).

Both queries changed in lockstep because `countActiveGenerationsByAgeQuery`'s
`stuck` bucket is the operator alarm for exactly the scopes
`RecoverWedgedGenerations` will re-drive; the query's own docstring states
"the stuck bucket ... must match the recovery gate." Fixing only the
recovery query would have left the alarm silent on an orphaned-lease wedge
even after the recovery sweep started correctly re-driving it.

### Performance Evidence (No-Regression Evidence)

The SELECT-side gate change only widens which rows are eligible for a
candidate that already passed every other wedge criterion (aged past the
activation deadline, outstanding downstream shared-projection intent,
reducer work drained, no newer sibling generation). It does not change the
write shape, the `ON CONFLICT` upsert, or the bounded re-drive budget. Both
queries remain exact 0-row/1-row bounded semi-joins per candidate row
against `fact_work_items` indexed by `scope_id`/`generation_id`; no new
index needed.

```
cd go && GOCACHE=<worktree>/.gocache go test ./internal/storage/postgres -run "GenerationLiveness|Recover" -race -count=1
# 34 tests, all pass

cd go && GOCACHE=<worktree>/.gocache go test ./internal/storage/postgres -count=1
# 1198 tests, all pass (full package regression)
```

Real-Postgres integration proof, `ESHU_GENERATION_LIVENESS_PROOF_DSN`
against a throwaway `postgres:16-alpine` Docker container (not testcontainers
— a plain `docker run`, torn down after the run):

```
docker run -d --name eshu-4464-liveness-proof -e POSTGRES_PASSWORD=proof \
  -e POSTGRES_USER=proof -e POSTGRES_DB=proof -p 15499:5432 postgres:16-alpine
ESHU_GENERATION_LIVENESS_PROOF_DSN="postgresql://proof:proof@localhost:15499/proof?sslmode=disable" \
  go test ./internal/storage/postgres -run TestGenerationLivenessIntegration -v -count=1
docker rm -f eshu-4464-liveness-proof
```

Result: `TestGenerationLivenessIntegration` all 8 subtests pass, including
two new ones:

- `ExpiredLeaseReclaimed` (positive case): a `claimed` row with
  `claim_until` 25 minutes in the past IS reopened to `pending` with
  `lease_owner`/`claim_until` cleared.
- `LiveLeaseNotReclaimed` (negative case): a `running` row with
  `claim_until` 4 minutes in the future is left untouched — proves the fix
  discriminates on lease expiry, not status alone, so it cannot race a
  genuinely active worker's in-flight canonical write.

Verified the new tests fail for the right reason against the pre-fix query
(temporarily reverted `generation_liveness_sql.go` and `generation_liveness.go`
in the worktree, reran, restored):

```
--- FAIL: TestRecoverWedgedActiveGenerationsQueryReclaimsExpiredProjectorLease
--- FAIL: TestGenerationLivenessIntegration/ExpiredLeaseReclaimed
    generation_liveness_integration_test.go:404: Recovered = 0, want 1
    (expired-lease claimed row must be reclaimed)
--- PASS: TestGenerationLivenessIntegration/LiveLeaseNotReclaimed
    (passes either way — confirms it is not a false-positive test)
```

### Observability Evidence (No-Observability-Change)

The existing `GenerationLivenessRecovered` counter
(`go/internal/telemetry/instruments.go`, emitted by
`GenerationLivenessRunner.recordResult`) and the `generation liveness
recovery cycle completed` structured log already count and log every
re-drive from `RecoverWedgedGenerations`, including this widened case; the
fix only changes which rows the existing query selects, not the emission
site. `scripts/verify-telemetry-coverage.sh` confirms no new
stage/metric drift (only `go/cmd/*` and `go/internal/storage/postgres/*`
files changed — neither is a watched stage-owner directory for the X2
gate).

## Verification commands run (both bugs)

```
cd go && gofmt -l ./cmd/ingester/ ./cmd/bootstrap-index/ ./internal/storage/postgres/generation_liveness*.go
cd go && go vet ./...
cd go && go build ./...
cd go && go test ./cmd/bootstrap-index/... ./cmd/ingester/... -race -count=1
cd go && go test ./internal/storage/postgres/... -count=1
bash scripts/test-verify-telemetry-coverage.sh
ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
```
