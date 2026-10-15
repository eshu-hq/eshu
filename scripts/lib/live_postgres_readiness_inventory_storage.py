#!/usr/bin/env python3
"""Expected live proofs for the impact and shared storage packages (data only)."""

from live_postgres_readiness_inventory_names import (
    IMPACT_PACKAGE,
    SEARCH_INDEX_RETENTION_PACKAGE,
    STORAGE_PACKAGE,
)

STORAGE_PACKAGES = {
    IMPACT_PACKAGE: {
        "go/internal/query/supply/chain/impact/readiness_package_manifest_repo_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive",
            "TestSupplyChainImpactReadinessRepoArmScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_container_identity_live_test.go": (
            "TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_consumption_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageConsumptionScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_orphan_package_keys_live_test.go": (
            "TestReadinessIgnoresOrphanPackageKeysLive",
        ),
        "go/internal/query/supply/chain/impact/runtime_environment_store_live_test.go": (
            "TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive",
            "TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive",
            "TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scan_tier_explain_live_test.go": (
            "TestSupplyChainImpactReadinessScanTierQueryPlanLive",
            "TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive",
        ),
        "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go": (
            "TestSupplyChainSuppressionPathsPerformanceLive",
        ),
    },
    STORAGE_PACKAGE: {
        "go/internal/storage/postgres/projector_marked_write_age_live_test.go": (
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeLive",
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeEmptyLive",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_contention_live_test.go": (
            "TestProjectorClaimMarkedGuardContention",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_epq_live_test.go": (
            "TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn",
            "TestProjectorClaimDropsHolderClaimedAfterSnapshot",
            "TestProjectorClaimGuardAgreesWithFenceAcrossClocks",
            "TestProjectorClaimMarkerSweepRace",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_interleave_live_test.go": (
            "TestProjectorClaimRetryAgainstNewerInterleave",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_live_test.go": (
            "TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry",
            "TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer",
            "TestProjectorClaimStillSupersedesUnmarkedStaleGeneration",
            "TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration",
            "TestProjectorAckKeepsMarkedObsoleteGeneration",
            "TestProjectorAckStillSupersedesUnmarkedObsoleteGeneration",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_plan_live_test.go": (
            "TestProjectorClaimMarkedGuardPlanShape",
            "TestProjectorClaimMarkedGuardPlanShapeAtScale",
            "TestProjectorClaimMarkedGuardLockSet",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_contention_live_test.go": (
            "TestProjectorClaimFullGuardContention",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_epq_live_test.go": (
            "TestProjectorClaimDropsFullHolderClaimedAfterSnapshot",
            "TestProjectorClaimFullGuardAgreesWithFenceAcrossClocks",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_live_test.go": (
            "TestProjectorClaimFullSurvivesNewerDelta",
            "TestProjectorClaimFailedFullStillHoldsDelta",
            "TestProjectorClaimHoldsDeltaBehindBackoffFull",
            "TestProjectorClaimRunsDriftedFullBeforeNewerDelta",
            "TestProjectorClaimStillSupersedesFullBehindNewerFull",
            "TestProjectorClaimStillSupersedesDeltaBehindNewerDelta",
            "TestProjectorClaimClaimsDeltaBehindTerminalFull",
            "TestProjectorAckStillSupersedesFullBehindAckedDelta",
        ),
        "go/internal/storage/postgres/projector_queue_claim_full_guard_plan_live_test.go": (
            "TestProjectorClaimFullGuardPlanShape",
            "TestProjectorClaimFullGuardPlanShapeAtScale",
            "TestProjectorClaimFullGuardLockSet",
        ),
        "go/internal/storage/postgres/projector_queue_ack_obsolete_marker_live_test.go": (
            "TestProjectorAckSeesLockTimeMarkerTruth",
            "TestProjectorAckMarkerCommitRace",
            "TestProjectorAckObsoleteSkipsFencedBusyScope",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_live_test.go": (
            "TestProjectorAckJournalAtomicity",
            "TestProjectorAckJournalPriorLive",
            "TestProjectorAckJournalOffWritesNothing",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_inversion_live_test.go": (
            "TestProjectorAckJournalInversion",
            "TestProjectorAckJournalGateHeldByPass",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_fence_live_test.go": (
            "TestProjectorAckJournalLockFences",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_contention_live_test.go": (
            "TestProjectorAckJournalContention",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_equivalence_live_test.go": (
            "TestProjectorAckJournalEquivalence",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_cost_live_test.go": (
            "TestProjectorAckJournalCost",
        ),
        "go/internal/storage/postgres/projector_queue_claim_marker_fence_live_test.go": (
            "TestMarkProjectionWriteStartedFenceSync",
            "TestMarkProjectionWriteStartedMissingFenceRefuses",
        ),
        "go/internal/storage/postgres/delta_union_live_test.go": (
            "TestFactStoreListFactsDeltaUnionLive",
            "TestFactStoreListFactsDeltaBaseMissingLive",
            "TestFactStoreReadDeltaUnionActiveLive",
            "TestFactStoreListFactsPrunedBaseRefusesLive",
        ),
        "go/internal/storage/postgres/drifted_bucket_skip_live_test.go": (
            "TestDriftedPathologicalBucketSkippedLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_backfill_live_test.go": (
            "TestPackageManifestConsumptionBackfillRepairsOldWriterAfterReadyLive",
            "TestPackageManifestConsumptionBackfillPagesHeavyScopeLive",
            "TestPackageManifestConsumptionBackfillWaitsForScopeWriterLive",
            "TestPackageManifestConsumptionBackfillBoundsTwentyFiveScopePassLive",
            "TestPackageManifestConsumptionBackfillConcurrentPassesAreIdempotentLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_lock_live_test.go": (
            "TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive",
            "TestPackageManifestConsumptionScopeRebuildLockTimeoutLeavesScopeUnreadyLive",
            "TestPackageManifestConsumptionScopeRebuildResumesAfterPageFailureAndCancelLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_fence_live_test.go": (
            "TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive",
            "TestPackageManifestConsumptionScopeRebuildKeepsConcurrentWriterKeysLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_plan_live_test.go": (
            "TestPackageManifestConsumptionScopeFactsPageUsesBackfillIndexLive",
        ),
        "go/internal/storage/postgres/generation_retention_hard_ceiling_live_test.go": (
            "TestGenerationRetentionHardCeilingLive",
        ),
        "go/internal/storage/postgres/trigger_bootstrap_parity_live_test.go": (
            "TestWebhookTriggerStoreRunsOnBootstrapSchemaLive",
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
        "go/internal/storage/postgres/generation_retention_uncovered_writer_live_test.go": (
            "TestGenerationRetentionKeepsUncoveredWritersLive",
        ),
        "go/internal/storage/postgres/generation_retention_samesha_live_test.go": (
            "TestGenerationRetentionSameSHALockstepLive",
            "TestGenerationRetentionSameSHAPrunesOldestKeepsNewestLive",
            "TestGenerationRetentionCoveredNeverActivatedPrunesPastCeilingLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_live_test.go": (
            "TestGenerationRetentionChunkedDrainLive",
            "TestGenerationRetentionChunkStatementsRollBackLive",
            "TestGenerationRetentionChunkLockMissDropsSilentlyLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_insert_wait_live_test.go": (
            "TestGenerationRetentionChunkConcurrentInsertWaitsLive",
        ),
        "go/internal/storage/postgres/generation_retention_delta_base_live_test.go": (
            "TestGenerationRetentionDeltaBaseLive",
            "TestGenerationRetentionDeltaBaseSkipsUnactivatedNewerFullLive",
        ),
        "go/internal/storage/postgres/generation_retention_delta_base_relock_live_test.go": (
            "TestGenerationRetentionRelockKeepsFlippedDeltaChainLive",
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
        "go/internal/storage/postgres/generation_retention_search_index_live_test.go": (
            "TestGenerationRetentionPredeletesSearchIndexBeforeTheScopeLockLive",
            "TestGenerationRetentionAdmitsOnlyDrainedGenerationsLive",
            "TestGenerationRetentionAdmitSkipsAGenerationWhoseLockIsHeldLive",
            "TestGenerationRetentionCandidateReadQueryPreparesLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_migration_upgrade_live_test.go": (
            "TestPackageManifestConsumptionMigrationsUpgradeAfterSecretLinesLive",
        ),
        "go/internal/storage/postgres/migration_169_reader_bound_live_test.go": (
            "TestMigration169PhasedBackfillReaderBoundLive",
        ),
        "go/internal/storage/postgres/prefetch_batch_plan_live_test.go": (
            "TestPrefetchBatchQueriesUsePrimaryKeyLive",
        ),
        "go/internal/storage/postgres/ingestion_backfill_generation_guard_lifecycle_live_test.go": (
            "TestReconcileReopenSkipsLiveLeaseWithoutStampLive",
            "TestReconcileReprojectionDeadLetterKeepsActiveGenerationLive",
            "TestPendingSameIdentitySkipsAndFailedStoresLive",
            "TestReconcileReopenResetsAttemptsSoRetryRetriesLive",
            "TestStaleReconcileSkipsPendingSameIdentityLive",
            "TestReindexStoresUnchangedIdentityLive",
            "TestStaleFullActivationReadsFreshLive",
            "TestReconcileReopenWorkRowShapesLive",
            "TestPendingWithoutLiveWorkStoresLive",
            "TestReconcileReopenOfDeadLetterIsBoundedLive",
        ),
        "go/internal/storage/postgres/projector_queue_dead_letter_live_test.go": (
            "TestProjectorDeadLetterGuardCellsLive",
        ),
        "go/internal/storage/postgres/generation_freshness_reproject_live_test.go": (
            "TestPreparseIdentityStampsAndActiveStateLive",
            "TestCheapReprojectLifecycleLive",
            "TestCheapReprojectRacesLive",
            "TestStampActiveGenerationLifecycleLive",
            "TestPreparseIdentityDroppedByTheStreamLive",
            "TestStreamDroppedGenerationHealsLive",
            "TestIdentitylessStreamDropIsMarkedLive",
        ),
        "go/internal/storage/postgres/ingestion_backfill_generation_guard_reproject_live_test.go": (
            "TestUnchangedReconcileStoresNothingLive",
            "TestReconcileIdentityChangesStillStoreLive",
            "TestReconcileReopenLosesLockRaceToNewerActivationLive",
            "TestReconcileReprojectionSupersededBeforeClaimIsNoopLive",
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
        "go/internal/storage/postgres/repository_catalog_projection_live_test.go": (
            "TestLoadRepositoryCatalogProjectionMatchesWholePayloadDecode",
        ),
        "go/internal/storage/postgres/container_image_identity_epoch_barrier_live_test.go": (
            "TestContainerImageIdentityEpochBarrierDefersPendingLive",
            "TestContainerImageIdentityActivationEpochMissIsSentinelLive",
        ),
        "go/internal/storage/postgres/container_image_identity_first_generation_live_test.go": (
            "TestContainerImageIdentityFirstGenerationEpochBarrierLive",
            "TestContainerImageIdentityFirstGenerationFailureExitsLive",
        ),
        "go/internal/storage/postgres/container_image_identity_first_generation_service_live_test.go": (
            "TestContainerImageIdentityFirstGenerationServiceDispatchLive",
            "TestContainerImageIdentityFirstGenerationSupersededWhileNullLive",
        ),
        "go/internal/storage/postgres/projector_queue_zombie_heal_graph_live_test.go": (
            "TestProjectorZombieHealRestoresCanonicalNodesLive",
        ),
        "go/internal/storage/postgres/projector_queue_zombie_heal_live_test.go": (
            "TestProjectorRefusalHealsMarkedSupersededGeneration",
            "TestProjectorRepeatedRefusalsOpenOneActiveRow",
            "TestProjectorConcurrentHealsOpenOneActiveRow",
            "TestProjectorRefusalSkipsHealWithoutWriteMarker",
            "TestProjectorSupersededByNewerSkipsHeal",
            "TestProjectorRefusalSkipsHealWithoutActiveGeneration",
            "TestProjectorRefusalSkipsHealWhenActiveRowInFlight",
        ),
        "go/internal/storage/postgres/active_symbol_loader_composer_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsComposerKeys",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderKeepsNpmAndComposerNamesApart",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForUnpublishedComposerPackage",
        ),
        "go/internal/storage/postgres/active_code_call_symbols_ahead_manifest_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated",
        ),
        "go/internal/storage/postgres/manifest_less_dirty_rule_live_test.go": (
            "TestReducerContentionGateManifestLessDirtyRuleKeepsEveryScopeThatCanNeedAProducer",
            "TestReducerContentionGateManifestLessDirtyRuleMatchesUncoveredProjectionWriters",
        ),
        "go/internal/storage/postgres/manifest_less_dirty_rule_answer_live_test.go": (
            "TestReducerContentionGateManifestLessDirtyRuleReturnsTheSameDefinitions",
            "TestReducerContentionGateManifestLessDirtyRuleNaiveRuleLosesTheHole",
            "TestReducerContentionGateManifestLessDirtyRuleSeededRedGreen",
        ),
        "go/internal/storage/postgres/recovery_refinalize_delta_active_live_test.go": (
            "TestRefinalizeDeltaActiveRequestsFullReindex",
            "TestRefinalizeDeltaActiveConvergesAcrossTwoCalls",
        ),
        "go/internal/storage/postgres/recovery_refinalize_delta_active_fence_live_test.go": (
            "TestRefinalizeDeltaActiveRollsBackReindexWithTheTransaction",
            "TestRefinalizeDeltaActiveReindexLockContention",
        ),
        "go/internal/storage/postgres/recovery_refinalize_fence_admission_live_test.go": (
            "TestRefinalizeFenceAdmissionBoundsRetryBegin",
            "TestRefinalizeFenceAdmissionCallerCancellation",
            "TestRefinalizeFenceAdmissionDeadlineStopsAfterFenceAcquire",
            "TestRefinalizeFenceAdmissionPersistentHolderExhaustsWithoutConvoy",
            "TestRefinalizeFenceAdmissionWriteAndCommitFailuresKeepTheirCauses",
        ),
        "go/internal/storage/postgres/recovery_refinalize_lock_convoy_live_test.go": (
            "TestRefinalizeFenceDoesNotConvoyUnrelatedQueueWrite",
            "TestRefinalizeFenceRetryBudgetIncludesReducerDrain",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_interleave_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceProducerCompletesMidCheck",
            "TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_bound_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerNotCatalogAge",
            "TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerIsWorked",
            "TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerLeaseIsLive",
            "TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound",
            "TestReducerContentionGateServiceCatalogRuntimeFenceReopenedProducerWaits",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_readiness_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceKeepsItsAttemptBudget",
        ),
        "go/internal/storage/postgres/service_catalog_runtime_producer_store_live_test.go": (
            "TestReducerContentionGateRuntimeProducerStatesActiveGenerationOnly",
            "TestReducerContentionGateRuntimeProducerStatesNewestInFlightWorkableAt",
            "TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex",
        ),
        "go/internal/storage/postgres/content_files_generation_tag_live_test.go": (
            "TestReducerContentionGateContentGenerationTagAheadWriteReadsDirty",
            "TestReducerContentionGateContentGenerationTagActivatedTagReadsClean",
            "TestReducerContentionGateContentGenerationTagManifestLessScopeWithSignalReadsDirty",
            "TestReducerContentionGateContentGenerationTagDangledAndNullTagReadDirty",
            "TestReducerContentionGateContentWriterStampsGenerationTag",
            "TestReducerContentionGateContentFilesBackfillAttributesOnlyCleanScopes",
            "TestReducerContentionGateContentGenerationTagGoModDirtLegs",
            "TestReducerContentionGateContentWriterBlankGenerationStoresNull",
        ),
        "go/internal/storage/postgres/content_files_language_covering_index_replay_live_test.go": (
            "TestContentFilesLanguageCoveringIndexReplayConvergesLive",
        ),
        "go/internal/storage/postgres/identity_epoch_active_set_live_test.go": (
            "TestIdentityEpochIgnoresSupersededGenerationRowsLive",
            "TestIdentityPageQueryServesOnlyActiveGenerationsLive",
        ),
        "go/internal/storage/postgres/identity_page_plan_live_test.go": (
            "TestIdentityPageQueryPlanRidesOrderedIndexLive",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_ack_reclaim_live_test.go": (
            "TestReducerQueueAckBatchFencesSupersededClaimLive",
            "TestReducerQueueExpiryInterleaveLive",
        ),
        "go/internal/storage/postgres/aws_cloud_runtime_drift_state_arn_plan_live_test.go": (
            "TestAWSCloudRuntimeDriftStateARNJoinProbesARNIndexLive",
        ),
        "go/internal/storage/postgres/aws_cloud_runtime_drift_state_arn_differential_live_test.go": (
            "TestAWSCloudRuntimeDriftStateARNJoinMatchesReferenceLive",
        ),
    },
    SEARCH_INDEX_RETENTION_PACKAGE: {
        "go/internal/storage/postgres/search/index/retention/drain_live_test.go": (
            "TestDrainLiveRemovesOnlyTheSupersededGeneration",
            "TestDrainLiveLeavesNonTerminalGenerationsWhole",
            "TestDrainLiveWaitsForRunningSearchDocumentWork",
            "TestDrainLiveCancelledMidLoopConverges",
            "TestDrainLiveResweepRemovesARowInsertedBehindTheCursor",
            "TestDrainLiveSkipsAGenerationAnotherSessionHolds",
        ),
    },
}
