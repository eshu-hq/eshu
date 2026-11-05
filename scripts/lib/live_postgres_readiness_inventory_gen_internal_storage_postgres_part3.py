#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/manifest_dependency_read_parity_live_test.go": (
            "TestManifestDependencyProbeTextsMatchFlatTextsLive",
            "TestManifestDependencyProbePlanUsesTheFoldedIndexLive",
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
        "go/internal/storage/postgres/nvd_window_ownership_live_test.go": (
            "TestNVDWindowOwnershipAccumulatesAcrossWindowsLive",
            "TestNVDWindowRefusalAndSkipHoldCursorLive",
        ),
        "go/internal/storage/postgres/osv_delta_changed_names_live_test.go": (
            "TestListVulnerabilityChangedNamesLive",
            "TestListVulnerabilityChangedNamesMalformedWithdrawnRidesLive",
        ),
        "go/internal/storage/postgres/osv_delta_replan_burst_live_test.go": (
            "TestOSVDeltaReplanBurstDrainsWithoutLossLive",
        ),
        "go/internal/storage/postgres/osv_delta_replan_live_test.go": (
            "TestOSVDeltaReplanFoldingDifferentialLive",
        ),
        "go/internal/storage/postgres/owned_package_resolver_live_test.go": (
            "TestResolveOwnedPackageNamesDifferentialLive",
            "TestResolveOwnedPackageNamesMatchesTheFeedLive",
            "TestResolveOwnedPackageNamesFindsEverySwiftURLNameLive",
            "TestResolveOwnedPackageNamesFollowsThePlannerRulesLive",
            "TestResolveOwnedPackageNamesChunksLongBatchesLive",
            "TestResolveOwnedPackageNamesSpanLive",
        ),
        "go/internal/storage/postgres/owned_package_targets_alias_live_test.go": (
            "TestListOwnedPackageDependencyTargetsAliasesLive",
        ),
        "go/internal/storage/postgres/owned_package_targets_key_hash_live_test.go": (
            "TestListOwnedPackageDependencyTargetsReportsKeyHashLive",
        ),
        "go/internal/storage/postgres/owned_package_targets_slot_live_test.go": (
            "TestListOwnedPackageDependencyTargetsSlotCoverageLive",
            "TestListOwnedPackageDependencyTargetsSlotWindowStatsLive",
            "TestListOwnedPackageDependencyTargetsSlotDifferentialLive",
        ),
        "go/internal/storage/postgres/owned_target_slot_progress_interval_live_test.go": (
            "TestOwnedTargetSlotProgressSurvivesScanIntervalChangeLive",
        ),
        "go/internal/storage/postgres/owned_target_slot_progress_live_test.go": (
            "TestOwnedTargetSlotProgressLive",
            "TestOwnedTargetSlotProgressSurvivesPFlipLive",
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
        "go/internal/storage/postgres/projector_queue_claim_marker_fence_live_test.go": (
            "TestMarkProjectionWriteStartedFenceSync",
            "TestMarkProjectionWriteStartedMissingFenceRefuses",
        ),
        "go/internal/storage/postgres/projector_queue_dead_letter_live_test.go": (
            "TestProjectorDeadLetterGuardCellsLive",
        ),
        "go/internal/storage/postgres/projector_queue_slot_scope_ack_live_test.go": (
            "TestProjectorAckLeavesOneActiveGenerationPerSlotScope",
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
        "go/internal/storage/postgres/pruned_generation_lock_order_live_test.go": (
            "TestRetentionCascadeHeldRowRaceLive",
            "TestRetentionCascadeDeletesRowChangedBeforeItsLockLive",
            "TestRetentionCascadeConcurrentWithSweepAndReplaysNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/public_advisory_ghsa_reconcile_live_test.go": (
            "TestPublicAdvisoryReconcileRefusesGHSALive",
            "TestPublicAdvisoryReconcileThroughStoreRefusesGHSAConfigLive",
        ),
    },
}
