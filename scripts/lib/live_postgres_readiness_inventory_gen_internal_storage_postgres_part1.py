#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/active_code_call_symbols_ahead_manifest_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated",
        ),
        "go/internal/storage/postgres/active_symbol_loader_composer_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAnchorsComposerKeys",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderKeepsNpmAndComposerNamesApart",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanForUnpublishedComposerPackage",
        ),
        "go/internal/storage/postgres/active_symbol_loader_member_keys_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesStaticMemberKeys",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesPHPInstanceKeys",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderResolvesPHPFunctionKeys",
        ),
        "go/internal/storage/postgres/active_symbol_loader_package_fixture_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsFixturePackageManifests",
            "TestFixtureDirPatternAgreesBetweenPostgresAndGo",
        ),
        "go/internal/storage/postgres/alert_fanout_cursor_live_test.go": (
            "TestSecurityAlertFanoutCursorLive",
        ),
        "go/internal/storage/postgres/alert_read_state_commit_live_test.go": (
            "TestSecurityAlertReadStateLiveCommit",
        ),
        "go/internal/storage/postgres/alert_read_state_concurrency_live_test.go": (
            "TestSecurityAlertReadStateLiveConcurrency",
        ),
        "go/internal/storage/postgres/alert_shard_planner_cost_live_test.go": (
            "TestShardPlannerScanCostLive",
        ),
        "go/internal/storage/postgres/alert_shard_retirement_live_test.go": (
            "TestShardRetirementLive",
        ),
        "go/internal/storage/postgres/alert_shard_split_merge_live_test.go": (
            "TestShardSplitMergeLive",
        ),
        "go/internal/storage/postgres/aws_cloud_runtime_drift_state_arn_differential_live_test.go": (
            "TestAWSCloudRuntimeDriftStateARNJoinMatchesReferenceLive",
        ),
        "go/internal/storage/postgres/aws_cloud_runtime_drift_state_arn_plan_live_test.go": (
            "TestAWSCloudRuntimeDriftStateARNJoinProbesARNIndexLive",
        ),
        "go/internal/storage/postgres/blockage_holder_live_test.go": (
            "TestReducerConflictBlockageNamesHolderDomain",
            "TestActiveWorkSummaryHolderLookupScansLeasesOnce",
        ),
        "go/internal/storage/postgres/collector_terminal_failure_ack_live_test.go": (
            "TestTerminalFailureAckStoreRefusalsLive",
            "TestTerminalFailureAckStoreRecordsAndSuppressesLive",
            "TestTerminalFailureAckStoreConcurrentLive",
            "TestTerminalFailureAckStoreRacesFailureWritesLive",
        ),
        "go/internal/storage/postgres/collector_terminal_failure_list_live_test.go": (
            "TestTerminalFailureListSemanticsLive",
            "TestTerminalFailureListPagesAreDisjointLive",
            "TestTerminalFailureListNeverCarriesMessageOrReasonLive",
            "TestTerminalFailureListUsesTheWindowIndexLive",
            "TestCollectorFailureDiscoveryAcknowledgeLoopLive",
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
        "go/internal/storage/postgres/content_file_upsert_guard_bench_live_test.go": (
            "TestContentFileUpsertGuardReplayBenchLive",
            "TestContentFileUpsertGuardChangedBatchBenchLive",
        ),
        "go/internal/storage/postgres/content_file_upsert_guard_columns_live_test.go": (
            "TestContentFileUpsertGuardColumnsLive",
            "TestContentFileUpsertGuardUnchangedReplayWritesNoRowWALLive",
        ),
        "go/internal/storage/postgres/content_file_upsert_guard_concurrency_live_test.go": (
            "TestContentFileUpsertGuardConcurrentWritersLive",
            "TestContentFileUpsertGuardRechecksAgainstCommittedRowLive",
        ),
        "go/internal/storage/postgres/content_file_upsert_guard_contract_live_test.go": (
            "TestContentWriterRejectsDuplicatePathInBatchLive",
            "TestContentFileUpsertBatchAcceptsSamePathInDifferentReposLive",
            "TestContentFileUpsertGuardSkippedStatementFiresStatementTriggersLive",
        ),
        "go/internal/storage/postgres/content_file_upsert_guard_live_test.go": (
            "TestContentFileUpsertGuardUnchangedReplayLeavesRowsUntouchedLive",
            "TestContentFileUpsertGuardChangedFileStillUpdatesLive",
        ),
        "go/internal/storage/postgres/content_files_generation_tag_live_test.go": (
            "TestReducerContentionGateContentGenerationTagAheadWriteReadsDirty",
            "TestReducerContentionGateContentGenerationTagActivatedTagReadsClean",
            "TestReducerContentionGateContentGenerationTagManifestLessScopeWithSignalReadsDirty",
            "TestReducerContentionGateContentGenerationTagDangledAndNullTagReadDirty",
            "TestReducerContentionGateContentWriterStampsGenerationTag",
            "TestReducerContentionGateContentWriterBlankGenerationStoresNull",
            "TestReducerContentionGateContentFilesBackfillAttributesOnlyCleanScopes",
            "TestReducerContentionGateContentGenerationTagGoModDirtLegs",
        ),
        "go/internal/storage/postgres/content_files_language_covering_index_replay_live_test.go": (
            "TestContentFilesLanguageCoveringIndexReplayConvergesLive",
        ),
        "go/internal/storage/postgres/content_policy_findings_live_test.go": (
            "TestContentPolicyKeepsEveryRawFindingLive",
        ),
        "go/internal/storage/postgres/content_policy_raw_rows_live_test.go": (
            "TestContentPolicyRawRowsHealsAfterTheNextGeneration",
            "TestContentPolicyRawRowsIsZeroWithoutAPolicy",
        ),
        "go/internal/storage/postgres/content_store_policy_upgrade_live_test.go": (
            "TestContentStorePolicyHealsStoredRowsOnTheNextGeneration",
        ),
        "go/internal/storage/postgres/content_writer_upsert_outcome_bench_live_test.go": (
            "TestContentFileUpsertOutcomePreReadCostLive",
        ),
        "go/internal/storage/postgres/content_writer_upsert_outcome_live_test.go": (
            "TestContentFileUpsertOutcomeMatchesRowVersionsLive",
            "TestContentFileUpsertOutcomeProxyLimitLive",
            "TestContentFileOutcomePreReadTakesNoLockLive",
            "TestContentFileUpsertObserversDoNotChangeRowsLive",
            "TestContentFileUpsertOutcomeConcurrentWritersLive",
        ),
        "go/internal/storage/postgres/content_writer_upsert_outcome_write_bench_live_test.go": (
            "TestContentFileUpsertOutcomeWriteCostLive",
        ),
        "go/internal/storage/postgres/corpus_fence_observer_live_test.go": (
            "TestQueueObserverStoreCorpusFenceCountsRunOnPostgres",
        ),
        "go/internal/storage/postgres/credential_cleanup_purge_live_test.go": (
            "TestCredentialCleanupPurgeFromThePageLive",
        ),
        "go/internal/storage/postgres/credential_cleanup_race_live_test.go": (
            "TestCredentialCleanupPurgeRacesTheContentWriterLive",
        ),
        "go/internal/storage/postgres/dead_letter_recovery_lock_order_live_test.go": (
            "TestPoisonRecoveryHeldRowRaceLive",
            "TestPoisonRecoveryDropsRowChangedBeforeItsLockLive",
            "TestPoisonRecoveryConcurrentWithReplaysNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/delta_union_chain_live_test.go": (
            "TestChainedDeltaProjectsOverNewestFullLive",
        ),
        "go/internal/storage/postgres/delta_union_chain_proof_live_test.go": (
            "TestChainedDeltaUnionMatchesFullAtHeadLive",
            "TestBurstDeltaRefusedThenCumulativeRediffActivatesLive",
            "TestDeltaFencedOnOlderFullRefusesLive",
            "TestChainedDeltaWithDrainedBaseFailsClosedLive",
        ),
        "go/internal/storage/postgres/delta_union_live_test.go": (
            "TestFactStoreListFactsDeltaUnionLive",
            "TestFactStoreListFactsDeltaBaseMissingLive",
            "TestFactStoreReadDeltaUnionActiveLive",
            "TestFactStoreListFactsPrunedBaseRefusesLive",
        ),
        "go/internal/storage/postgres/delta_union_projection_live_test.go": (
            "TestDeltaGenerationProjectsOverUnionBaseLive",
        ),
        "go/internal/storage/postgres/drifted_bucket_skip_live_test.go": (
            "TestDriftedPathologicalBucketSkippedLive",
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
        "go/internal/storage/postgres/generation_retention_chain_live_test.go": (
            "TestGenerationRetentionKeepsCumulativeDeltaBaseLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_insert_wait_live_test.go": (
            "TestGenerationRetentionChunkConcurrentInsertWaitsLive",
        ),
        "go/internal/storage/postgres/generation_retention_chunk_live_test.go": (
            "TestGenerationRetentionChunkedDrainLive",
            "TestGenerationRetentionChunkStatementsRollBackLive",
            "TestGenerationRetentionChunkLockMissDropsSilentlyLive",
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
        "go/internal/storage/postgres/manifest_less_dirty_rule_answer_live_test.go": (
            "TestReducerContentionGateManifestLessDirtyRuleReturnsTheSameDefinitions",
            "TestReducerContentionGateManifestLessDirtyRuleNaiveRuleLosesTheHole",
            "TestReducerContentionGateManifestLessDirtyRuleSeededRedGreen",
        ),
        "go/internal/storage/postgres/manifest_less_dirty_rule_live_test.go": (
            "TestReducerContentionGateManifestLessDirtyRuleKeepsEveryScopeThatCanNeedAProducer",
            "TestReducerContentionGateManifestLessDirtyRuleMatchesUncoveredProjectionWriters",
        ),
        "go/internal/storage/postgres/migration_169_reader_bound_live_test.go": (
            "TestMigration169PhasedBackfillReaderBoundLive",
        ),
        "go/internal/storage/postgres/package_manifest_central_pin_live_test.go": (
            "TestNotCentralVersionDeclarationSQLAgreesWithGoLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_backfill_live_test.go": (
            "TestPackageManifestConsumptionBackfillRepairsOldWriterAfterReadyLive",
            "TestPackageManifestConsumptionBackfillPagesHeavyScopeLive",
            "TestPackageManifestConsumptionBackfillWaitsForScopeWriterLive",
            "TestPackageManifestConsumptionBackfillBoundsTwentyFiveScopePassLive",
            "TestPackageManifestConsumptionBackfillConcurrentPassesAreIdempotentLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_migration_upgrade_live_test.go": (
            "TestPackageManifestConsumptionMigrationsUpgradeAfterSecretLinesLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_fence_live_test.go": (
            "TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive",
            "TestPackageManifestConsumptionScopeRebuildKeepsConcurrentWriterKeysLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_lock_live_test.go": (
            "TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive",
            "TestPackageManifestConsumptionScopeRebuildLockTimeoutLeavesScopeUnreadyLive",
            "TestPackageManifestConsumptionScopeRebuildResumesAfterPageFailureAndCancelLive",
        ),
        "go/internal/storage/postgres/package_manifest_consumption_page_plan_live_test.go": (
            "TestPackageManifestConsumptionScopeFactsPageUsesBackfillIndexLive",
        ),
        "go/internal/storage/postgres/package_manifest_sbt_artifact_live_test.go": (
            "TestListActivePackageManifestDependencyFactsReachesGradleAndSBTLive",
        ),
        "go/internal/storage/postgres/package_manifest_sbt_repair_live_test.go": (
            "TestPackageManifestSBTRepairOldLogicRunLeavesRepairStateUntouched",
            "TestPackageManifestSBTRepairAddsKeysWithoutFlippingReadiness",
            "TestPackageManifestSBTRepairIsANoOpWithoutSBTRows",
            "TestPackageManifestSBTRepairWaitsForTheV2Marker",
        ),
        "go/internal/storage/postgres/package_manifest_sbt_repair_lock_live_test.go": (
            "TestPackageManifestSBTRepairResumesAtItsCursorAfterAFailedPage",
            "TestPackageManifestSBTRepairReturnsLockTimeoutWithoutAdvancing",
            "TestPackageManifestSBTRepairNeverInsertsAStaleKeyBehindARewrite",
        ),
        "go/internal/storage/postgres/prefetch_batch_plan_live_test.go": (
            "TestPrefetchBatchQueriesUsePrimaryKeyLive",
        ),
        "go/internal/storage/postgres/projector_ack_supersede_lock_order_live_test.go": (
            "TestProjectorAckSupersedeHeldRowRaceLive",
            "TestProjectorAckSupersedeDropsRowClaimedBeforeItsLockLive",
            "TestProjectorAckSupersedeConcurrentWithReplaysNeverDeadlockLive",
            "TestProjectorAckSupersedeKeepsLockTimeoutLive",
        ),
        "go/internal/storage/postgres/projector_marked_write_age_live_test.go": (
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeLive",
            "TestQueueObserverStoreProjectorMarkedWriteOldestAgeEmptyLive",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_contention_live_test.go": (
            "TestProjectorAckJournalContention",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_cost_live_test.go": (
            "TestProjectorAckJournalCost",
            "TestProjectorAckJournalCostCacheImbalance",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_equivalence_live_test.go": (
            "TestProjectorAckJournalEquivalence",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_fence_live_test.go": (
            "TestProjectorAckJournalLockFences",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_inversion_live_test.go": (
            "TestProjectorAckJournalInversion",
            "TestProjectorAckJournalGateHeldByPass",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_live_test.go": (
            "TestProjectorAckJournalAtomicity",
            "TestProjectorAckJournalPriorLive",
            "TestProjectorAckJournalOffWritesNothing",
        ),
        "go/internal/storage/postgres/projector_queue_ack_journal_sweeper_retention_live_test.go": (
            "TestSweeperActivationSurvivesRetentionPrune",
        ),
        "go/internal/storage/postgres/projector_queue_ack_obsolete_marker_live_test.go": (
            "TestProjectorAckSeesLockTimeMarkerTruth",
            "TestProjectorAckObsoleteSkipsFencedBusyScope",
            "TestProjectorAckMarkerCommitRace",
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
        "go/internal/storage/postgres/projector_queue_claim_marked_guard_contention_live_test.go": (
            "TestProjectorClaimMarkedGuardContention",
        ),
    },
}
