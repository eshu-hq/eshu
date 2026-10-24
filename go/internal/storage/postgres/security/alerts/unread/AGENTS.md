# AGENTS.md — security-alert read state guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `store.go` for the write path and `migrations/217_security_alert_target_read_states.sql`.
3. `../../../ingestion_stream_error.go` for where the writer runs in the commit transaction.

## Invariants

- Write only through `Writer`, and only inside the generation's commit
  transaction. A write outside it can leave a state for a generation that never
  committed, or a committed generation with no state.
- Keep both tables cascading from `scope_generations`. Generation retention
  owns their lifetime; do not add a sweeper, and do not depend on
  `workflow_work_items`.
- Keep the child rows. The allowlist has no size bound, so an array column
  would be read in full on every empty answer; the read path asks about a few
  repositories by primary key.
- Keep the replace semantics (upsert the parent, delete then insert the
  children). A replay of one generation id must never leave rows of the earlier
  write, and the parent `unread_total` must equal the child count.
- Reasons and errors never carry a repository name beyond the row itself; the
  caller's logs and metrics must not either.
- Never import the parent `postgres` package.

## Common changes

- A new reason token changes the collector (`alertruntime`), the query pin
  (`query/supply/chain/security_alert_unread_reason.go`), and the OpenAPI enum
  together.
- A schema change is a new migration plus the checksum manifest, the golden
  digest, `schema_order_late_test.go`, and the retention row-count arms.
