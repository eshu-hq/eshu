# Changed-since link ledger store

## Purpose

This package persists the changed-since link ledger of #7127. The reducer's
`changed_since_link` runner (`go/internal/reducer/freshness/links`) calls it to
turn each generation activation into a link: the keys that changed between
the scope's previous linked generation and the activating one. A later read
path (PR-3c) answers `get_changed_since` from those links in O(changes)
instead of diffing two whole generations.

The writer is dark unless `ESHU_CHANGED_SINCE_LINK_ENABLED=true`. The same
switch makes the projector Ack in the ingester and bootstrap-index journal
each activation it commits (PR-3b, "Ack writer" below); off, the Ack issues
no ledger SQL. `Enabled` parses the switch for all three binaries, so they
cannot disagree on a value. The read path below is dark behind its own
switch, `ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED` (PR-3c, `ReadEnabled`).

## Tables (migration 136)

| Table | Holds |
| --- | --- |
| `changed_since_activations` | One row per activation, ordered by `activation_seq`. `source` is `sweeper`, `backfill` or (PR-3b) `ack`. |
| `changed_since_key_state` | The effective key state of a scope at its cursor's `state_generation_id`: one row per live key with a SHA-256 multiset digest. |
| `changed_since_scope_cursor` | The per-scope writer fence and the generation the state describes. |
| `changed_since_links` | One row per link, with the linked generation's key counts. |
| `changed_since_link_deltas` | The changed keys of one link, with their classification. |
| `changed_since_link_bucket_counts` | Key counts per (category, classification) of one link. |

None of the tables has a foreign key. A foreign key would make every inserted
row take `KEY SHARE` on the `ingestion_scopes` row for the whole link, which
is the multixact shape that deadlocked the projector claim in #7115.

## Link transaction

`LinkWriter.LinkNext(scope)` runs one transaction per activation:

1. Lock the cursor row `FOR UPDATE SKIP LOCKED`. No row means another writer
   holds the scope: `RetryError{cursor_locked}`.
2. Read the scope's oldest activation above the cursor. None: idle.
3. Read the generation. If it is absent, the break is `pruned_before_link`.
   Otherwise set the transaction-local `lock_timeout` to
   `generationLockTimeout` (250 ms) and lock the generation `FOR KEY SHARE
   SKIP LOCKED`; no row means retention holds it: `generation_locked`.
   SQLSTATE 55P03 means the lock waited 250 ms in PostgreSQL's update-chain
   walk (see "Generation locks are bounded") or on a table lock:
   `generation_lock_timeout`.
4. For a delta generation, record a chain break (`delta_without_root`,
   `prior_mismatch`, or `overlay_unproven`), advance the cursor, and keep the
   state.
5. For a full generation with a state at another generation X (the link's
   prior), fence X (arbiter ruling arb-7127-3d, C1): a plain existence read
   of X, then, if X exists, `FOR KEY SHARE SKIP LOCKED` on it under the same
   timeout. No row back means retention holds X: `generation_locked`; 55P03
   is `generation_lock_timeout`. If X no longer exists, retention pruned it
   and the link is a rebase (step 6).
6. Take one of `Slots` advisory slots (`slot_busy` when none is free). Then
   `SET LOCAL lock_timeout = 0`, `work_mem = '256MB'`, `plan_cache_mode = force_custom_plan` and
   `statement_timeout`, and run one statement: `RootLinkSQL` when there is no
   state yet, `RebaseLinkSQL` when X was pruned, otherwise
   `IncrementalLinkSQL`.
7. Advance the cursor. Commit.

The lock order is cursor, activating generation, prior, slot. The cursor
lock and the slot never wait; each generation lock waits at most 250 ms.
Generation retention never waits on anything a link holds, so no lock order
with retention can deadlock.

**Rebase** (C2). `RebaseLinkSQL` shares the incremental statement's diff and
state move (`stateDiffCTE`, `stateMoveCTEs`), so it writes exactly the keys
an incremental link of the same pair would write, never the whole scope. It
records the link as `root` with an empty prior and writes no link delta or
bucket row, because the only prior it could name is gone. The result is
`Kind` root with `Break` `prior_pruned` and `RebasedFrom` X. A read from X
answers `retention_expired` anyway: X is resolved from `scope_generations`.

