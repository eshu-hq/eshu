#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/drifted_bucket_skip_live_test.go": (
            "TestDriftedPathologicalBucketSkippedLive",
        ),
        "go/internal/storage/postgres/ecs_binding_declaration_census_live_test.go": (
            "TestECSBindingCensusReadsActiveDeclarationsLive",
            "TestECSBindingCensusTruncationIsVisibleLive",
            "TestECSBindingCensusPlanIsIndexBackedLive",
            "TestECSBindingCensusEntityTypesMatchSQL",
        ),
        "go/internal/storage/postgres/ecs_task_index_migration_live_test.go": (
            "TestECSRuntimeIndexFreshBootstrapLive",
            "TestECSRuntimeIndexUpgradeLive",
        ),
        "go/internal/storage/postgres/fact_key_foreign_key_drop_live_test.go": (
            "TestBootstrapDropsEveryFactRecordsForeignKeyLive",
            "TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive",
        ),
        "go/internal/storage/postgres/fact_key_foreign_key_reverse_live_test.go": (
            "TestFactKeyForeignKeyReverseProcedureLive",
            "TestFactKeyForeignKeyDropFailsLoudlyWhenKeyTableStaysBusyLive",
        ),
        "go/internal/storage/postgres/fact_key_orphan_census_live_test.go": (
            "TestFactKeyOrphanCensusCountsPlantedOrphansLive",
        ),
        "go/internal/storage/postgres/fact_records_autovacuum_reloptions_live_test.go": (
            "TestFactRecordsAutovacuumReloptionsMigrationLive",
        ),
        "go/internal/storage/postgres/gap_admitted_anchor_index_live_test.go": (
            "TestDeployableUnitGapAdmittedAnchorIndexLifecycleAndSeekLive",
        ),
        "go/internal/storage/postgres/generation_freshness_reproject_live_test.go": (
            "TestStampActiveGenerationLifecycleLive",
            "TestPreparseIdentityStampsAndActiveStateLive",
            "TestCheapReprojectLifecycleLive",
            "TestCheapReprojectRacesLive",
            "TestPreparseIdentityDroppedByTheStreamLive",
            "TestStreamDroppedGenerationHealsLive",
            "TestIdentitylessStreamDropIsMarkedLive",
        ),
        "go/internal/storage/postgres/generation_prior_check_live_test.go": (
            "TestPriorGenerationCheckIgnoresNewerGenerationLive",
            "TestPriorGenerationCheckStrictlyOlderCasesLive",
        ),
        "go/internal/storage/postgres/generation_retention_batch_predrain_live_test.go": (
            "TestGenerationRetentionBatchPredrainKeepsScopeLockBoundedLive",
            "TestGenerationRetentionBatchPredrainCrashResumeKeepsExactCountsLive",
            "TestGenerationRetentionBatchPredrainDeltaFlipExcludesLive",
            "TestGenerationRetentionBatchPredrainConcurrentPassDefersLive",
        ),
        "go/internal/storage/postgres/generation_retention_chain_live_test.go": (
            "TestGenerationRetentionKeepsCumulativeDeltaBaseLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_insert_wait_live_test.go": (
            "TestGenerationRetentionChunkPredrainConcurrentInsertLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_live_test.go": (
            "TestGenerationRetentionChunkedDrainLive",
            "TestGenerationRetentionChunkStatementsRollBackLive",
            "TestGenerationRetentionChunkLockMissDropsSilentlyLive",
            "TestGenerationRetentionChunkPredrainEligibilityFlipStopsLive",
        ),
        "go/internal/storage/postgres/generation_retention_credential_payloads_live_test.go": (
            "TestGenerationRetentionPrunesCredentialCopiesByDefaultLive",
        ),
        "go/internal/storage/postgres/generation_retention_delta_base_live_test.go": (
            "TestGenerationRetentionDeltaBaseLive",
            "TestGenerationRetentionDeltaBaseSkipsUnactivatedNewerFullLive",
        ),
        "go/internal/storage/postgres/generation_retention_delta_base_relock_live_test.go": (
            "TestGenerationRetentionRelockKeepsFlippedDeltaChainLive",
        ),
        "go/internal/storage/postgres/generation_retention_fact_keys_accuracy_live_test.go": (
            "TestKeyWritersReplaceOrphanFactKeysLive",
            "TestOrphanRelationshipKeysAreInvisibleToDeferredLoadLive",
            "TestOrphanFactKeysCannotBeWrittenForAPrunedGenerationLive",
        ),
        "go/internal/storage/postgres/generation_retention_fact_keys_concurrency_live_test.go": (
            "TestRetentionFactKeyDeletesBesideKeyWriterLive",
            "TestRetentionFactKeyDeletesBesideBackfillRebuildLive",
            "TestRetentionFactKeyReversedOrderDeadlocksLive",
        ),
        "go/internal/storage/postgres/generation_retention_fact_keys_live_test.go": (
            "TestGenerationRetentionNormalPruneLeavesNoOrphanFactKeysLive",
            "TestGenerationRetentionChunkDrainLeavesNoOrphanFactKeysLive",
            "TestGenerationRetentionRefusesWithoutValidFactKeyIndexLive",
        ),
        "go/internal/storage/postgres/generation_retention_fact_keys_plan_live_test.go": (
            "TestGenerationRetentionFactKeyStatementsPrepareLive",
            "TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive",
        ),
        "go/internal/storage/postgres/generation_retention_hard_ceiling_live_test.go": (
            "TestGenerationRetentionHardCeilingLive",
        ),
        "go/internal/storage/postgres/generation_retention_large_fixture_live_test.go": (
            "TestGenerationRetentionStoreLargeFixtureLive",
        ),
        "go/internal/storage/postgres/generation_retention_relock_live_test.go": (
            "TestGenerationRetentionPrescreenChunksOverFactGenerationsLive",
            "TestGenerationRetentionPrescreenProbesFactIndexLive",
            "TestGenerationRetentionTargetedLockPlanShapeLive",
            "TestGenerationRetentionTargetedLockEvalPlanQualDropsRacedMembersLive",
        ),
        "go/internal/storage/postgres/generation_retention_relock_uncovered_live_test.go": (
            "TestGenerationRetentionRelockDropsNewlyUncoveredWriterLive",
        ),
        "go/internal/storage/postgres/generation_retention_row_counts_10512_live_test.go": (
            "TestGenerationRetentionRowCountPlansStayOnGenerationIndexesLive",
            "TestGenerationRetentionPackageKeyCountsMatchFactJoinOnCleanKeysLive",
            "TestGenerationRetentionRefusesWithoutValidIntentsGenerationIndexLive",
        ),
        "go/internal/storage/postgres/generation_retention_row_counts_10525_live_test.go": (
            "TestGenerationRetentionWorkItemsPlansStayOnGenerationIndexLive",
            "TestGenerationRetentionWorkCountsMatchCascadeEnvelopeLive",
            "TestGenerationRetentionRefusesWithoutValidWorkItemsGenerationIndexLive",
            "TestGenerationRetentionWorkIndexCycleIgnoresHeldForeignWorkLockLive",
        ),
        "go/internal/storage/postgres/generation_retention_samesha_live_test.go": (
            "TestGenerationRetentionSameSHALockstepLive",
            "TestGenerationRetentionSameSHAPrunesOldestKeepsNewestLive",
            "TestGenerationRetentionCoveredNeverActivatedPrunesPastCeilingLive",
        ),
        "go/internal/storage/postgres/generation_retention_search_index_live_test.go": (
            "TestGenerationRetentionPredeletesSearchIndexBeforeTheScopeLockLive",
            "TestGenerationRetentionAdmitsOnlyDrainedGenerationsLive",
            "TestGenerationRetentionAdmitSkipsAGenerationWhoseLockIsHeldLive",
            "TestGenerationRetentionCandidateReadQueryPreparesLive",
            "TestGenerationFreshnessFailsClosedForAPrunedGenerationLive",
        ),
        "go/internal/storage/postgres/generation_retention_terms_foreign_key_live_test.go": (
            "TestGenerationRetentionPrunesWithoutTheTermsForeignKeyLive",
            "TestGenerationRetentionLateTermsWriterLeavesASweptInvisibleOrphanLive",
        ),
        "go/internal/storage/postgres/generation_retention_uncovered_writer_live_test.go": (
            "TestGenerationRetentionKeepsUncoveredWritersLive",
        ),
        "go/internal/storage/postgres/hardcoded_finding_parity_live_test.go": (
            "TestSecretDetectorEngineParity",
        ),
        "go/internal/storage/postgres/identity_epoch_active_set_live_test.go": (
            "TestIdentityEpochIgnoresSupersededGenerationRowsLive",
            "TestIdentityPageQueryServesOnlyActiveGenerationsLive",
        ),
        "go/internal/storage/postgres/identity_epoch_empty_set_live_test.go": (
            "TestProbeIdentityEpochEmptySetLive",
        ),
        "go/internal/storage/postgres/identity_epoch_probe_reformulation_live_test.go": (
            "TestIdentityEpochProbeReformulationMatchesOracleLive",
        ),
        "go/internal/storage/postgres/identity_page_plan_live_test.go": (
            "TestIdentityPageQueryPlanRidesOrderedIndexLive",
        ),
        "go/internal/storage/postgres/ingestion_backfill_generation_guard_lifecycle_live_test.go": (
            "TestReconcileReopenSkipsLiveLeaseWithoutStampLive",
            "TestReconcileReprojectionDeadLetterKeepsActiveGenerationLive",
            "TestPendingSameIdentitySkipsAndFailedStoresLive",
            "TestReconcileReopenResetsAttemptsSoRetryRetriesLive",
            "TestReconcileReopenOfDeadLetterIsBoundedLive",
            "TestStaleReconcileSkipsPendingSameIdentityLive",
            "TestReindexStoresUnchangedIdentityLive",
            "TestStaleFullActivationReadsFreshLive",
            "TestReconcileReopenWorkRowShapesLive",
            "TestPendingWithoutLiveWorkStoresLive",
        ),
        "go/internal/storage/postgres/ingestion_backfill_generation_guard_reproject_live_test.go": (
            "TestUnchangedReconcileStoresNothingLive",
            "TestReconcileIdentityChangesStillStoreLive",
            "TestReconcileReopenLosesLockRaceToNewerActivationLive",
            "TestReconcileReprojectionSupersededBeforeClaimIsNoopLive",
        ),
        "go/internal/storage/postgres/ingestion_claim_fence_live_test.go": (
            "TestClaimPlaneFenceRefusesReclaimedStaleWriterLive",
            "TestClaimPlaneFenceDeadlineRollsBackLive",
            "TestClaimPlaneFenceStaleRepinCancelsCommitLive",
            "TestClaimPlaneFenceReplayAndCrossItemLive",
            "TestClaimPlaneFenceHungRepinDoesNotHoldADurableCommitLive",
        ),
        "go/internal/storage/postgres/ingestion_claim_fence_race_live_test.go": (
            "TestClaimPlaneFenceConcurrentWritersAckAndMaintenanceLive",
        ),
        "go/internal/storage/postgres/ingestion_fanin_unchanged_partition_live_test.go": (
            "TestWholePassFanInSkipsUnchangedMemoHitPartitionsLive",
            "TestWholePassFanInRepublishesUnchangedPartitionMissingPhaseRowLive",
            "TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive",
        ),
        "go/internal/storage/postgres/ingestion_flux_evidence_identity_live_test.go": (
            "TestIngestionStoreCommitScopeGenerationPersistsFluxEvidenceNatively",
            "TestFluxEvidenceMixedGenerationLegacyAndCurrentCoexist",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint_concurrency_live_test.go": (
            "TestDeployableUnitReopenFingerprintConcurrentPassesLive",
            "TestDeployableUnitReopenFingerprintWriteRacesReclaimLive",
            "TestDeployableUnitReopenFingerprintWriteRacesRetentionLive",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint_differential_live_test.go": (
            "TestDeployableUnitReopenFingerprintDifferentialLive",
            "TestDeployableUnitReopenFingerprintZeroCandidateLive",
            "TestDeployableUnitReopenFingerprintGatesTargetedPassLive",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint_interleave_live_test.go": (
            "TestDeployableUnitReopenFingerprintInterleavePassVsClaimVsAckVsWriteLive",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint_live_test.go": (
            "TestDeployableUnitReopenFingerprintGateLive",
            "TestDeployableUnitReopenFingerprintKeepsReadSnapshotLive",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_fingerprint_write_live_test.go": (
            "TestDeployableUnitReopenFingerprintWriteIsClaimFencedLive",
            "TestDeployableUnitReopenFingerprintWriteIsMonotonicLive",
            "TestDeployableUnitReopenFingerprintCascadesWithWorkItemLive",
        ),
        "go/internal/storage/postgres/ingestion_reopen_correlation_invalidate_live_test.go": (
            "TestInvalidateDeployableUnitReopenFingerprintsForReposLive",
            "TestListAllDeployableUnitAdmittedAnchorReposLive",
            "TestListAllDeployableUnitAdmittedAnchorReposConvergesAfterSupersedingDecisionLive",
            "TestListAllDeployableUnitAdmittedAnchorReposBreaksDecidedAtTiesLive",
        ),
        "go/internal/storage/postgres/ingestion_successor_guard_chain_live_test.go": (
            "TestGuardAnchorsChainedDeltaAtUnionBaseLive",
            "TestGuardStoresSmallChainedDeltaLive",
        ),
        "go/internal/storage/postgres/ingestion_successor_guard_sizing_live_test.go": (
            "TestGuardStoresForcedFullOverReducerHeavyPriorLive",
            "TestGuardRefusesSharplyShrunkenFullOverReducerHeavyPriorLive",
            "TestGuardStoresSmallDeltaOverLargePriorLive",
            "TestGuardRefusesMassDeleteDeltaLive",
            "TestGuardStoresRenameDeltaLive",
            "TestGuardStoresDeltaDeletingNeverIndexedPathsLive",
            "TestGuardStoresDeltaWithMissingBaseLive",
            "TestGuardSizesDeltaWithoutRepositoryFactAsFullLive",
            "TestGuardRefusesSharpShrinkFullWhileChainedDeltaPendingLive",
            "TestGuardStoresHalfSizeFullWhileChainedDeltaPendingLive",
            "TestGuardSizesAgainstLargerPendingFullLive",
            "TestGuardRefusesStepwiseShrinkThroughPendingFullLive",
        ),
        "go/internal/storage/postgres/ingestion_successor_guard_window_live_test.go": (
            "TestGuardRecheckRefusesAfterACommitInTheWindowLive",
            "TestGuardRecheckStoresAHalfSizeFullAfterTheResizeLive",
            "TestGuardRecheckResizesAfterAnAckInTheWindowLive",
            "TestGuardRecheckUnchangedScopeReadsOnceLive",
            "TestGuardRecheckScopeRowBlocksCommitAndAckLive",
        ),
        "go/internal/storage/postgres/ingestion_vulnerability_successor_live_test.go": (
            "TestVulnerabilityUnchangedHintSkipsAndDriftCommitsLive",
            "TestVulnerabilityTrustedShrinkPassesAndUntrustedIsRefusedLive",
            "TestVulnerabilityRefusedCommitKeepsTheLastSuccessLive",
            "TestVulnerabilityRepeatedRefusalsHoldScopeStaleLive",
            "TestVulnerabilityConcurrentCommitsSerializeOnTheScopeRowLive",
        ),
    },
}
