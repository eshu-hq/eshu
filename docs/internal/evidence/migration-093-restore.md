# Migration 093 Restore

Issue #7002: #6785 and #6923 edited migration
`093_cross_scope_completion_queue.sql` after it had shipped and been applied.
The bootstrap checksum guard in `applySchemaMigrations`
(`go/internal/storage/postgres/schema_bootstrap_lock.go`) then refused to run
on any database that had recorded the original 093. On ops-qa that stopped
schema bootstrap at migration 111, so migrations 112-119 never ran and the
`sha-a49323c` rollout's pods failed `/readyz`. The change restores 093 to its
shipped bytes (sha256 `c95cae27…`) and accepts the two edited checksums
(`6cdb3e58…`, `7f73153b…`) as narrow aliases for 093 only. It also adds a
blocking gate, `scripts/verify-migration-immutability.sh`, that refuses to
modify, delete or rename a shipped migration.

No-Regression Evidence: against live Postgres 16 (`-tags integration`),
`TestBootstrapAcceptsLedgerRecordingShippedChecksum093Live` fails on the pre-fix code with
`checksum changed: recorded c95cae27…, current 7f73153b…`. On this branch it passes, and
migrations 112-120 apply (`applied=25 skipped=116`).
`TestBootstrapAcceptsSupersededChecksumAliasesFor093Live`,
`TestBootstrapRejectsUnknownChecksumFor093Live` and
`TestBootstrapAliasDoesNotLeakToOtherPathLive` pass.
`TestBootstrapFreshInstallMatchesPreFix093Live` shows that a fresh install
(093 -> 112 -> 120) produces the same
`cross_scope_completion_events_producer_domain_check` definition and the same
trigger definition as `origin/main`. The same test run on an `origin/main`
worktree also passes, which confirms the expected values. The immutability gate's self-test
covers edit, delete, rename, add, the 093 restore, a branch behind main, an
unresolvable base, a shallow checkout with no base, and an unresolved
`GITHUB_BASE_REF`. It exits 0, and each new case failed on the previous
script. The bootstrap path adds one map lookup, and only on the
checksum-mismatch branch, so bootstrap cost is unchanged.

Observability Evidence: accepting an alias logs WARN
`bootstrap.postgres.migration.checksum_alias_accepted` with `path`, `variant`,
`recorded_checksum` and `current_checksum`, and it repeats on every bootstrap
because aliased ledger rows are not rewritten. The live alias test asserts the
event. It is documented in `docs/public/deployment/service-runtimes-bootstrap.md`.
