# #5429: CI/CD run watermark -- theory proof and local proof

## Theory being proved

`cicd_run_watermarks` (`go/internal/storage/postgres/cicd_run_watermark.go`)
is a new table backing `runwatermark.Store`. The claim: point `Load`/`Save`
queries scoped by the `(scope_id, repository)` primary key use an index
lookup, not a sequential scan, at a representative row count, and the
fencing `WHERE` guard on `Save`'s `ON CONFLICT DO UPDATE` correctly rejects a
stale (lower) fencing token while accepting a newer one -- in real Postgres,
not only in the Go unit tests that fake the `ExecQueryer`/`Rows` seam.

## Proof method

Ran the throwaway shim below against a scratch `postgres:16-alpine` container
(`docker run --rm -e POSTGRES_PASSWORD=postgres -p 55429:5432
postgres:16-alpine`), no eshu Docker Compose stack involved. 50,000
synthetic `(scope_id, repository)` rows -- a generous upper bound versus any
realistic single-deployment fleet of polled GitHub Actions repositories.

Full script:
`/private/tmp/claude-501/-Users-asanabria-repos-eshu-hq-eshu/d7b9d08f-a0d9-493d-9b12-2459308cf5a0/scratchpad/5429_watermark_proof.sql`
(scratchpad path, not committed; reproducible from the DDL/query constants in
`cicd_run_watermark.go` plus the `INSERT ... generate_series` seed below).

```sql
CREATE TABLE cicd_run_watermarks (
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    last_run_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, repository)
);

INSERT INTO cicd_run_watermarks (scope_id, repository, generation_id, fencing_token, last_run_id, updated_at)
SELECT
    'ci-cd:github-actions:org' || (i % 500) || '/repo' || i,
    'org' || (i % 500) || '/repo' || i,
    'generation-' || i,
    i,
    (100000 + i)::text,
    now() - (i || ' seconds')::interval
FROM generate_series(1, 50000) AS i;

ANALYZE cicd_run_watermarks;
```

## Results

### Load query -- index scan on the PK

```
EXPLAIN (ANALYZE, BUFFERS)
SELECT last_run_id, generation_id, fencing_token, updated_at
FROM cicd_run_watermarks
WHERE scope_id = 'ci-cd:github-actions:org250/repo25000' AND repository = 'org250/repo25000';
```

```
Index Scan using cicd_run_watermarks_pkey on cicd_run_watermarks
  (cost=0.41..8.43 rows=1 width=39) (actual time=0.009..0.009 rows=0 loops=1)
  Index Cond: ((scope_id = ...) AND (repository = ...))
  Buffers: shared hit=3
Execution Time: 0.021 ms
```

### Comparison baseline -- forced sequential scan (same predicate)

```
SET enable_indexscan = off; SET enable_bitmapscan = off;
EXPLAIN (ANALYZE, BUFFERS) <same SELECT>
```

```
Seq Scan on cicd_run_watermarks
  (cost=0.00..1520.97 rows=1 width=39) (actual time=2.537..2.537 rows=1 loops=1)
  Filter: ((scope_id = ...) AND (repository = ...))
  Rows Removed by Filter: 50001
  Buffers: shared hit=770
Execution Time: 2.540 ms
```

The PK index scan reads 3 buffers and 0.021 ms versus the seq scan's 770
buffers and 2.540 ms at 50,000 rows (~120x fewer buffer reads, ~120x lower
latency) -- the query plan the shipped `Load` query gets in practice, not the
forced worst case.

### Save (UPSERT) -- fencing guard rejects a stale token, accepts a newer one

Existing row at `fencing_token=99999` (from a prior save in the same
session). Attempting a stale write (`fencing_token=1`):

```
Insert on cicd_run_watermarks (actual time=0.021..0.021 rows=0 loops=1)
  Conflict Resolution: UPDATE
  Conflict Arbiter Indexes: cicd_run_watermarks_pkey
  Conflict Filter: (cicd_run_watermarks.fencing_token <= excluded.fencing_token)
  Rows Removed by Conflict Filter: 1
  Tuples Inserted: 0
  Conflicting Tuples: 1
```

`RowsAffected() == 0` here is exactly what `CICDRunWatermarkStore.Save`
turns into `runwatermark.ErrStaleFence`. Confirmed the row was in fact left
unchanged:

```sql
SELECT fencing_token, last_run_id FROM cicd_run_watermarks WHERE ...;
--  fencing_token | last_run_id
-- ---------------+-------------
--          99999 | 999999
```

A higher fencing token (`99999` following an earlier lower value) shows
`Tuples Inserted: 1` -- the update proceeds. A brand-new key (no conflict)
also inserts cleanly via the same statement.

## Conclusion

Theory confirmed: the PK composite index is used for both `Load` and
`Save`'s conflict arbiter (no sequential scan at 50k rows), and the fencing
`WHERE` guard is enforced by Postgres itself, not only application code --
real-Postgres proof, not the Go unit tests' faked `ExecQueryer`. No
optimization is being claimed here (this is a brand-new table, not a
rewrite of an existing query), so there is no "old shape vs. new shape"
equivalence check to run; this proof exists to satisfy the repository's
mandatory prove-the-theory-first gate before landing new hot-path-adjacent
SQL, and to catch a missing/wrong index before it ships.

