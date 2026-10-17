# #7585 PR1 expired-but-blocked reporting: no-regression evidence

Scope: branch `feat/7585-expired-blocked-reporting`. New pure-Go
evaluation (`generation_retention_expired_blocked.go`) of one
superseded generation against the hard history ceiling, plus a
closed blocked-reason vocabulary. Reporting only: no deletion path,
SQL, migration, or default changes. The gate flagged the postgres
package files as hot by path; this note records why the change
cannot regress runtime cost.

No-Regression Evidence (pure reporting vocabulary): the evaluation
is O(1) per generation with no allocation beyond the result struct,
no I/O, no lock, no retry, and no database handle. It runs only
where a future caller invokes it; this PR wires no caller, so
production wall time is unchanged by construction. Baseline:
`go test ./internal/storage/postgres/ -run
TestEvaluateGenerationRetention -count=1` wall 3.3s at HEAD
`bd627acd2` (GOTOOLCHAIN=go1.26.9, darwin/arm64 dev harness, not a
production backend), 9/9 green. Input shape: one generation's
retention flags across active-current, live-work, retrying,
dependency-pin, ceiling-edge, and default-90-day boundaries.
Queue/row counts: not applicable, the evaluation touches no rows.
Change is safe because no existing statement, predicate, or prune
guard is modified; the candidate SQL's guards stay the authoritative
deletion bound.

No-Observability-Change: this PR emits no new spans, metrics, or
logs. The blocked-reason vocabulary is closed so PR3 ceiling
telemetry can label it, but no instrument is added here. No
operator dashboard or alert changes.
