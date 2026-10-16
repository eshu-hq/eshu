# #7533: PostgreSQL contention live proof enrollment

The reducer contention runner verified 92 named tests in 47 live files on a disposable PostgreSQL service.
Every enrolled test emitted one run and one pass event in its expected Go package. No enrolled test or subtest skipped or failed.
This establishes the PostgreSQL runner mapping only; it makes no graph backend claim.

## Run identity and method

- Baseline source: `eac0860933e4124400a7e59056f58954346d4c32`.
- Go: `go1.26.9 linux/arm64` with `-race -count=1 -json`.
- PostgreSQL: `18.6` using `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`.
- Selection: 86 exact Test names from the first 43 files, plus six readiness wait tests from four files. The committed inventory checks source and workflow selectors.
- Event check: `python3 scripts/lib/reducer_contention_events.py --events <file> --package <full import path>` on each package stream.
- Outcome: 92 top-level passes, 0 skips, 0 failures across the three packages.
- The service ran on a shared host. These elapsed values are enrollment observations, not a controlled performance baseline.

The three package invocations were:

```text
go test ./internal/query -run '^(TestServiceStoryTargetSupportSQLSemanticsLive)$' -json -race -count=1 -timeout 750s
go test ./internal/storage/postgres -run '^(TestActiveFactWorkItemsFormsSelectTheSameRows|TestActiveWorkSummaryBlockageHashesLeasesOnce|TestActiveWorkSummaryDropsTerminalTextFromMaterializedRows|TestActiveWorkSummaryGateMutationsAgainstOracle|TestActiveWorkSummaryGateResolvesTableByRegclass|TestActiveWorkSummaryMatchesPreHistoryGroupsOracle|TestActiveWorkSummaryMatchesStandaloneReads|TestActiveWorkSummaryOracleCatchesSeededHistoryMutations|TestAdvisoryOnlySuppressionHTTPStoreReducerQueryLive|TestAdvisorySuppressionIsolatedSchemaDerivesFromBootstrapLive|TestFinalizedGenerationRecommitSkipsFactsLive|TestGenerationRetentionCandidatePlanNeverLoopsFactWorkItemsLive|TestGenerationRetentionContentPrunesDeleteExactRowsLive|TestGenerationRetentionContentPrunesFinishWithoutPlannerStatisticsLive|TestGenerationRetentionEvalPlanQualDropsRacedCandidatesLive|TestGenerationRetentionFairnessAcrossScopesLive|TestGenerationRetentionFirstCountRunsWithoutScopeLocksLive|TestGenerationRetentionLockSetMatchesEligibleScopesLive|TestGenerationRetentionNarrowedCandidateTakenElsewhereLive|TestGenerationRetentionProbeMatchesGroupedPassLive|TestGenerationRetentionProbePlansStayOnKeyIndexesLive|TestGenerationRetentionPrunesMigratedSchemaLive|TestGenerationRetentionRefusesWithoutValidKeyIndexLive|TestGenerationRetentionRowCountsAttributeSharedRowsOnceLive|TestGenerationRetentionScopeMismatchedFactNeverOverDeletesLive|TestGenerationRetentionSelectsEligibleGenerationsAcrossScopesLive|TestGenerationRetentionSkipsHeldScopeAndReplacesLive|TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive|TestListOSPackageAdvisoryFactEnvelopesGenerationFlipMidDrainLive|TestListOSPackageAdvisoryFactEnvelopesNarrowedPagesToCompletionLive|TestListOSPackageAdvisoryFactEnvelopesPURLFormsMatchTheGoMatcherLive|TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive|TestProjectorAckDefersWhileIngestionHoldsScope|TestProjectorCompletionDoesNotDeadlockSameGenerationCommit|TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal|TestProjectorHeartbeatRenewsLeaseWhileIngestionHoldsScope|TestProjectorHeartbeatSupersessionPreservesActivePointer|TestProjectorHeartbeatWriteGateRowsLive|TestProjectorHeartbeatWriteMarkerInterleave|TestProjectorQueueRejectsAttemptReclaimedDuringLockWait|TestProjectorQueueRejectsReclaimedSameOwnerAttempt|TestProjectorWriteMarkerHeartbeatOrderingLive|TestPublishedGenerationRecommitIsIdempotentLive|TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin|TestReplayAfterFullRecordsLatestWriteStartLive|TestReplayedFailedGenerationWritesAndActivatesLive|TestServiceCatalogCorpusFenceDefersAndPreservesEvidenceLive|TestServiceCatalogCorpusFenceUnfencedReadReportsSpuriousRemovalLive|TestServiceChangedSinceBindsGrantToLineageScopeLive|TestServiceChangedSinceResolvePicksAttributedNewestActiveLive|TestServiceMaterializationActiveIndexReplayConvergesLive|TestServiceMaterializationWriterKeepsScopedLineagesLive|TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive|TestSharedIntentAcceptanceWriterStaleWriteCounterLive|TestSharedIntentGenerationPendingIndexLifecycleLive|TestSharedIntentUpsertCreatedAtLastWriterWinsLive|TestSharedProjectionAcceptanceConcurrentOutOfOrderLive|TestSharedProjectionAcceptanceLegacyNullKeyAdvancesLive|TestSharedProjectionAcceptanceLegacyNullKeyInvisibleGenerationAdvancesLive|TestSharedProjectionAcceptanceLegacyNullKeyRejectsStaleLive|TestSharedProjectionAcceptanceOrdersByIngestedAtLive|TestSharedProjectionAcceptancePostSnapshotStoredGenerationLive|TestSharedProjectionAcceptanceSameGenerationRetryKeepsHealedKeyLive|TestSharedProjectionAcceptanceSequentialStaleWriteLive|TestStatusActiveWorkQueriesPreserveSemantics|TestSupplyChainImpactCappedScopeConvergesLive|TestSupplyChainImpactEmptyFresherPassStillFencesOlderPassLive|TestSupplyChainImpactFencingTokenIssuerIssuesStrictlyIncreasingValuesLive|TestSupplyChainImpactRetractsLegacyZeroRowsAndStampsTokenLive|TestSupplyChainImpactSeedAdvancesAnUncalledSequenceEqualToTheWatermarkLive|TestSupplyChainImpactStalePassResumingAfterReclaimCannotRetractFresherSetLive|TestSupplyChainImpactUpsertGuardRefusesAStaleReviveLive|TestSupplyChainImpactWriterConcurrentPassesConvergeLive|TestSupplyChainImpactWriterPartialEvidenceSkipsRetractionLive|TestSupplyChainImpactWriterRetractionHonorsBoundaryLive|TestSupplyChainImpactWriterRetractionIsIdempotentLive|TestSupplyChainImpactWriterRetractsSupersededFindingsLive|TestSupplyChainImpactWriterSerializesOverlappingPassesLive|TestUncoveredProjectionWritersLive|TestWorkloadFencedReplaySupersedesOnlyOlderInFlightToken|TestWorkloadReplayAndBatchAckContentionConvergesToPending|TestWorkloadReplayClaimAfterAckToleratesSkewedAppClock|TestWorkloadReplayConcurrentFirstScheduleReportsSuccess|TestWorkloadReplayDuringClaimReturnsAckToPending|TestWorkloadReplayOutcomeReportsSupersededStableItem)$' -json -race -count=1 -timeout 750s
go test ./internal/storage/postgres/readiness/wait/ -run '^TestReadinessWait.*Live$' -json -race -count=1 -timeout 120s
```

