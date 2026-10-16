# Evidence: #7926 — structured terminal failure classification needs no performance regression proof

Scope: classification persistence ONLY (this lane). Claim mutations now carry
the bounded structured classification (operation, status code, failure class,
terminal flag) alongside the message, persisted on `workflow_claims` and
`workflow_work_items` (migrations 184-185). No freshness-hint or snapshot
change; the separate diagnosis lane owns that.

No-Regression Evidence:
The hot statements keep byte-identical predicates, CTE structure, and lock
clauses; only SET assignments widen. `failWorkflowClaimTerminalQuery` and
`failWorkflowClaimRetryableQuery` still select the candidate with
`FOR UPDATE OF item, claim`, fence on claim_id, fencing_token, owner_id, and
`status = 'claimed'`/`'active'`, and write both rows in the same single
statement. Complete and release clear the new columns with NULL literals, so
stale classification never survives completion or release. No index was
added (predicates unchanged; low-cardinality status/terminal columns alone
do not justify one), no worker count, batch size, lease TTL, heartbeat, or
`visible_at` computation changed, and nothing was serialized.
Baseline: base `a9fe1ec6e`. After: the #7926 classification commits.
Backend: hermetic `fakeExecQueryer` (no live Postgres locally;
`ESHU_POSTGRES_DSN` unset, so the live round-trip test
`mustWorkflowStructuredFailure` skips here and runs in CI).
Input shape: one terminal mutation
(class `terminal`, operation `collect_failure`, status 400, flag true) and
one retryable mutation (class `rate_limited`, status 429, flag false).
Measured: `FailClaimTerminal` issues exactly 1 exec with 10 dense args
($1-$10, no $11 hole); `FailClaimRetryable` issues exactly 1 exec with 11
dense args (no $12 hole); 0-rows-affected still returns
`ErrWorkflowClaimRejected` (stale-claim tests green). Full suites at the
commit: `go test ./internal/collector/sdk ./internal/collector
./internal/workflow ./internal/storage/postgres/migrations` and full
`./internal/storage/postgres` — all exit 0; `go vet` on the touched trees
exit 0; `gofumpt` clean. Local EXPLAIN was not run (no live backend); the
only remaining delta is wider SET tuples (three small columns), and the CI
live lane owns that measurement alongside the round-trip replay.

Observability Evidence:
No new metric, span, or log line was added: the per-claim run duration,
retry counters, and claim spans keep their existing labels (the operation
token stays out of metric dimensions to avoid cardinality). The new
operator signal is queryable columns. Terminal classification for one item
and its claim reads back with:

```sql
SELECT last_failure_operation, last_failure_status_code, last_failure_terminal
FROM workflow_work_items WHERE work_item_id = $1;
SELECT failure_operation, failure_status_code, failure_terminal
FROM workflow_claims WHERE claim_id = $1;
```

Legacy rows read NULL (unknown); new terminal rows carry
(`collect_failure`, 400, true) next to the bounded message, so consecutive
400s stay distinguishable without exposing URLs, bodies, or credentials.
