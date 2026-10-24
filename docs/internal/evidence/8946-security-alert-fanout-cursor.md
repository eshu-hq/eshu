# #8946 Security-Alert Fan-out Cursor Evidence

No-Regression Evidence: for an allowlist of at most 100 same-org repositories
the generations stored per hour grow by at most one settling generation per
persistent stopper plus one, then the committer skips again. An allowlist over
100 reads the same first 100 repositories on every claim, exactly as before, and
stores one generation then skips. The per-claim provider bound is unchanged. A
rotating window for an allowlist over 100 was built, measured, and withdrawn
(see "Why rotation was withdrawn"); no rotation ships.

## Baseline and after

- Baseline: `origin/main` at 2733856cb3. Every claim starts at allowlist
  position 0 and reads at most 100 repositories; the first failure that is not
  repository-specific stops the walk, so repositories behind it are never read.
- After: for an allowlist that fits, a claim starts at the cursor the previous
  generation committed. The start moves forward through the allowlist, to the
  repository after a failed read. A failed read at the last position closes a
  lap: the start returns to the top and stays there until a claim reads every
  repository or that last repository leaves the allowlist. A full read resets
  the start; a rate limit leaves it. The lap needs one durable bit
  (`fanout_lap_repository`): with stoppers cutting the allowlist into segments,
  no stateless rule both moves past the first stopper and settles on a read set
  that contains the start-at-top set (a rule that kept the start at the last
  position settled on a smaller read set for 63 of 255 stopper subsets of an
  8-repository ring, and on an empty one for two trailing stoppers; a rule that
  wrapped to the top without a lap cycles when a stopper at the last position
  coexists with any other stopper; a stopper at the top alone settles). An
  allowlist over the cap always starts at the top.
- Properties, checked exhaustively by
  `TestFanoutNextIsForwardOnlyBoundedAndNeverReadsLessThanToday` for every
  subset of persistent stoppers on rings of 1, 2, 3, 8, 10, and 12 repositories
  over three laps of claims, with a stored generation keyed on the full
  per-position outcome map (complete, failed, stopped) that the freshness hint
  covers: stored generations at most stoppers plus one; the outcome map reaches
  its fixed point by claim stoppers plus one; the settled read set is a
  superset of the start-at-top read set; the start never decreases except for
  the single lap close.
- Backend/version: PostgreSQL 18 (`postgres:18-alpine`, aarch64), bootstrap
  schema with migrations through 232, `shared_buffers=256MB`.
- Cadence: one work item per target per 30 s (`DefaultReconcileInterval`,
  `coordinator/schedule/derivation.go`), at most one open run per target. The
  committer stores a generation only when its freshness hint differs from the
  newest pending-or-active generation's.

## Generations stored over 12 claims

Measured with the test fixtures (`org_fanout_cursor_test.go` helpers): 12
consecutive claims over unchanged provider data against a fake provider whose
listed repositories answer 503 on every claim, through a model of the
committer's hint comparison. "Before" is the same source without cursor wiring,
which starts every claim at the top as `origin/main` does.

| Target | Persistent stoppers | Before (start always at the top) | After (start with the lap) |
| --- | --- | --- | --- |
| 10 repositories | none | 1 stored, then skips | 1 stored, then skips |
| 10 repositories | position 4 | 1 stored, then skips | 2 stored (starts 0, 5, 5, ...) |
| 10 repositories | positions 2, 6 | 1 stored, then skips | 3 stored (starts 0, 3, 7, 7, ...) |
| 10 repositories | positions 1, 5, 8 | 1 stored, then skips | 4 stored (starts 0, 2, 6, 9, 9, ...) |
| 10 repositories | position 9 | 1 stored, then skips | 1 stored (starts 0, lap at 9) |
| 10 repositories | positions 0, 9 | 1 stored, then skips | 3 stored (starts 0, 1, 0, 0, ..., lap) |
| 10 repositories | positions 2, 9 | 1 stored, then skips | 3 stored (starts 0, 3, 0, 0, ..., lap) |
| 10 repositories | positions 8, 9 | 1 stored, then skips | 3 stored (starts 0, 9, 0, 0, ..., lap) |
| 250 repositories | none | 1 stored, then skips | 1 stored, then skips (always the top 100) |

