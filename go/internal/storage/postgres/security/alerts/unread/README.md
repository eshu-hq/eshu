# Postgres security-alert unread state

## Purpose

This package stores what the security-alert collector knows about the last read
of one organization target: whether the organization stream stopped at
`max_pages`, how many allowlisted repositories the target covers, and which of
them the per-repository fan-out did not read in full, each with the collector's
closed reason. The query layer joins it on the target's active generation so an
empty answer for an unread repository reports `target_incomplete` with that
reason instead of `complete` (#8943).

## Ownership boundary

- Owns the two tables from `migrations/230_security_alert_target_read_states.sql`:
  `security_alert_target_read_states` (one row per generation, primary key
  `generation_id`) and `security_alert_unread_repositories` (one row per unread
  repository, primary key `(generation_id, repository)`).
- `Writer(state)` is the only write path. It validates the state, lowercases
  each repository slug the way the read probe does, upserts the parent row,
  replaces the generation's child rows, and returns errors that carry no
  repository name.
- Migration `232_security_alert_fanout_cursor.sql` adds the nullable
  `fanout_start_repository`, `fanout_next_repository`, and
  `fanout_lap_repository` columns to the parent row (#8946). `Writer` stores
  them (NULL for the top of the allowlist and for no lap).
  `CursorReader.FanoutCursor(scopeID)` returns `fanout_next_repository` and
  `fanout_lap_repository` of the newest pending-or-active generation of the scope, ordered as
  `activeGenerationFreshnessQuery` orders (not `ingestion_scopes.active_generation_id`),
  and "" for no generation or no cursor; a query error is returned, never
  answered with "". `FanoutCursorQuery` is exported for the live plan proof.
- The collector (`internal/collector/securityalerts/alertruntime`) builds the
  state; `cmd/collector-security-alerts` passes `Writer` to the source; the
  ingestion store (`storage/postgres.IngestionStore.CommitClaimedScopeGenerationWithState`)
  runs it in the generation's commit transaction. The reader is
  `query.PostgresSecurityAlertCollectionStateStore`.

## Lifecycle

Rows are written in the generation's own transaction, so there is no state for
a generation that did not commit and no committed generation without its state
(a generation committed before migration 230 has none, and the read path treats
that as nothing unread). A replay of a still-pending generation id replaces the
child rows. Both tables cascade from `scope_generations`; the generation
retention row-count query counts them so batching sees the cascade.

## Operations

Find the targets whose latest read was cut short:

```sql
SELECT s.scope_id, s.org_read_truncated, s.repositories_total, s.unread_total, s.unread_reasons
FROM security_alert_target_read_states AS s
JOIN ingestion_scopes AS sc ON sc.active_generation_id = s.generation_id
WHERE s.unread_total > 0 OR s.org_read_truncated
ORDER BY s.unread_total DESC;
```

## Proof

`alert_fanout_cursor_live_test.go` (parent `postgres` package) proves the
cursor on Postgres: commit and read, a newer generation without a read state,
rollback, the unchanged-generation skip, replay, two generations racing from the
same start, and the cursor read's plan (an index range on the scope plus a
primary-key lookup, 0.044 ms on 20,000 generations).

`alert_read_state_commit_live_test.go` and
`alert_read_state_concurrency_live_test.go` (in the parent `postgres`
package, so the readiness shards price them with its other live proofs) run against a disposable
Postgres 18 (`ESHU_POSTGRES_DSN`): commit with state, replay, rollback,
retention cascade, retention against concurrent commits of the same target,
concurrent claims of one target, and two claims racing on one generation id,
under the race detector. See
`docs/internal/evidence/8943-security-alert-org-read-state.md`.
