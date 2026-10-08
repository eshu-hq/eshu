# 7636: reopen memo-hit partitions that receive new evidence — performance note

The #7636 fix revises the deferred relationship maintenance same-pass
memo-hit skip-set with the rows the pass's backfill actually inserted
(`excludePartitionsWithNewEvidence`,
`go/internal/storage/postgres/ingestion_reopen_partition_memo_gate.go`).
`insertEvidenceFactBatch` now returns `RowsAffected` through the new
`UpsertEvidenceFactsCounted` (`relationship_evidence_batch.go`, moved there
from `relationship_store.go` unchanged apart from the count), threaded up
through `writeDeferredBackfillBatchWith` /
`runDeferredBackfillBatchesWith` to both the whole pass
(`backfillAllRelationshipEvidence`) and the targeted pass
(`writeTargetedMaintenanceEvidence`), each of which subtracts
inserted-positive partitions from its skip-set before the relationship-domain
reopens. A re-upsert of identical evidence reports 0 and never invalidates a
skip. Three pinned differential fixtures change by the same mechanism
(`cloud_scope_gcp_relation_owed`, `argocd_applicationset_external_config_repo`,
`owed_repo_id_collision_loser`); the quiet-partition negative controls are
untouched.

## No-Regression Evidence:

No new SQL statement, no new round-trip, no lock or transaction change:
`RowsAffected` is read off the already-issued batch `ExecContext` results,
and the revision is one in-memory map subtraction per pass, O(skip-set
size). The per-batch inserted map merges under the pool's existing
contribution mutex — no new shared state, no lock-order change, batch
transaction boundaries unchanged. The no-new-round-trip half is pinned, not
argued: `TestUpsertEvidenceFactsBatchesInserts` asserts the exact
`ExecContext` call count per fact volume and stays green, and the
`deferred_backfill_batch_committed` batch counts in the live runs are
identical before and after. (No before/after wall-time comparison is
claimed: on fixtures this small it would be noise, and the change adds no
I/O to measure.)
Baseline: clean origin/main — `gsrc-1/deployment_mapping` stays
`succeeded` though its generation received new inbound evidence (the #7636
stall), RED at
`TestMemoHitPartitionWithNewInboundEvidenceReopensRelationshipItems`.
After: the pass logs `deferred_backfill_skip_set_revised skipped_before=13
unskipped_new_evidence=1 skipped_after=12` and reopens exactly 2 items per
relationship domain (the memo-hit gsrc-1 plus the memo-miss gcp-2); every
reopened item drains through the real reducer Claim/Ack path (only the 14
pre-existing readiness-gated k8s correlation items stay pending); the next
pass unskips 0 and leaves every memo-hit relationship row byte-identical
(exactly-once); 0 reopens across 10 quiet memo-hit scopes (cost bound).
Differential: `TestTargetedMaintenanceMatchesWholePass` 9/9 subtests green
with whole/targeted 0/0 in-scope agreement, including the two updated
fixtures and the untouched `inbound_content_reference_from_another_repo`
and `memo_hit_and_miss` negative controls; the outcomes suite is green with
its one updated fixture. Affected families
(`Backfill|TargetedMaintenance|Reopen|MemoHit|DeferredMaintenance|PartitionMemo`):
114/114 pass. The B-7 golden-corpus gate is unaffected by construction: it
drives bootstrap-index, which uses the nil skip-set (reopen-all) path this
change does not touch; the same-pass path is ingester-only and not
gate-covered.

## Observability Evidence:

One new log line per maintenance pass, emitted by both the whole and the
targeted pass: `deferred_backfill_skip_set_revised skipped_before=%d
unskipped_new_evidence=%d skipped_after=%d`. It attributes cause (how many
memo-hit partitions received new evidence); the existing per-domain
`reopen_partition_memo_gate_completed` lines and the
`ReopenSkippedByPartitionMemo` counter show the effect (fewer skips, more
reopens). No new metric, span, route, worker, lease, or runtime knob: the
gate counters already observe the behavior change, and a dedicated
unskipped-partitions instrument would duplicate the log line's two integers
without a distinct alert use.