At the default 30 s that is a one-time settling of up to one extra generation
per persistent stopper, then no growth. A repository behind a stopper stays
labeled `org_repository_fanout_stopped` (and the stopper itself
`org_repository_read_failed`) until the stopper is fixed or leaves the
allowlist; a lapped target reads exactly what the start-at-top fan-out reads.
Provider requests per claim stay at `max_pages` x (1 + 100).

## Why rotation was withdrawn

A rotating window for an allowlist over 100 stores a generation per claim:
120 an hour at 30 s. Fact ids embed the generation id, retention keeps every
superseded generation younger than 7 days, and a security-alert generation has
no source commit sha for the same-hint witness to match, so a 5,000-fact
target retains about 20,160 generations and 100.8M `fact_records` rows a week,
against one generation before, plus a whole-scope re-projection every 30 s.
That cost is not acceptable at any default cadence or with any mitigation
inside this change, so the fix for an allowlist over 100 is a split target
(#9019). The collector logs one `security_alert.org_fanout_over_cap` warning
for such a target at startup.

## Measured commit cost of one org generation

Throwaway live test (removed after the run) committing a synthetic 250-repository
org generation through `IngestionStore.CommitClaimedScopeGenerationWithState`
on the Postgres above: 5,000 facts (20 alerts per repository) and a read state
with 150 unread repositories and the cursor columns, five consecutive
generations of one scope. Each fact payload held four short fields (`number`,
`repository_id`, `package_name`, `severity`), about 150 bytes of JSON, so the
batch was under 1 MB; a real Dependabot alert payload carries advisory, CVSS,
and CWE fields and is larger, which makes the fact upsert figure a lower bound.
The host was a shared development machine. The reviewer's independent repro of
the same shape under a host load average of about 7 to 15 measured 434 to 582 ms
for the whole commit and 2.2 to 54 ms for `write_generation_state`, the same
order of magnitude.

| Measure | Result |
| --- | --- |
| Whole claim-fenced commit, 5,000 facts plus read state with three cursor columns | 397 to 440 ms (five runs: 440, 399, 411, 397, 402 ms) |
| `write_generation_state` stage (read state, 150 child rows, three cursor columns) | 2.3 to 3.1 ms on four runs; 60.8 ms on the first run of a fresh database |
| Earlier run with two cursor columns, quieter host | 315 to 341 ms whole commit, 2.2 to 3.1 ms stage |
| Reviewer A/B, same host and load | median about 390 ms with two columns, about 360 ms with three |

The third column (`fanout_lap_repository`, one nullable text value on the
parent row the stage already upserts) does not move the stage: its steady-state
2.3 to 3.1 ms matches the two-column run. The whole-commit difference between
the two runs tracks the load on the shared host (the reviewer's repro under a
load average of 7 to 15 measured 434 to 582 ms); the fact upsert, not the read
state, dominates it. A same-host, same-load A/B by the reviewer measured a median of about 390 ms
with two columns and about 360 ms with three columns (the lap): the column adds
no measurable cost.

This is the commit cost of one stored generation of a target of this size. The
settling generations above are one-time. The downstream projector and reducer
cost of one generation is not measured here (NOT_CHECKED, no end-to-end run).

## The cursor read

`unread.FanoutCursorQuery` on a seeded scope_generations of 5,000 scopes with 4
generations each (20,000 generations, every one with a read state), one claim,
`EXPLAIN (ANALYZE, BUFFERS)` output from
`TestSecurityAlertFanoutCursorLive/CursorReadIsAnIndexRangeOnTheScope`,
verbatim:

```
Nested Loop Left Join  (cost=12.90..20.94 rows=1 width=46) (actual time=0.022..0.022 rows=1.00 loops=1)
  Buffers: shared hit=6
  ->  Limit  (cost=12.62..12.62 rows=1 width=18) (actual time=0.015..0.016 rows=1.00 loops=1)
        Buffers: shared hit=3
        ->  Sort  (cost=12.62..12.62 rows=1 width=18) (actual time=0.015..0.015 rows=1.00 loops=1)
              Sort Key: generation.ingested_at DESC, generation.generation_id DESC
              Sort Method: quicksort  Memory: 25kB
              Buffers: shared hit=3
              ->  Index Scan using scope_generations_scope_idx on scope_generations generation  (cost=0.29..12.61 rows=1 width=18) (actual time=0.010..0.010 rows=1.00 loops=1)
                    Index Cond: ((scope_id = 'target-7'::text) AND (status = ANY ('{pending,active}'::text[])))
                    Filter: (COALESCE(freshness_hint, ''::text) <> ''::text)
                    Index Searches: 1
                    Buffers: shared hit=3
  ->  Index Scan using security_alert_target_read_states_pkey on security_alert_target_read_states read_state  (cost=0.29..8.30 rows=1 width=56) (actual time=0.005..0.005 rows=1.00 loops=1)
        Index Cond: (generation_id = generation.generation_id)
        Index Searches: 1
        Buffers: shared hit=3
Planning:
  Buffers: shared hit=104
Planning Time: 0.574 ms
Execution Time: 0.044 ms
```

- Index scan using `scope_generations_scope_idx` on the scope, then a primary
  key lookup on `security_alert_target_read_states_pkey`; no sequential scan on
  either table. Execution 0.044 ms, 6 shared buffers hit.
- An earlier join form picked a hash join and scanned the read-state table; the
  lateral probe is what ships and the test asserts the primary-key plan.
- The read is one statement per org claim that reaches the fan-out, issued
  before the first per-repository request, for an allowlist that fits the cap
  (a target over the cap reads no cursor).

## Concurrency, retry, idempotency

- The cursor moves only in the generation's commit transaction (the #8943
  state writer). `TestSecurityAlertFanoutCursorLive` proves on Postgres that a
  rolled-back generation and an unchanged-generation skip leave the cursor
  where it was, a replay of one generation id replaces all three columns, and two
  generations of one scope committing in parallel from the same start keep
  their own rows and agree on the next repository.