Container was a throwaway `docker run --rm ... postgres:16-alpine`, stopped
and removed after the run; no state persists.

## Follow-up: commit-ordering fix (P1, codex review on PR #5765)

### The bug

`ghactionsruntime.ClaimedSource.NextClaimed` (`source.go`) called
`saveWatermark` directly on its own success path, BEFORE
`collector.ClaimedService.commitCollected` (`claimed_service.go`) had
committed that cycle's facts. If the commit failed (a retryable Postgres
error, for example) and the SAME work item was retried, the retry's
`NextClaimed` re-fetched the identical window but now compared it against
the ALREADY-ADVANCED watermark from the failed attempt -- so
`detectRunBackfillGap` no longer saw a gap, and the `runs_backfill_gap`
warning silently vanished on retry even though the runs between the true
prior watermark and the window floor were never fetched by any
successfully-committed cycle. The watermark is durable state; a failed
commit is not idempotent-safe to precede.

### The fix

The durable `saveWatermark` write moved out of `NextClaimed` entirely. A new
optional interface, `collector.ClaimedGenerationCommitObserver`
(`go/internal/collector/claimed_service_commit_observer.go`), gives a
claim-aware source a post-commit hook: `collector.ClaimedService` invokes it
from `processClaimed`, immediately after `commitCollected` succeeds and
before the claim is completed -- mirroring the existing optional
`GenerationDeadLetterReplayCompleter` pattern
(`claimed_service_dead_letter.go`).

`ghactionsruntime.ClaimedSource` implements the new hook
(`source_commit_observer.go`). `NextClaimed` now only STASHES the observed
newest run ID in a mutex-guarded, process-local map keyed by
`(ScopeID, GenerationID)` (`pending_watermark.go`'s `pendingWatermarks`,
shared via a pointer field across every value copy of `ClaimedSource`
because `collector.MultiSourceCollectorHost` can run several
`ClaimedService` workers against one registered source concurrently).
`ObserveClaimedGenerationCommitted` takes the stashed entry and calls the
existing (unchanged) `saveWatermark`. An observer error is treated as
non-fatal by `collector.ClaimedService` (recorded as a span event, claim
still completes) because the facts already committed durably -- there is
nothing to roll back, and the watermark simply stays where it was until the
next successful commit.

### Local proof: failing-then-green regression test

`TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap`
(`go/internal/collector/cicdrun/ghactionsruntime/source_watermark_commit_ordering_test.go`)
seeds a watermark simulating an earlier successfully-committed cycle, then
calls `NextClaimed` twice for the SAME work item with no
`ObserveClaimedGenerationCommitted` call between them (modeling a failed
commit followed by a retry). Both calls must independently emit the
`runs_backfill_gap` warning; only a subsequent explicit
`ObserveClaimedGenerationCommitted` call may advance the stored watermark.

Fail-before (against `NextClaimed` still calling `saveWatermark` directly):

```
=== RUN   TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap
    source_watermark_commit_ordering_test.go:112: no ci.warning fact with reason "runs_backfill_gap" found; observed reasons = [runs_truncated]
--- FAIL: TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap (0.00s)
FAIL
```

Pass-after (fix applied):

```
=== RUN   TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap
--- PASS: TestClaimedSourceRetryAfterCommitFailureStillDetectsBackfillGap (0.00s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime	0.706s
```

### Regression and concurrency verification

```
cd go && go test ./internal/collector/cicdrun/... ./internal/collector/ ./cmd/collector-cicd-run/ -count=1
cd go && go test -race ./internal/collector/cicdrun/... -count=1
cd go && go test -race ./internal/collector/ -count=1
cd go && go build ./...
cd go && go vet ./internal/collector/...
```

All green, including the pre-existing `source_watermark_test.go` suite (four
of its tests were updated to call `ObserveClaimedGenerationCommitted`
explicitly between cycles, since they previously relied on `NextClaimed`
itself durably advancing the watermark) and two new concurrency tests
(`TestPendingWatermarksConcurrentStashAndTakeIsRaceFree`,
`TestClaimedSourceConcurrentNextClaimedAndObserveIsRaceFree`,
`go/internal/collector/cicdrun/ghactionsruntime/pending_watermark_test.go`)
proving the shared staging map is race-free under concurrent claims for
different work items.

No-Regression Evidence: the fix only moves WHEN `saveWatermark` runs; its
own fencing/nil-safety/no-op-on-empty-window semantics are unchanged and
covered by the existing `run_watermark_test.go` unit tests plus the updated
`source_watermark_test.go` end-to-end tests.

No-Observability-Change: no new metric was added. A commit-observer failure
is recorded as a span event (`claimed_generation_commit_observer_failed`) on
the existing `collector.claimed_run` span rather than a new instrument,
since it is a secondary, self-healing signal, not a claim-outcome change.
