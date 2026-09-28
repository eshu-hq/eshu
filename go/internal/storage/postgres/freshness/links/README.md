# Changed-since link ledger store

## Purpose

This package persists the changed-since link ledger of #7127. The reducer's
`changed_since_link` runner (`go/internal/reducer/freshness/links`) calls it to
turn each generation activation into a link: the keys that changed between
the scope's previous linked generation and the activating one. A later read
path (PR-3c) answers `get_changed_since` from those links in O(changes)
instead of diffing two whole generations.

The store is dark. Nothing reads the ledger yet, and the runner is off unless
`ESHU_CHANGED_SINCE_LINK_ENABLED=true`.

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
   Otherwise lock it `FOR KEY SHARE SKIP LOCKED`; no row means retention holds
   it: `generation_locked`.
4. For a delta generation, record a chain break (`delta_without_root`,
   `prior_mismatch`, or `overlay_unproven`), advance the cursor, and keep the
   state.
5. For a full generation, take one of `Slots` advisory slots
   (`slot_busy` when none is free). Then `SET LOCAL work_mem = '256MB'`,
   `plan_cache_mode = force_custom_plan` and `statement_timeout`, and run one
   statement: `RootLinkSQL` when there is no state yet, otherwise
   `IncrementalLinkSQL`.
6. Advance the cursor. Commit.

Outcomes (#7127 ruling 8.10):

- **Non-counting, no write:** `cursor_locked`, `generation_locked`, `slot_busy`
  (`*RetryError`, which satisfies the reducer's `contract.RetryableError`).
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
- The whole transaction has a context deadline of the statement timeout plus
  30 s (`LinkWriter.TransactionDeadline` overrides it; the stall test uses 2 s). The candidate list is a hint; the head is re-read under the lock, and a
  head still backing off returns `Idle` with `Deferred`.

## Journal

`JournalStore.Journal` runs one pass under a try-only advisory lock (slot 0 of
`SlotLockClass`), so passes on different replicas cannot interleave. It first
backfills retained chains for scopes with no journal rows, following
`prior.superseded_at = next.activated_at` from the newest activated full
generation to the active one. Then it journals every active generation that
has no row, with a `NULL` prior. `BacklogScopes`, `Stats` and
`OrphanScopes`/`DeleteOrphanScope` serve the runner.

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
whose rows outside the ledger exceed the limit is still skipped
(`row_limit`, ADR #2248). Such a batch of one holds one scope row and one
generation row while it deletes: the retention store selects inside a
savepoint, rolls back to it (which releases the selection's row locks), and
re-locks only that candidate with its own targeted query before it recounts
and deletes (arbiter ruling arb-7127-3d-c). After a skip the
batch is recounted and the limit is checked again: a link shared with a
skipped generation is still deleted with the selected one, so its charge
moves there and a recount can grow.

**Plan shape.** Both statements read `changed_since_links` once, by the
`scope_id` prefix of its key with array filters on the generation ids
(globally unique), and reach deltas and bucket counts through `LATERAL`
probes of the full (scope, generation, prior) key prefix, then delete by
ctid. `TestRetentionStatementPlanShape` pins this at 2M delta rows in four
statistics states and three plan-cache modes.

**The bound** (arbiter ruling arb-7127-3d, rationale 2). The link writer locks
the activating generation, not the link's prior, so a link can be written
after its prior was pruned: in a race, or when the writer lags past the
retention window. Per scope, at any time:

- At most one link whose prior is gone, with its deltas (at most the keys of
  X and G together) and its bucket counts (at most 18 rows). It is removed
  when its `generation_id` is pruned. Exactly one incremental link in a
  scope's history has prior X, and the next link has prior G, which can be an
  orphan only if G was pruned first, and that batch deleted `(X -> G)` by the
  generation side. This holds for the incremental chain only; a `pairwise`
  writer that does not lock both ends breaks it.
- Activation rows from a backfill that raced a prune: at most one chain (64
  rows) per backfill episode, removed only when the scope is purged.

Such a row is accurate, not wrong: the state still is the effective state at
X, so `(X -> G)` is the true diff of the pair. It is unreachable from a live
read, because the since generation is resolved from `scope_generations`, so a
pruned generation can never be the since generation, and every link whose
`generation_id` is X was deleted with X. The fix at the source (the writer
fencing the prior) is PR-3e.

**Endings of an activation.** An activation ends as exactly one link, as
exactly one break, or deleted by retention before the writer reaches it (its
generation was pruned). The last ends without a link or a break.

Lock interaction: a link transaction holds its activating generation
`FOR KEY SHARE`, so retention's `FOR UPDATE SKIP LOCKED` candidate lock skips
it until the link ends. The orphan-scope purge only takes scopes with no
`ingestion_scopes` row, and retention holds its scopes' rows `FOR UPDATE`, so
the two never touch the same scope.

## Telemetry

None here. The runner records the metrics, the span and the structured log
(see `go/internal/reducer/freshness/links/README.md`).

## Verification

```bash
cd go && ESHU_POSTGRES_TEST_DSN=postgres://... go test ./internal/storage/postgres/freshness/links -count=1
```

The live tests clone a bootstrapped template database per test. They need
PostgreSQL 18.
