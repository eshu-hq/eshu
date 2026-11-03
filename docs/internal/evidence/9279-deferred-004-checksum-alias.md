# #9279: deferred 004 alias — no-regression and observability evidence

Issue #9279: Compose installs bootstrapped before 2026-10-13 recorded the
deferred 004 checksum `027b3563…`; #8028 edited the deferred Go constant in
place, moving the current deferred checksum to `3d8ef124…`. `db-migrate`
aborts on the mismatch before migrations 171–173 run. The fix adds one
`(004, deferred)` alias row and re-keys the alias table by
`(path, variant)`; the single caller passes the ledger variant it holds.

## Classification

Correctness fix on the bootstrap path, not a performance change. The alias
consult sits inside the `recorded != checksum` branch of
`applyTrackedDefinitions`, which previously returned an error
unconditionally. Every path that succeeded before executes byte-identical
code: no new query, no new index, no lock, lease, queue, or worker change.
The added work on the rescued path is one O(1) map lookup over two entries
plus the warn log below.

## No-Regression Evidence: bootstrap timings on the fixed head

Backend: local PostgreSQL 18.6 on a shared host, disposable database,
deferred bootstrap (`ApplyBootstrapWithoutContentSearchIndexes`, 296
definitions). Timings from the live proof run
(`schema_migration_9279_live_test.go`, fresh database):

| Run | Result |
| --- | --- |
| Fresh deferred bootstrap, 296 definitions | applied=296, total about 1.36 s |
| Re-bootstrap, fully recorded ledger | applied=0 skipped=296, about 0.03 s |
| Upgrade with pre-#8028 ledger (alias accept + 171 drop + 172 concurrent rebuild + 173 vacuum) | left the covering index `(language, repo_id) INCLUDE (indexed_at)` in place, test wall 1.38 s |
| Control (ledger already at `3d8ef124…`) and unknown-checksum reject | both behave as before, 0.06–0.07 s |

There is no successful pre-fix baseline for the rescued population: base
code aborts that ledger with `checksum changed`. The comparable operation —
a recorded plan skipping re-apply — costs the same map lookup it always did.
Unit legs: migrations package 9/9 alias tests, 3/3 pin tests, 4/4 #7002
live tests on the same database.

## Observability Evidence: alias acceptance is logged and asserted

Accepting an alias emits `bootstrap.postgres.migration.checksum_alias_accepted`
at Warn with `path`, `variant`, `recorded_checksum`, and `current_checksum`
(`schema_bootstrap_lock.go`). The live upgrade test asserts the event is
present; the reject test asserts the `checksum changed … (deferred)` error
text. No new metrics, spans, or dashboards; no 3 AM runbook change — an
operator seeing the event knows exactly which ledger row was rescued and why.
