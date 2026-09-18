# Admin Postgres Store

The Postgres implementation of the admin `Store` port: durable work-item,
dead-letter, input-invalid-fact, decision, replay-event, and backfill
reads/writes, plus the replay idempotency ledger (`NewStore`).

The port and every row/filter model live in the parent `admin` package;
this package only implements them. Unit tests here run against scripted
queryers; the live Postgres proofs live in `admin/live_test.go` and focused `admin/*_live_test.go` files.

Admin replay and dead-letter update the container-image identity v2/v3
status authorizations in the same row update as `status`. The cutover checks
require equality for guarded rows; leave claim epoch and last-attempt history
for the next reducer claim to advance. The shared UPDATE rechecks terminal status
after locking the row so a concurrent claim cannot be reset by stale selection.