Go package terminal elapsed values were `github.com/eshu-hq/eshu/go/internal/storage/postgres` 260.317s, `github.com/eshu-hq/eshu/go/internal/query` 3.004s, and `github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/wait` 7.095s. The first two process wall times, including setup, were `./internal/query` 55.656s, `./internal/storage/postgres` 499.119s. The readiness wait wall time was not recorded separately.

## File inventory

| File | Named tests | Summed Go elapsed |
| --- | ---: | ---: |
| `go/internal/query/service_story_target_support_live_test.go` | 1 | 1.94s |
| `go/internal/storage/postgres/generation_retention_candidate_plan_live_test.go` | 1 | 2.29s |
| `go/internal/storage/postgres/generation_retention_evalplanqual_live_test.go` | 1 | 2.32s |
| `go/internal/storage/postgres/generation_retention_fairness_live_test.go` | 2 | 5.26s |
| `go/internal/storage/postgres/generation_retention_key_index_live_test.go` | 2 | 5.85s |
| `go/internal/storage/postgres/generation_retention_lock_set_live_test.go` | 2 | 6.33s |
| `go/internal/storage/postgres/generation_retention_migrated_schema_live_test.go` | 2 | 5.57s |
| `go/internal/storage/postgres/generation_retention_narrow_live_test.go` | 1 | 4.82s |
| `go/internal/storage/postgres/generation_retention_probe_exact_live_test.go` | 1 | 2.83s |
| `go/internal/storage/postgres/generation_retention_probe_plan_live_test.go` | 1 | 4.12s |
| `go/internal/storage/postgres/generation_retention_prune_plan_live_test.go` | 2 | 15.81s |
| `go/internal/storage/postgres/generation_retention_row_attribution_live_test.go` | 1 | 2.49s |
| `go/internal/storage/postgres/generation_retention_selects_eligible_live_test.go` | 1 | 2.66s |
| `go/internal/storage/postgres/generation_uncovered_writers_live_test.go` | 1 | 0.46s |
| `go/internal/storage/postgres/ingestion_published_generation_recommit_live_test.go` | 2 | 0.35s |
| `go/internal/storage/postgres/installed_advisory_targets_paging_live_test.go` | 4 | 21.88s |
| `go/internal/storage/postgres/projector_heartbeat_refusal_lock_order_live_test.go` | 1 | 1.60s |
| `go/internal/storage/postgres/projector_heartbeat_write_marker_interleave_live_test.go` | 1 | 3.44s |
| `go/internal/storage/postgres/projector_queue_ack_scope_wait_live_test.go` | 1 | 2.09s |
| `go/internal/storage/postgres/projector_queue_attempt_fence_live_test.go` | 1 | 0.27s |
| `go/internal/storage/postgres/projector_queue_attempt_reclaim_contention_live_test.go` | 1 | 0.34s |
| `go/internal/storage/postgres/projector_queue_heartbeat_scope_lock_live_test.go` | 1 | 0.09s |
| `go/internal/storage/postgres/projector_queue_ingestion_lock_order_live_test.go` | 1 | 0.18s |
| `go/internal/storage/postgres/projector_queue_supersession_live_test.go` | 1 | 0.31s |
| `go/internal/storage/postgres/projector_queue_write_marker_live_test.go` | 2 | 0.90s |
| `go/internal/storage/postgres/projector_write_marker_replay_live_test.go` | 2 | 6.84s |
| `go/internal/storage/postgres/readiness/wait/store_clear_fence_live_test.go` | 1 | 2.27s |
| `go/internal/storage/postgres/readiness/wait/store_fence_live_test.go` | 2 | 1.61s |
| `go/internal/storage/postgres/readiness/wait/store_live_test.go` | 2 | 1.50s |
| `go/internal/storage/postgres/readiness/wait/store_settle_fence_live_test.go` | 1 | 0.68s |
| `go/internal/storage/postgres/reducer_queue_workload_replay_live_test.go` | 6 | 16.33s |
| `go/internal/storage/postgres/service_catalog_corpus_fence_live_test.go` | 2 | 5.17s |
| `go/internal/storage/postgres/service_changed_since_scope_live_test.go` | 1 | 2.37s |
| `go/internal/storage/postgres/service_materialization_scope_replay_live_test.go` | 1 | 6.44s |
| `go/internal/storage/postgres/service_materialization_scope_writer_live_test.go` | 2 | 4.65s |
| `go/internal/storage/postgres/shared_intents_generation_pending_index_live_test.go` | 1 | 0.09s |
| `go/internal/storage/postgres/shared_intents_upsert_created_at_live_test.go` | 1 | 2.29s |
| `go/internal/storage/postgres/shared_projection_acceptance_contention_live_test.go` | 6 | 16.44s |
| `go/internal/storage/postgres/shared_projection_acceptance_healed_key_live_test.go` | 1 | 2.45s |
| `go/internal/storage/postgres/shared_projection_acceptance_monotonic_live_test.go` | 3 | 11.36s |
| `go/internal/storage/postgres/status_active_work_semantics_live_test.go` | 8 | 25.26s |
| `go/internal/storage/postgres/status_blockage_leases_live_test.go` | 2 | 15.24s |
| `go/internal/storage/postgres/supply_chain_impact_capped_scope_live_test.go` | 1 | 2.36s |
| `go/internal/storage/postgres/supply_chain_impact_fencing_live_test.go` | 6 | 18.66s |
| `go/internal/storage/postgres/supply_chain_impact_replace_set_concurrency_live_test.go` | 2 | 7.98s |
| `go/internal/storage/postgres/supply_chain_impact_replace_set_live_test.go` | 4 | 12.50s |
| `go/internal/storage/postgres/vulnerability_suppression_advisory_e2e_live_test.go` | 2 | 10.15s |

The file values sum the Go terminal `Elapsed` values for tests declared in that file.
Tests can run in parallel, so these sums are not additive wall time.
The committed inventory records each exact Test name and package; the runner checks it against source and selector text.
