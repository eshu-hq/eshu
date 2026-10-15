# 7852: bound refinalize claim-fence contention

## No-Regression Evidence:

Same real-Postgres regression command and timing boundary before and after:
start the timer when an unrelated `UPDATE fact_work_items` is issued while a
separate transaction holds `ROW EXCLUSIVE` on that table; stop when the update
returns, and verify the holder still owns its lock at that point. Before the
fix, that update waited 1.504933211 seconds and failed the 1.5-second ceiling.
After the fix, it completed in 1.004146947 seconds while the holder retained its
lock. The fence's first 1-second `lock_timeout` raised 55P03, the failed
transaction rolled back, and a retry completed after the holder was released.

Backend/version: PostgreSQL 18.3 (Debian 18.3-1.pgdg13+1), disposable local
Docker database with the repository's 196 migrations applied. Input shape:
one recovery scope, one held queue row, one unrelated queue row, and one active
relationship generation. This isolates the lock-queue convoy and does not claim
an end-to-end rebuild speedup. The issue's larger QA reproduction remains the
context: an 8-second holder made the fence wait 6.975 seconds and an unrelated
update wait 5.963 seconds.

## Observability Evidence:

The recovery store logs each fence timeout and retry at INFO with attempt,
backoff, elapsed contention time, and SQLSTATE. Exhaustion logs WARN with
`outcome=blocked/claim_fence_busy`, attempts, elapsed time, and SQLSTATE. Both
admin refinalize routes return HTTP 409 with that stable outcome after the
30-second bounded contention budget. The API request duration/error metrics,
existing queue-depth metrics, and existing worker lease signals remain
available to correlate the event. No new metric or span is needed for this
operator-triggered recovery path.
