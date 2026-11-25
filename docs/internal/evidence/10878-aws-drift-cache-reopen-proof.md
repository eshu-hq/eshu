# #10878 AWS drift evidence cache invalidation and reopen fingerprint clear: no-regression evidence

Refs eshu-hq/eshu#10878, eshu-hq/eshu#5848, eshu-hq/eshu#7635.
`AWSDriftEvidenceCache` previously stored `state_pending` loads under an
AWS-only key with no invalidation on defer, `audit_due`, fallback, or write
error, and `SettleClaimedProducerActivation` reopened `aws_cloud_runtime_drift`
work items without clearing `aws_drift_reopen_fingerprints`. Both defects caused
stale cross-scope state to persist after Terraform state activated.

No-Regression Evidence: baseline before change on steady-state non-pending runs
serves `LoadShared` cache hits and incremental `AWSDriftIncrementalModeSkip`
paths unchanged. After change, steady-state non-pending runs keep the exact
same cache-hit and skip path (`go test -race -count=1 ./internal/reducer/awscloud/...`
green in 2.352s, including `TestAWSDriftEvidenceCacheInvalidateDropsStoredEntryAndUpdatesBudget`
proving in-flight coalescing is preserved and `storedBytes` budget stays exact);
only `state_pending` defers, 24h `audit_due`/fallback full runs, write errors,
and producer-activation reopens bypass or evict the cached entry. On the
producer-activation path, `DELETE FROM aws_drift_reopen_fingerprints WHERE work_item_id = $1`
runs by primary key inside the existing `settle.Tx()` transaction only when a
reopened dependent has domain `aws_cloud_runtime_drift`, preserving the
`fact_work_items` -> `aws_drift_reopen_fingerprints` lock order.
Backend/version: PostgreSQL 18 (`postgres:18-alpine`), disposable proof
database with full 375-migration schema. Input shape and live proofs:
`TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive` (5.76s),
`TestAWSDriftReopenFingerprintAuditClockLive` (4.31s),
`TestAWSDriftReopenCandidatesAuditStaleLive` (7.65s),
`TestProducerActivationRunnerStoreSettlesThroughThePort` (6.83s),
`TestProducerDependentListingCostLive` (6.01s), and
`TestProducerRemovalOnlyGenerationReopensConsumersLive` (4.32s) all passed with
0 stale-cache skips and exact terminal work-item/fingerprint row counts.
Telemetry/log/status evidence: existing `aws_cloud_runtime_drift_state_pending`
defer reason, incremental plan reason labels, and producer-activation settle
counters remain unchanged. Why safe: cache eviction never drops an active
in-flight leader, primary-key delete rolls back atomically with `settle.Tx()`
on error or lease loss, and steady-state hit behavior is preserved.

No-Observability-Change: no new metrics, spans, logs, status fields, or
dashboards; existing AWS drift defer/audit/reopen counters and producer
activation settle telemetry remain the sole signals, unchanged.
