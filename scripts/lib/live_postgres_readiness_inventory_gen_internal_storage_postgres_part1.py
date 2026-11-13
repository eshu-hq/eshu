#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/actions_work_live_test.go": (
            "TestCICDActionsAdmissionBeforeACKCrashUsesExactDurableWorkLive",
            "TestCICDMixedProviderScheduledGroupsShareDurableRunLive",
        ),
        "go/internal/storage/postgres/active_code_call_symbols_ahead_manifest_live_test.go": (
            "TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole",
            "TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated",
        ),
        "go/internal/storage/postgres/active_code_call_symbols_delta_live_test.go": (
            "TestLoadActiveCodeCallSymbolDefinitionFactsDeltaActiveScopeLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsDeltaStatementsLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsNoDeltaStatementsLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsDeltaAnchoredBaseBindingLive",
            "TestDeriveDeltaOverlayBaseQueryForCodeCallReaders",
            "TestLoadActiveCodeCallSymbolDefinitionFactsManifestChangingDeltaLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsManifestStableAnchoredLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsAnchoredNoDeltaStatementsLive",
            "TestLoadActiveCodeCallSymbolDefinitionFactsUnionExcludesVendoredNonProducerLive",
        ),
        "go/internal/storage/postgres/active_code_call_symbols_variant_b_live_test.go": (
            "TestLoadActiveCodeCallSymbolDefinitionFactsDroppedPublisherVariantBLive",
        ),
        "go/internal/storage/postgres/active_jvm_reachability_delta_live_test.go": (
            "TestListActiveJVMReachabilityFactsDeltaActiveScopeLive",
            "TestListActiveJVMReachabilityFactsDeltaStatementsLive",
            "TestListActiveJVMReachabilityFactsNoDeltaStatementsLive",
        ),
        "go/internal/storage/postgres/active_package_manifest_dependency_case_live_test.go": (
            "TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesLive",
            "TestListActivePackageManifestDependencyFactsLoadsMixedCaseNamesThroughSBTQueryLive",
        ),
        "go/internal/storage/postgres/active_package_manifest_dependency_delta_cases_live_test.go": (
            "TestListActivePackageManifestDependencyFactsDeltaOverridesBasePinLive",
            "TestListActivePackageManifestDependencyFactsDeltaDeletesManifestPathLive",
            "TestListActivePackageManifestDependencyFactsDeltaTombstonesPinLive",
            "TestListActivePackageManifestDependencyFactsChainedDeltasLive",
            "TestListActivePackageManifestDependencyFactsDeltaBaseUnavailableLive",
            "TestListActivePackageManifestDependencyFactsDeltaWithoutUnionLive",
            "TestListActivePackageManifestDependencyFactsDeltaMavenFamilyLive",
            "TestListActivePackageManifestDependencyFactsFullActiveScopeUnchangedLive",
            "TestAuthorizedImpactLoaderCountsManifestDeltaUnionLive",
            "TestAuthorizedImpactLoaderCountsManifestDeltaUnionEngagedLive",
        ),
        "go/internal/storage/postgres/active_package_manifest_dependency_delta_live_test.go": (
            "TestListActivePackageManifestDependencyFactsDeltaActiveScopeLive",
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
        "go/internal/storage/postgres/advisory_groups_synthetic_seed_live_test.go": (
            "TestAdvisoryGroupsSyntheticSeedLive",
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
        "go/internal/storage/postgres/collector_evidence_lateral_live_test.go": (
            "TestCollectorFactEvidenceLateralMatchesPreChangeJoinLive",
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
        "go/internal/storage/postgres/content_store_metadata_only_live_test.go": (
            "TestContentStoreSingleRowMetadataOnlyPreservesIndexedAtLive",
        ),
        "go/internal/storage/postgres/content_store_policy_upgrade_live_test.go": (
            "TestContentStorePolicyHealsStoredRowsOnTheNextGeneration",
        ),
        "go/internal/storage/postgres/content_writer_metadata_only_hot_live_test.go": (
            "TestContentWriterMetadataOnlyRewriteIsHOTLive",
            "TestContentWriterUnobservedRewriteIsHOTLive",
            "TestContentWriterUnchangedReplayStaysHOTLive",
            "TestContentWriterObservedAndUnobservedRewriteAlikeLive",
            "TestContentWriterMetadataOnlyConcurrentNoTornRowsLive",
        ),
        "go/internal/storage/postgres/content_writer_metadata_only_redarm_live_test.go": (
            "TestContentWriterMetadataOnlyIndexedAtMoveIsNotHOTLive",
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
        "go/internal/storage/postgres/control_claim_stats_concurrency_live_test.go": (
            "TestWorkflowStatsAnalyzeDuringClaims",
        ),
        "go/internal/storage/postgres/control_claim_stats_live_test.go": (
            "TestWorkflowClaimColdTableTheory",
            "TestWorkflowClaimFirstClaimAfterBulkAdmission",
            "TestWorkflowClaimFirstClaimAfterBulkAdmissionOntoStatlessTable",
        ),
        "go/internal/storage/postgres/control_retire_unclaimed_live_test.go": (
            "TestRetireUnclaimedLiveHappyPath",
            "TestRetireUnclaimedLiveIdempotent",
            "TestRetireUnclaimedLiveGateRefusals",
            "TestRetireUnclaimedLiveClaimRace",
            "TestRetireUnclaimedLiveHeldRow",
            "TestRetireUnclaimedLiveReconcileClosesRun",
            "TestRetireUnclaimedLiveGuardRelease",
            "TestRetireUnclaimedLiveReadsUnaffected",
            "TestRetireUnclaimedLiveRecheckAfterReactivation",
        ),
        "go/internal/storage/postgres/control_scanner_admission_atomicity_live_test.go": (
            "TestScannerAdmissionRejectsConflictingDuplicateSourceLive",
        ),
        "go/internal/storage/postgres/control_scanner_discovery_scale_live_test.go": (
            "TestScannerDiscoveryScaleShapeLive",
        ),
        "go/internal/storage/postgres/control_scanner_targets_live_test.go": (
            "TestScannerImageTargetAdmissionAndCompletionLive",
            "TestScannerMigration241RefusesNonPublicSchemaLive",
            "TestScannerProtectedApiIgnoresTempShadowLive",
            "TestScannerImageTargetConcurrentCrossInstanceAdmissionLive",
        ),
        "go/internal/storage/postgres/control_scanner_targets_tenant_live_test.go": (
            "TestScannerImageTargetHostedBoundaryAndGrantRevocationLive",
        ),
        "go/internal/storage/postgres/corpus_fence_observer_live_test.go": (
            "TestQueueObserverStoreCorpusFenceCountsRunOnPostgres",
        ),
        "go/internal/storage/postgres/credential_cleanup_entity_guard_live_test.go": (
            "TestCredentialCleanupEntityUnderPostWatermarkFileLive",
        ),
        "go/internal/storage/postgres/credential_cleanup_purge_live_test.go": (
            "TestCredentialCleanupPurgeFromThePageLive",
        ),
        "go/internal/storage/postgres/credential_cleanup_purge_revision_live_test.go": (
            "TestCredentialCleanupPurgeBumpsSearchRevisionsLive",
        ),
        "go/internal/storage/postgres/credential_cleanup_race_live_test.go": (
            "TestCredentialCleanupPurgeRacesTheContentWriterLive",
            "TestCredentialCleanupPurgeABRacesTheSearchDocumentWriterLive",
            "TestCredentialCleanupPurgeABGuardRemovalDeletesRebuiltLive",
        ),
        "go/internal/storage/postgres/dead_letter_recovery_lock_order_live_test.go": (
            "TestPoisonRecoveryHeldRowRaceLive",
            "TestPoisonRecoveryDropsRowChangedBeforeItsLockLive",
            "TestPoisonRecoveryConcurrentWithReplaysNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/deferred_maintenance_barrier_two_shard_live_test.go": (
            "TestDeferredMaintenanceBarrierTwoShardCompletionLive",
        ),
        "go/internal/storage/postgres/delta_union_base_live_test.go": (
            "TestResolveDeltaBasesMatchesPerScopeRuleLive",
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
        "go/internal/storage/postgres/drift_overlay_tfstate_live_test.go": (
            "TestTerraformDriftOverlayReadsLive",
            "TestTerraformDriftOverlayConfigRejectsStaleAnchor",
            "TestTerraformDriftOverlayConfigRejectsStaleFullAnchor",
            "TestAWSRuntimeDriftEvidenceOverlaidAnchorSeesBaseConfigLive",
        ),
    },
}
