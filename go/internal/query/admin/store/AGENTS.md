# Admin Store — Agent Instructions

Scope: `go/internal/query/admin/store/` (package `store`).

## Ownership

The Postgres implementation of the admin `Store` port: work-item,
dead-letter, input-invalid-fact, decision, replay-event, and backfill
reads/writes, plus the replay idempotency ledger. The port and every
row/filter model stay in the parent `admin` package; this package only
implements them.

## Rules

- Import the parent `admin` package for the port and models. Nothing in
  the parent imports this package (cycle).
- SQL text is wire: table and column names, status strings, and schema
  versions stay byte-identical unless a migration moves with them.
- Live tests live in `admin/live_test.go` (external package), not here:
  `admin`'s internal tests cannot import `store` (import cycle), so live
  seeding through the real constructor lives outside.
