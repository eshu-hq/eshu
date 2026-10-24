# Changed-since sweeper holds its generations until it commits (#8932)

Refs #8932. `TestProjectorAckJournalContention` once reported one orphan
`changed_since_activations` row while generation retention ran beside the
journal loop. The cause was a real, narrow race between the sweeper insert and
retention, not a test artifact.

## Cause

`journalActiveGenerationsQuery` inserted a row for a generation that was active
at its snapshot and took no lock on that generation. The statement order that
leaves the orphan:

1. A journal pass inserts the sweeper row for g0 and has not committed.
2. An Ack activates g1 (it misses the journal gate, so it writes no row) and
   commits. g0 is superseded.
3. `PruneSupersededGenerations` finds g0 eligible, locks it (nothing holds it),
   deletes the committed ledger rows (the pass's row is invisible to it), and
   deletes g0.
4. The pass commits. The row names a generation that no longer exists.

The old exemption in the links `AGENTS.md` rested on a time premise (retention
prunes only generations superseded longer than its age cutoff). The contention
test's retention clock (two hours ahead, one-minute ages) voids that premise.
At the production defaults (168 h soft, 2160 h hard) the window is reachable
only with a tiny configured age or clock skew (skew not checked).

## Change

The sweeper statement locks each generation it names `FOR KEY SHARE OF
generation SKIP LOCKED` in a `locked` CTE and holds it until the pass commits.
The CTE is an inner join, because PostgreSQL refuses a row lock on the nullable
side of a LEFT JOIN. Retention's `FOR UPDATE ... SKIP LOCKED` then skips the
generation, and KEY SHARE does not conflict with Ack's status update. A scope
whose active generation has no `scope_generations` row is journaled as before,
with the fallback `activated_at`.

The pass runs the statement under the 250 ms generation lock timeout, like the
backfill insert, because the lock can wait in the update-chain walk (arbiter
ruling arb-7127-3e-wait). A 55P03 rolls the pass back as the non-counting
`generation_lock_timeout`, which `Runner.journalRetry` already handles.

A skipped generation is one that retention is pruning (it locks only a
superseded generation) or that the purge cascade is deleting with its scope.
Its activation is dropped on purpose and is harmless: the scope's current active
generation is journaled by the next pass or by its Ack, and readers fall back to
the exact diff until then. The skip test proves the skip mechanics (no wait, no
error, the rest of the pass proceeds, a later pass journals the row if the
generation is still active), not a retry of a generation retention prunes.

## Proof

PostgreSQL 18.6 over a local Unix socket, `nice -n 15`, one run at a time, on a
shared host with load average 12 to 19.

- RED on `origin/main` `c664b72cb0`: `TestSweeperActivationSurvivesRetentionPrune`
  pruned the held generation (1) and ended with
  `orphans {Links:0 Activations:1 HeadlessBucketGroups:0}`, the shape of the CI
  failure.
- GREEN with the change: the prune while the pass is held prunes 0, orphans are
  0 after the commit, and the next prune removes the generation with its now
  visible row. 20 of 20 runs passed (`-count=20`, 59 s wall).
- `TestProjectorAckJournalContention`, `-count=10` (20 subtests, 480 Acks each,
  with the journal loop, four link writers and retention): all passed in 350.7 s
  of test time (384 s wall). Every subtest logged 0 deadlocks, 0 duplicates, 0
  commit-order violations and orphans `{Links:0 Activations:0
  HeadlessBucketGroups:0}`.
- Mutation: removing the lock from the statement fails the regression (pruned
  while held 1, one orphan), the skip test and both bounded-wait tests. Removing
  the lock timeout from the pass fails `TestJournalSweeperLockWaitIsBounded`
  (the statement waits until the 3 s test context expires).
- `TestJournalSweeperSkipsAGenerationRetentionHoldsAndJournalsItNextPass`: a
  held active generation is skipped without waiting or an error, the other scopes
  (including the generation-less one) are journaled in the same pass, and the
  pass after the holder ends journals the skipped generation.
- `TestJournalSweeperLockWaitIsBounded`: with a holder on the new version of the
  row, the pass answers `generation_lock_timeout` (55P03) after 254 ms and writes
  nothing. The unbounded statement waits past 1.5 s (the RED control).

- `TestJournalSweeperLockWaitIsBounded/bound_ends_before_the_cursor_insert`: a
  held cursor insert on a swept scope makes the cursor statement wait 402 ms
  and the pass succeed. Removing the reset to 0 after the sweeper fails it with
  `create cursors: ... 55P03` after 254 ms.

## Performance Evidence

Performance Evidence: baseline `origin/main` `c664b72cb0`, PostgreSQL 18.6, 50,000
scopes with one active generation each plus the bootstrap `eshu:global` scope.
Terminal counts: 11 rows inserted in the steady case, 50,001 in the bulk case.

The conflict domain is the `scope_generations` row of each generation the
sweeper journals, against retention's `FOR UPDATE SKIP LOCKED`. No worker count,
batch size or lease setting changes. The pass still holds its exclusive advisory
key and never waits on it.

`EXPLAIN (ANALYZE, BUFFERS)` of the old and new statement on 50,000 scopes with
one active generation each, in a rolled-back transaction, execution time, median
of three runs:

| Case | Old | New |
| --- | --- | --- |
| Steady state, 10 of 50,000 unjournaled | 16.8 ms (17.2, 16.8, 16.6) | 17.0 ms (17.2, 17.0, 16.9) |
| First pass, all 50,000 unjournaled | 322 ms (315, 322, 648) | 851 ms (3,126, 593, 851) |

Steady state is unchanged. The plan keeps the hash anti join over
`ingestion_scopes`, the full scan that dominates both, and adds a `LockRows` over
the unjournaled rows only (11 here).

The one-time bulk case is slower, about 2.6 times at the median. It locks 50,000
rows (892 dirtied heap pages against 649), and the host was loaded: the 3,126 ms
first run includes JIT and a CTE spill to disk at the default `work_mem`. It
happens once, when a fleet's active generations first enter the ledger. Not
measured: the bulk case on a quiet host.

Scale bound. Row locks live in the tuple headers, not in the shared lock table:
the pass's backend held 35 `pg_locks` entries with 50,001 rows locked, so
`max_locks_per_transaction` does not bind (the PostgreSQL explicit-locking page
says there is no limit on rows locked at one time, though a lock can write to
disk). `lock_timeout` bounds each lock wait, not the statement, and the journal
transaction sets no `statement_timeout`, so the first pass takes as long as it
takes and grows with the number of unjournaled active generations. Through
`Journal()` at 50,000 scopes (50,001 rows, no 55P03) the first pass took 883,
717 and 559 ms (median 717 ms) and the second pass about 37 ms with 0 rows, on a
host at load average 7 to 12. While the first pass runs it holds the exclusive
advisory key, so Acks skip the journal gate and the purge cascade waits for it.

## Observability

No-Observability-Change: no metric, span, log or status signal changes. A
sweeper lock timeout is the existing
`eshu_dp_changed_since_link_retries_total{reason="generation_lock_timeout"}`,
documented as coming from a link or a journal pass. The orphan probe
`eshu_dp_changed_since_ledger_orphans` is the signal that showed this race. A
skipped generation has no signal of its own; it shows up as the next pass's
`SweeperRows`.

NOT_CHECKED: clock skew between replicas, and the contention test on a loaded
Postgres with a competing checkpoint.
