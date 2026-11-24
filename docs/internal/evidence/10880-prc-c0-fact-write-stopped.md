# PR-C0: a refused finding-fact write retries without spending an attempt (#10880)

PR-C arms a database trigger that refuses every INSERT and UPDATE of a
`reducer_supply_chain_impact_finding` row in `fact_records` (SQLSTATE 55000,
constraint `fact_records_reject_supply_chain_impact_finding`). This change
teaches the reducer's impact transaction to recognise that refusal before the
trigger exists, so the image that runs just before the stop already handles it.

## What the code does

`supplyChainImpactTx.ExecContext` and `QueryContext` pass a statement error
through `classifySupplyChainImpactFactWriteStopped`. Only SQLSTATE 55000 with the
trigger's constraint name becomes a `SupplyChainImpactFactWriteStoppedError`
(retryable, class `supply_chain_impact_fact_write_stopped`, not an expected wait).
Any other error, including another 55000, is returned unchanged. The class is in
`nonCountingReducerRetryFailureClasses` and `WaitingDeferralMap`, so the queue
defers the intent about every 60 seconds with its attempt count unchanged. Each
classified refusal adds one to `eshu_dp_supply_chain_impact_fact_write_rejected_total`.
The counter has no label.

Nothing raises the error until PR-C2 adds the trigger. A statement that succeeds
runs no new code; a statement that already failed runs one `errors.As`.

## Evidence

No-Regression Evidence: baseline `origin/main` f1f45ddad3, after this branch; PostgreSQL
statements unchanged apart from the class list, so there is no new plan to measure; input
shape: the reducer claim statements with one more string in their non-counting `IN` list;
terminal row counts: not applicable, no queue row changes state. The change adds no statement, query, lock, claim, lease,
batch or worker path. The queue claim statements embed the non-counting class
list, so one more class adds one string to an `IN` list those statements already
carry; the stale-gate guard `TestStaleGate11234LeavesEveryOtherByteUnchanged`
shows the funnel statement byte-identical and the batch and single statements
changed only by that list (their two pinned digests are re-pinned). The only
code on a hot path is an `errors.As` on a statement error, taken only after a
statement has already failed, so a healthy writer pays nothing.

Observability Evidence: `eshu_dp_supply_chain_impact_fact_write_rejected_total`
(no labels; one per refused statement), documented in
`docs/public/reference/telemetry/supply-chain-impact-supports.md` under "Refused
fact writes" and in the coverage row of
`docs/public/observability/telemetry-coverage-reducer-domains.md`. At 3 AM, a
rising count after PR-C2 means a reducer pod runs an image older than the schema;
the same pod's intents show as retrying under failure class
`supply_chain_impact_fact_write_stopped` on the queue's failure-class series, and
the service reports the refusal as a deferral (execution status `deferred`, a WARN
log), because every retryable error in the non-counting class set is deferred
(`go/internal/reducer/service_observability.go`, `isDeferredFailure`, wired at
`go/cmd/reducer/main.go` to `IsNonCountingReducerRetryFailureClass`). The counter
is the primary signal.

Roll order for C2: the retry is not bounded in elapsed time. The class is
non-counting, so the attempt count stays fixed and the delay stays near 60 s; it is
never parked and never dead-letters, and it is not a hot loop, but a pod on a C0
image re-runs the full pass about every 60 s per intent until the rollout ends. A
pod older than C0 does not know the class: it counts the attempt and dead-letters.
Roll old pods to zero before C2 arms the trigger.

Write-path census: `eshu_security.publish_impact_publication_v1` (migration 269)
inserts only `scanner_*` tables and never `fact_records`, so it cannot raise this
error. The impact writer and `cmd/ifa` both write through
`supplyChainImpactTx`. C2 re-runs the write-path census.

Tests: `TestSupplyChainImpactFactWriteStoppedClassification`,
`TestSupplyChainImpactFactWriteStoppedIsNonCounting`,
`TestSupplyChainImpactTxCountsOneRejectionPerStoppedWrite`,
`TestRecordSupplyChainImpactFactWriteRejected`. Removing the classify call or the
counter call from the transaction fails the wiring test.
