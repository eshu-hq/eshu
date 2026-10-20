# Evidence: #8602 trigger dedup keeps one retirement trigger plus sentinel errors

## What changed

PR #8626 merged the operator trigger as `POST
/api/v0/admin/repository-retirements` (`eshu admin repository retire`,
`cli/admin Retire`, `query/admin/retirement` subpackage). This branch
carried a divergent second trigger (`POST
/api/v0/admin/repositories/retire`, `eshu admin retire-repositories`,
`RetireRepositories`); the dedup deletes the second handler, route,
OpenAPI fragment, CLI command, client, tests, and trigger doc, and
reverts the duplicate wiring, failure strings, spec hunks, inventory
row, and docs to the merged base.

Two deltas survive. The CLI mints the idempotency key before the POST
and prints it to stderr (P1: a post-fence failure no longer hides the
retry key), with a regression test on the merged command. The purge
refusal exits carry `ErrRetirementNoOpenTombstone` and
`ErrRetirementTombstoneNotFenced` via `%w`, pinned by a unit test and a
fence-then-purge sequence live test.

## Why no regression

No-Regression Evidence: the sentinel change alters error values only.
No SQL text changes, no new statement, no plan or index change, no
worker, lease, batch, or concurrency knob change. The wrong-phase wrap
appends `: %w` (former text preserved as a prefix); the no-open-tombstone
line now reads `retirement purge %s: retirement purge: no open tombstone`
(formerly `retirement purge %s: no open tombstone`). Verified by grep that
no consumer matches purge error text: the operator trigger maps any purge
error to a failed outcome retried with the same key, and the old-string
literal in the base trigger test is a scripted fixture, never a matcher. Proof: `go test
./internal/storage/postgres/ -run 'Retirement|Retire'` green (includes
`TestPurgeRefusalsAreSentinelMapped`, which also pins rollback on both
refusals), `go test ./internal/query/ ./internal/query/admin/
./internal/query/admin/retirement/` green, the sequence live test green
on disposable PostgreSQL 18 during the lane (ledger row in
`specs/live-tests.d/live-tests-06.yaml`), and the scheduled-ledger pin
test green at count 382.

No-Observability-Change: no metric, span, log line, audit reason code,
or status value is added, removed, or renamed; the trigger's counters,
WARN/INFO lines, and `admin_retirement_action` events are the merged
base's unchanged.
