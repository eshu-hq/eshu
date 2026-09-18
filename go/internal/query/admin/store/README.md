# Admin Postgres Store

The Postgres implementation of the admin `Store` port: durable work-item,
dead-letter, input-invalid-fact, decision, replay-event, and backfill
reads/writes, plus the replay idempotency ledger (`NewStore`).

The port and every row/filter model live in the parent `admin` package;
this package only implements them. Unit tests here run against scripted
queryers; live Postgres proofs live in `admin/live_test.go` and focused
`admin/*_live_test.go` files.

Admin replay, dead-letter, and repository skip update the container-image
identity v2/v3 status authorizations in the same row update as `status`. The
cutover checks require equality for guarded rows. All three actions preserve
claim epoch and last-attempt history; a later reducer claim advances them. The shared replay
and dead-letter UPDATE rechecks terminal status after locking the row so a
concurrent claim cannot be reset by stale selection.

Repository skip selects only pending, retrying, or failed rows, up to 100. It
rechecks the selected status and eligibility on the locked target row so an
in-flight worker claim or a concurrent status change is not overwritten.
