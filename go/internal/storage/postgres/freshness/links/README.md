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

## Telemetry

None here. The runner records the metrics, the span and the structured log
(see `go/internal/reducer/freshness/links/README.md`).

## Verification

```bash
cd go && ESHU_POSTGRES_TEST_DSN=postgres://... go test ./internal/storage/postgres/freshness/links -count=1
```

The live tests clone a bootstrapped template database per test. They need
PostgreSQL 18.
