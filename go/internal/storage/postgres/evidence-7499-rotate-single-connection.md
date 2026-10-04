# Evidence: rotate failed attempts recorded on tx (#7499)

`RotateLocalIdentityPassword` holds its row-locked transaction's pooled
connection while recording the failed attempt. The wrong-password,
wrong-TOTP, and invalid-recovery-code paths wrote that attempt through the
pool, so on a `MaxOpenConns(1)` pool the call asked for a second connection
while holding the only one and waited on itself until the context expired;
writes that did land were discarded by the deferred rollback instead of
committing. The fix writes the attempt on `tx` and commits there, and
`verifyLocalIdentityTOTPCode` runs on the caller's executor (`tx` in rotate,
`s.database` in login). No SQL text, predicate, or index changed: the same
upsert runs on a different executor, so no plan or throughput change is
expected.

No-Regression Evidence: issue #7499 live proof on 2026-10-04 against
PostgreSQL 18.6 (local Compose project `eshu-7498-localid`, isolated schema
via `openIsolatedLiveDB` applying all 178 bootstrap migrations). Baseline:
every failure-path rotation on a single-connection pool self-waited to the
context deadline, so no successful baseline latency exists to regress. After
the fix, `TestRotateLocalIdentityPasswordSingleConnectionCompletes`
passes: 5 sequential rotations on the same `MaxOpenConns(1)` pool (input
shape: single-user fixture, wrong password x2, wrong TOTP with an active
factor, invalid recovery code, then a full success rotation with TOTP
re-proof under one 30s call context) record durable terminal counts
`failed_attempts=2, 3, 4` and `0` after the success clears the lockout, with
per-stage statuses invalid then authenticated. Package wall time 4.331s
including the full bootstrap migration, matching the ledger's ~4.3s anchor.
No benchmark delta is claimed because the pre-fix path never completed.

No-Observability-Change: the fix adds no spans, metrics, or logs; failure
and success paths keep their pre-existing result statuses, and the live
proof asserts per-stage `t.Fatalf` diagnostics with got/want counts
(2, 3, 4, 0).