- The planner admits one open run per target and the claim is leased with a
  fencing token checked in the commit transaction; a stale replica's commit
  fails the fence, so its cursor never lands. The worst case is a duplicated
  window read, never a skipped repository. No per-scope lock was added.
- The reader selects the newest pending-or-active generation with the ordering
  `activeGenerationFreshnessQuery` uses, not `ingestion_scopes.active_generation_id`,
  so a claim between a commit and its activation resumes instead of re-reading
  the same window and being skipped.
- The start is a pure function of the allowlist and the cursor. The walk order
  changes with the start, and the freshness digest sorts alerts, so walk order is
  not in the hint. The hint excludes the cursor fields, so an unchanged
  generation is skipped and the cursor, which rides on the stored generation's
  row, is not rewritten; for a partial walk, equal outcome maps imply equal
  starts.
- Retention: the columns ride on the read-state row, which cascades from
  `scope_generations`; no new retention arm is needed.

Observability Evidence: the `security_alert.observe` span carries
`eshu.security_alert.org_fanout.start_offset`, `.next_offset`, and `.lapped`
(`TestClaimedSourceFanoutSpanCarriesCursorOffsets`), the read-state row carries
`fanout_start_repository`, `fanout_next_repository`, and
`fanout_lap_repository` (a lap in progress is `fanout_next_repository IS NULL
AND fanout_lap_repository IS NOT NULL`), and
`NewClaimedSource` logs one `security_alert.org_fanout_over_cap` WARN per org
target over the cap with the repository count, the cap, and the scope id
(`TestNewClaimedSourceWarnsForAnOrgTargetOverTheFanoutCap`). With the existing
fan-out counter (`skipped_cap` non-zero on every claim of one target means that
target has more than 100 same-org repositories and needs splitting) and the `org_repository_*` reasons on each fact, an
operator sees where a claim started, where the next resumes, and which
repositories were unread, without a repository name in a label.