Outcomes (#7127 ruling 8.10):

- **Non-counting, no write:** `cursor_locked`, `generation_locked` (the
  activating generation or the prior), `generation_lock_timeout` (a
  generation lock waited 250 ms and gave up with 55P03; the journal pass
  reports it too), `slot_busy` (`*RetryError`, which satisfies the reducer's
  `contract.RetryableError`; `RetryError.SQLState` carries 55P03).
- **Counting:** a failure once the link statement ran (`*FailureError`):
  `statement_timeout` (57014 or the transaction deadline), `connection_lost`
  (57P01-57P03, class 08, a broken connection), `sql_error`, `internal`. A
  failure of begin or of the lock reads counts nothing.
- `RecordFailure` counts a counting failure in a second short transaction
  under the cursor lock, only if the head is still the failed activation:
  `attempt_count + 1`, `next_attempt_at = now + min(30 min, 30 s * 2^(n-1))`.
  At the limit (default 5) the activation becomes a `link_poisoned` chain
  break: the cursor advances past it, the state stays, and
  `poisoned_activation_seq` and `poisoned_at` mark the scope until its next
  full link clears them.
- `LinkResult.Phases` (`LinkPhases`, `phases.go`) splits `Duration` into
  `EnsureCursor`, `Begin`, `Locks`, `Statement`, `Advance` and `Commit` (#9275).
  `Locks` is every read and lock before the link statement or an early
  advance; `Statement` is zero when none ran. The phases are laid end to end on
  one monotonic clock, so `Sum()` never exceeds `Duration`; the rest is Go code
  between the calls. The runner logs them next to `duration_seconds`.
- The whole transaction has a context deadline of the statement timeout plus
  30 s (`LinkWriter.TransactionDeadline` overrides it; the stall test uses 2 s). The candidate list is a hint; the head is re-read under the lock, and a
  head still backing off returns `Idle` with `Deferred`.

## Read path (PR-3c)

With `ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED=true` (default off), the
changed-since read answers from the ledger instead of diffing two whole
generations. Off, it issues no ledger SQL. The statements live in
`read_sql.go`; the attempt in the parent package tries one link, then a
chain of links under a 200k stored-delta-row budget (`ChainDeltaRowBudget`),
then the legacy statement, with the wire contract unchanged.

- Every statement is a plain SELECT by the full three-column key
  (`scope_id`, `generation_id`, `prior_generation_id`): no row lock, no
  advisory lock, no aggregation by generation alone, so an orphan row naming
  another prior can never leak into a window.
- The chain walk follows `prior_generation_id` pointers from the requested
  prior to the current generation. A gap, a branch, a break, or a rebase
  root on the way is `prior_off_chain`, never composed.
- Counts are exact: composed added/updated/retired/superseded come from the
  link deltas, and unchanged is the final link's live key total minus
  composed added and updated (the G1 oracle shape). Unchanged samples come
  from the key-state rows outside the composed changed set; removed keys go
  through a final-generation tombstone probe, mirroring the legacy tombstone
  join, so retired and superseded match exactly.
- A cursor re-read after the fetch turns a mid-read writer or retention
  commit into `cursor_moved`; a delta-row count that no longer matches the
  links' recorded counts turns it into `ledger_error`. Both fall back to the
  legacy statement with exact rows.
- Every attempt counts `eshu_dp_changed_since_ledger_reads_total` by
  outcome (`ledger_single`, `ledger_chain`, `fallback_<reason>`).

## Journal

`JournalStore.Journal` runs one pass under a try-only advisory lock (slot 0 of
`SlotLockClass`), so passes on different replicas cannot interleave. It first
backfills retained chains for scopes with no journal rows, following
`prior.superseded_at = next.activated_at` from the newest activated full
generation to the active one. Each backfilled row is inserted through its
generation row, `INSERT ... SELECT ... FROM scope_generations ... FOR KEY
SHARE OF generation SKIP LOCKED` (arbiter ruling arb-7127-3d, C3), so it
exists only while the generation does and the pass holds the generation
until it commits. A row that inserts nothing (the generation is held by
retention or already pruned) ends that scope's chain for the pass; the rows
after it would name it as their prior. The inserts run under the generation
lock timeout: a 55P03 rolls the whole pass back as `generation_lock_timeout`,
and the next cycle retries. The timeout is reset after the backfill. Then it journals every active
generation that has no row, with a `NULL` prior, under the same bound: the
sweeper insert locks each generation it names `FOR KEY SHARE OF generation
SKIP LOCKED` until the pass commits (#8932), so retention skips it and its
ledger delete cannot miss the sweeper's row. A generation that retention
holds or has pruned is skipped; the next pass journals whatever is active
then. A scope whose active generation has no generation row is journaled
without a lock, as before. `BacklogScopes`, `Stats` and
`OrphanScopes`/`DeleteOrphanScope` serve the runner.

## Ack writer (PR-3b)

With the switch on, `ProjectorQueue.Ack`
(`go/internal/storage/postgres/projector_queue_ack.go`) writes the scope's
activation row in its own transaction, so the link writer does not wait for
the sweeper. Its supersede of the scope's active generation returns the
superseded generation id: exactly one row is the prior, none or several give
a `NULL` prior, as the sweeper writes. Its activate statement becomes one
statement with three CTEs: the activate `UPDATE ... RETURNING`, a
`MATERIALIZED` gate that evaluates `AckJournalGateSQL` once for the activated
row, and an `INSERT ... ON CONFLICT (scope_id, generation_id) DO NOTHING` of
`source = 'ack'` that runs only when the gate was acquired. `activated_at`
is the generation's own activation time.

**The journal key is shared with the Ack, exclusive for the pass, and
neither waits.** `AckJournalGateSQL` is
`pg_try_advisory_xact_lock_shared(SlotLockClass, 0)`; the journal pass takes
`pg_try_advisory_xact_lock(SlotLockClass, 0)`. Acks share the key with each
other, and a pass excludes them all. An Ack that misses the gate (a pass is
running) writes no row and does not wait; the next pass's sweeper journals
the generation with a `NULL` prior. That is accuracy-neutral: a full link is
computed from the cursor state, and the prior only feeds the delta break
reasons. A pass that misses the key skips its cycle, as it does when another
replica holds it, and returns `JournalResult{Skipped: true}`.

**Skip bound.** A pass tries the key once per cycle and never waits. So the
chance that one try misses is the Ack duty cycle: the share of time that at
least one Ack transaction holds the key shared. A projector that acks often
makes skips more frequent, but each poll is a new try. The pass starves only
while Ack windows cover all time, for example during a bootstrap burst. While
the pass is starved, each Ack still journals its own activation, so the link
writer keeps up with new activations. The work that waits is the backfill of
retained chains and the sweeper's rows for generations activated without an
Ack row (switch off, or before the switch was on). The backlog and lag gauges
count only journaled rows, so they do not show that waiting work. The runner
counts each skip as `eshu_dp_changed_since_link_retries_total{reason="journal_busy"}`
and logs the skip streak (`changed-since journal pass skipped`, with
`skipped_cycles` and `skipped_seconds`, at most once a minute, then
`changed-since journal pass resumed after skips`). With one replica, a
`journal_busy` rate equal to the cycle rate, or a skip line whose
`skipped_seconds` keeps growing with no resume line, means the pass is
starved. With several replicas, check that the streak grows on every one.

Why the gate. Without it, a pass can run between an Ack's insert and its
commit. If the scope's active generation has no journal row (it was
activated by an Ack with the switch off, or before the switch was turned
on), the backfill or the sweeper inserts that older generation with a higher
`activation_seq` than the Ack's row and commits first. The cursor then ends
on the superseded generation while the newer one is active. A plain insert
after the activate statement and an ungated CTE both fail this way
(`TestProjectorAckJournalInversion`).

Lock rules. The Ack holds the scope row `FOR UPDATE` from its first
statement, so Acks of one scope are serialized and retention, which locks
scope rows `FOR UPDATE SKIP LOCKED`, skips the scope until the Ack commits.
The Ack's own `UPDATE`s hold both generations its row names (the activated
generation and the prior), which satisfies the writer rule below. The Ack
never writes attempt columns and never reads the cursor, so it takes no lock
the link writer holds.

Telemetry: `eshu_dp_changed_since_ack_journal_total{outcome}` (`journaled`,
`exists`, `gate_skipped`), counted by the projector queue after the Ack
commits, and an INFO log with `scope_id` and `generation_id` for a gate
skip. The log is sampled: the first skip in the process logs, then at most
one a minute, each line with `gate_skipped_since_last_log` (the skips not
logged since the last line). The counter counts every skip; read rates from
it, not from the log.

## Retention

Generation retention (`go/internal/storage/postgres/generation_retention.go`)
prunes the ledger in its own prune transaction (#7127 ruling 2.8 and arbiter
ruling arb-7127-3d), through `PrunedGenerationRowCounts` and
`DeletePrunedGenerationRows`. Both take the candidates' scope ids and
generation ids as parallel arrays.

- A link, with its link deltas and bucket counts, is deleted when its
  `generation_id` or `prior_generation_id` is a pruned generation.
- The activation rows of the pruned generations are deleted, including rows
  above the cursor. An activation is deleted only for its own
  `generation_id`, never for its `prior_generation_id`; the writer compares
  that column as a string and never joins it.
- `changed_since_key_state` and `changed_since_scope_cursor` are never read or
  written by retention.
- The delete runs whatever `ESHU_CHANGED_SINCE_LINK_ENABLED` says. Rows
  written while the switch was on are pruned after it is turned off.

**One statement.** The links, their deltas, their bucket counts and the
activation rows are deleted by one `WITH`. Every CTE of a statement shares one
snapshot. As separate statements, a link that commits between the delta
delete and the link delete loses its link row and keeps its deltas, and no
rule reading pairs from `changed_since_links` can find those deltas again
(`TestSplitLedgerDeleteLeaksHeadlessDeltas`). With one statement a late link
survives whole. `RowsPruned` for the four tables is what that statement
returns; the pre-count only feeds `BatchRowLimit` and the events, and a
difference between the two is logged as a WARN.

**Counts.** The pre-count runs with retention's other row counts, so
`BatchRowLimit` covers the ledger. A link naming two pruned generations is
charged to the newer. Link deltas are counted from
`changed_since_links.delta_rows`, which the link statement writes in the same
statement as the delta rows. The limit caps a batch of two or more
generations (arbiter ruling arb-7127-3d-b). A candidate over it only because
of its ledger rows is pruned in a batch of its own, exceeding the limit by at
most its own ledger rows; while another batch is being filled it is deferred
with reason `row_limit_ledger` and leads a later batch. So a link larger than
the limit no longer holds the generations it names
(`TestRetentionRowLimitDoesNotStarveLedgerHeavyGenerations`). A candidate
whose own rows outside the ledger exceed the limit drains one bounded chunk
per cycle (`row_limit_chunked`, #7981) instead of skipping forever: the
pre-screen diverts it past the full count, and the generation row goes in a
later cycle once the remainder fits, with the ledger-before-generations
order intact. `row_limit_own_rows` remains only for the recount race. The
deferred rest keep `row_limit`. Every pass selects inside a
savepoint, rolls back to it (which releases the selection's row locks),
plans unlocked, and re-locks only the selected set in `(scope,
generation)` order with `SKIP LOCKED` before it recounts and deletes
(arbiter ruling arb-7127-3d-c). After a skip the
batch is recounted and the limit is checked again: a link shared with a
skipped generation is still deleted with the selected one, so its charge
moves there and a recount can grow.

**Plan shape.** Both statements read `changed_since_links` once, by the
`scope_id` prefix of its key with array filters on the generation ids
(globally unique), and reach deltas and bucket counts through `LATERAL`
probes of the full (scope, generation, prior) key prefix, then delete by
ctid. `TestRetentionStatementPlanShape` pins this at 2M delta rows in four
statistics states and three plan-cache modes.

**The writer rule** (arbiter ruling arb-7127-3d, C4). The ruling-2.8 delete
is complete only if every ledger writer holds `FOR KEY SHARE` on every
generation it names until it commits. Then a row naming a generation commits
before retention can lock that generation, and retention's delete statement
sees it. The link writer locks the activating generation and the prior
(`fencePrior`); the backfill inserts through the generation row; the Ack
writes its row while its own `UPDATE`s hold the activated generation and the
prior, under the scope row. **Any later
writer of a ledger row, `pairwise` (PR-3c) first, must lock both generations
it names the same way**, `SKIP LOCKED` under the generation lock timeout,
after the cursor and before the slot.
A writer that names a generation it did not lock can leave a row that
names a pruned generation, which no retention rule finds again.

With the rule in place no link or activation row names a pruned generation.
Before PR-3e the writer did not lock the prior, and the bound was at most one
such link per scope plus at most one backfill chain per racing episode; any
such rows written then are removed when their other generation is pruned, or
with the scope. The orphan probe (`JournalStore.Orphans`, sampled by the
runner as `eshu_dp_changed_since_ledger_orphans`) watches the rule. It is
non-zero only briefly for a deleted scope, until the orphan-scope purge.

**Generation locks are bounded** (arbiter ruling arb-7127-3e-wait). `FOR KEY
SHARE SKIP LOCKED` can still wait. If a non-key update of the row commits
between the lock statement's snapshot and its row lock, and another
transaction holds the new version `FOR UPDATE` or is deleting it, PostgreSQL
follows the update chain (`heap_lock_updated_tuple`) and waits there without
honouring `SKIP LOCKED`; `NOWAIT` waits in the same place. Every generation
lock therefore runs under a transaction-local `lock_timeout` of 250 ms, below
the server's `deadlock_timeout`, and 55P03 becomes the non-counting
`generation_lock_timeout`. The #7115 multixact shape (a running `KEY SHARE`
member plus a committed updater) does not wait at all: the member's lock
carries to the new version, so retention skips it
(`TestPriorLockWithACommittedUpdaterDoesNotWait`).

**Endings of an activation.** An activation ends as exactly one link, as
exactly one break, or deleted by retention before the writer reaches it (its
generation was pruned). The last ends without a link or a break. A rebase is
a link that also reports the `prior_pruned` break.

Lock interaction: a link transaction holds its activating generation and
its prior `FOR KEY SHARE`, so retention's `FOR UPDATE SKIP LOCKED` candidate
lock skips both until the link ends. The orphan-scope purge only takes scopes with no
`ingestion_scopes` row, and retention holds its scopes' rows `FOR UPDATE`, so
the two never touch the same scope.

## Telemetry

None here. The runner records the metrics, the span and the structured log
(see `go/internal/reducer/freshness/links/README.md`); the projector queue
records the Ack writer's counter ("Ack writer" above).

## Verification

```bash
cd go && ESHU_POSTGRES_TEST_DSN=postgres://... go test ./internal/storage/postgres/freshness/links -count=1
```

The live tests clone a bootstrapped template database per test. They need
PostgreSQL 18.

### Scale harness (G7/G8) load rule

`scale_live_test.go` enforces rule PD of
`docs/internal/timing-proof-rules.md`, mirroring the PD1-PD5 shape of
`docs/internal/evidence/7127-ledger-retention-timing.sh`: the round waits for
load1 below half the CPU count, samples load1 every second through the round,
and runs a base-binary canary each round. The round counts only when every
sample is below the threshold and the canary ran within its bound
(`scale_load_rule_test.go`; unit tests in
`scale_load_rule_unit_test.go` use injected samples).

The canary is the `bare_b` aggregate through a test binary built at the base
ref: `buildScaleBaseBinary` adds a detached worktree at the ref, copies the
current scale-harness test files into it (so the `TestLinkScaleCanary` entry
exists whatever the base predates), and runs `go test -c` on this package.
The worktree is removed by `t.Cleanup`. A failed build fails the test: a
scale run without its control proves nothing.

| Knob | Default | Meaning |
| --- | --- | --- |
| `ESHU_CHANGED_SINCE_LINK_SCALE_LOAD_THRESHOLD` | `NumCPU()/2` | PD headroom bound |
| `ESHU_CHANGED_SINCE_LINK_SCALE_CONTROL_MAX_SECONDS` | `12` | Canary bound, derived from the committed G7 `bare_b` spread (max 8.50s) |
| `ESHU_CHANGED_SINCE_LINK_SCALE_BASE_REF` | merge base with `origin/main`, else `HEAD` | Ref the canary binary is built from |
| `ESHU_CHANGED_SINCE_LINK_SCALE_GATE_FILE` | unset (no record) | PD1 gate file for the `uptime`/`docker ps` record |
