# Admin Postgres Store

The Postgres implementation of the admin `Store` port: durable work-item,
dead-letter, input-invalid-fact, decision, replay-event, and backfill
reads/writes, plus the replay idempotency ledger (`NewStore`).

The port and every row/filter model live in the parent `admin` package;
this package only implements them. Unit tests here run against scripted
queryers; the live Postgres proofs live in `admin/live_test.go`.
