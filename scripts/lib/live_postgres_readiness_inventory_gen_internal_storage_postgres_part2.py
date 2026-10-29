#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
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
        "go/internal/storage/postgres/producer_ingestion_acl_live_test.go": (
            "TestProducerIngestionACLsLive",
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
        "go/internal/storage/postgres/recovery_refinalize_delta_active_fence_live_test.go": (
            "TestRefinalizeDeltaActiveRollsBackReindexWithTheTransaction",
            "TestRefinalizeDeltaActiveReindexLockContention",
        ),
        "go/internal/storage/postgres/recovery_refinalize_delta_active_live_test.go": (
            "TestRefinalizeDeltaActiveRequestsFullReindex",
            "TestRefinalizeDeltaActiveConvergesAcrossTwoCalls",
        ),
        "go/internal/storage/postgres/recovery_refinalize_fence_admission_live_test.go": (
            "TestRefinalizeFenceAdmissionDeadlineStopsAfterFenceAcquire",
            "TestRefinalizeFenceAdmissionPersistentHolderExhaustsWithoutConvoy",
            "TestRefinalizeFenceAdmissionBoundsRetryBegin",
            "TestRefinalizeFenceAdmissionCallerCancellation",
            "TestRefinalizeFenceAdmissionWriteAndCommitFailuresKeepTheirCauses",
        ),
        "go/internal/storage/postgres/recovery_refinalize_lock_convoy_live_test.go": (
            "TestRefinalizeFenceDoesNotConvoyUnrelatedQueueWrite",
            "TestRefinalizeFenceRetryBudgetIncludesReducerDrain",
        ),
        "go/internal/storage/postgres/recovery_replay_lock_order_interleave_live_test.go": (
            "TestRecoveryReplayTwoSessionsBlockOneWayLive",
            "TestRecoveryReplayHeldRowRaceLive",
            "TestRecoveryReplayDropsRowChangedBeforeItsLockLive",
        ),
        "go/internal/storage/postgres/recovery_replay_lock_order_live_test.go": (
            "TestRecoveryReplayConcurrentReplaysNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/reducer_claim_sweep_lock_order_live_test.go": (
            "TestReducerClaimSweepAndReplayBlockOneWayLive",
            "TestReducerClaimSweepHeldRowRaceLive",
            "TestReducerClaimSweepConcurrentWithReplaysNeverDeadlockLive",
            "TestReducerClaimSweepKeepsActiveGenerationRowsLive",
            "TestReducerClaimSweepDropsRowClaimedBeforeItsLockLive",
        ),
        "go/internal/storage/postgres/reducer_heartbeat_startup_window_live_test.go": (
            "TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_ack_reclaim_live_test.go": (
            "TestReducerQueueAckBatchFencesSupersededClaimLive",
            "TestReducerQueueExpiryInterleaveLive",
        ),
        "go/internal/storage/postgres/reducer_queue_producer_refinalize_live_test.go": (
            "TestOwnGateWaitsThroughRefinalizeReResolutionLive",
        ),
        "go/internal/storage/postgres/reducer_queue_producer_terminal_live_test.go": (
            "TestOwnGateProducerDeadLetteredConsumerDeadLettersLive",
            "TestOwnGateProducerRetryingConsumerKeepsDeferringLive",
        ),
        "go/internal/storage/postgres/reducer_queue_security_alert_impact_wait_concurrent_live_test.go": (
            "TestReducerContentionGateSecurityAlertReconciliationImpactFenceUnderContention",
        ),
        "go/internal/storage/postgres/reducer_queue_security_alert_impact_wait_live_test.go": (
            "TestReducerContentionGateSecurityAlertReconciliationImpactFenceKeepsItsAttemptBudget",
            "TestReducerContentionGateSecurityAlertReconciliationGivesUpVisiblyWithoutAFence",
        ),
        "go/internal/storage/postgres/reducer_queue_security_alert_settled_skip_live_test.go": (
            "TestReducerContentionGateSecurityAlertReconciliationSettledSkipsTheWait",
            "TestReducerContentionGateSecurityAlertReconciliationLateProducerKeepsItsWait",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_bound_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceBoundsOnProducerNotCatalogAge",
            "TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerIsWorked",
            "TestReducerContentionGateServiceCatalogRuntimeFenceConvergesAtTheElapsedBound",
            "TestReducerContentionGateServiceCatalogRuntimeFenceDefersWhileProducerLeaseIsLive",
            "TestReducerContentionGateServiceCatalogRuntimeFenceReopenedProducerWaits",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_interleave_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceProducerCompletesMidCheck",
            "TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_readiness_live_test.go": (
            "TestReducerContentionGateServiceCatalogRuntimeFenceKeepsItsAttemptBudget",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_retrigger_batch_live_test.go": (
            "TestReducerContentionGateServiceCatalogRetriggerBatchAckAndTelemetry",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_retrigger_fence_live_test.go": (
            "TestReducerContentionGateServiceCatalogFenceWaitsOnRunsOnIntent",
            "TestReducerContentionGateServiceCatalogConcurrentAcksReopenOnce",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_retrigger_live_test.go": (
            "TestReducerContentionGateServiceCatalogRetriggerRepairsNoProducerGeneration",
            "TestReducerContentionGateServiceCatalogRetriggerFlagsClaimedRow",
            "TestReducerContentionGateServiceCatalogRetriggerFlagsFirstRunWithoutDecisions",
        ),
        "go/internal/storage/postgres/reducer_queue_service_catalog_runtime_retrigger_window_live_test.go": (
            "TestReducerContentionGateServiceCatalogRetriggerFlagsRunThatNewlyNamesRepo",
        ),
        "go/internal/storage/postgres/relationship_activation_wake_live_test.go": (
            "TestActivationWakesOwnGateConsumersLive",
        ),
        "go/internal/storage/postgres/relationship_activation_wake_race_live_test.go": (
            "TestActivationWaitsForAnInflightFinalFailLive",
            "TestFinalFailAfterTheActivationLockIsWokenLive",
            "TestActivationLockTimeoutFailsRetryablyLive",
        ),
        "go/internal/storage/postgres/relationship_store_completion_wake_contention_live_test.go": (
            "TestCompletionWakeLockTimeoutClassifiedLive",
            "TestCompletionWakeEvalPlanQualDropsReopenedRowLive",
            "TestCompletionWakeConcurrentWithMaintenanceAndClaimLive",
            "TestCompletionWakeMetricsLive",
        ),
        "go/internal/storage/postgres/relationship_store_completion_wake_live_test.go": (
            "TestCompletionWakeFloorMissingAndUnchangedLive",
            "TestCompletionWakeRunningItemReplayBitLive",
            "TestCompletionWakeClaimedMissingFingerprintNotFlaggedLive",
            "TestCompletionWakeRetryConvergesAfterLostWakeLive",
        ),
        "go/internal/storage/postgres/reopen_stamp_grace_live_test.go": (
            "TestDeadLetterReplayGetsBoundedFreshReadinessGraceLive",
        ),
        "go/internal/storage/postgres/reopen_stamp_race_live_test.go": (
            "TestReopenStampConcurrentWritersLive",
        ),
        "go/internal/storage/postgres/reopen_stamp_writers_live_test.go": (
            "TestTerminalReopenWritersStampReopenedAtLive",
        ),
        "go/internal/storage/postgres/repository_catalog_projection_live_test.go": (
            "TestLoadRepositoryCatalogProjectionMatchesWholePayloadDecode",
        ),
        "go/internal/storage/postgres/run_reconciliation_factless_live_test.go": (
            "TestWorkflowControlStoreIntegrationReconcileConvergesFactlessClaim",
            "TestWorkflowControlStoreIntegrationReconcileHoldsFactCommittedClaim",
        ),
        "go/internal/storage/postgres/security_alert_impact_producer_store_live_test.go": (
            "TestReducerContentionGateSecurityAlertImpactProducerStateScopesToTheGeneration",
            "TestReducerContentionGateSecurityAlertImpactProducerStatePlanUsesScopeGenerationIndex",
        ),
        "go/internal/storage/postgres/service_catalog_runtime_producer_store_live_test.go": (
            "TestReducerContentionGateRuntimeProducerStatesActiveGenerationOnly",
            "TestReducerContentionGateRuntimeProducerStatesNewestInFlightWorkableAt",
            "TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex",
        ),
        "go/internal/storage/postgres/shared_intent_pending_age_plan_live_test.go": (
            "TestSharedIntentPendingAgeIndexOnlyScanLive",
        ),
        "go/internal/storage/postgres/successor_guard_recheck_plan_live_test.go": (
            "TestGuardRecheckReadPlansLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_authorized_loader_live_test.go": (
            "TestAuthorizedImpactLoaderAdmitsCompletedScanLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_evidence_live_test.go": (
            "TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive",
            "TestSupplyChainImpactActiveEvidenceBufferBudgetLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_evidence_plan_live_test.go": (
            "TestSupplyChainImpactBranchPlansStayOnAnchorsLive",
            "TestSupplyChainImpactNewIndexInsertCostLive",
        ),
        "go/internal/storage/postgres/terms_foreign_key_migration_budget_live_test.go": (
            "TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive",
        ),
        "go/internal/storage/postgres/trigger_bootstrap_parity_live_test.go": (
            "TestWebhookTriggerStoreRunsOnBootstrapSchemaLive",
        ),
        "go/internal/storage/postgres/workflow_control_claim_class_columns_live_test.go": (
            "TestWorkflowClaimClassColumnsRoundTrip",
            "TestWorkflowClaimClassColumnsBackfill",
        ),
        "go/internal/storage/postgres/workflow_control_claim_depth_live_test.go": (
            "TestWorkflowClaimDepthEightClaimersDrainExactlyOnce",
            "TestWorkflowClaimDepthFairRotationServesSmallKeysEarly",
            "TestClaimDepthAnalysisHelpers",
        ),
        "go/internal/storage/postgres/workflow_control_claim_forced_serial_live_test.go": (
            "TestWorkflowClaimPacingForcedSerialTripsOverlapGate",
        ),
        "go/internal/storage/postgres/workflow_control_claim_order_plan_live_test.go": (
            "TestWorkflowClaimCandidateUsesFairClaimIndex",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_depth_live_test.go": (
            "TestWorkflowClaimPacingRecordsDepthPendingGauge",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_failure_live_test.go": (
            "TestWorkflowClaimPacingFailsOpenOnLatchQueryFailure",
            "TestWorkflowClaimPacingStuckHoldsMidBandAndReleasesBelowWatermark",
            "TestWorkflowClaimPacingStuckEmitsOperatorSignals",
        ),
        "go/internal/storage/postgres/workflow_control_claim_pacing_live_test.go": (
            "TestWorkflowClaimPacingEngagesOnDomainDepth",
            "TestWorkflowClaimPacingHysteresisRelease",
            "TestWorkflowClaimPacingFailsOpenOnDepthQueryFailure",
            "TestWorkflowClaimPacingStuckSignal",
            "TestWorkflowClaimPacingGlobalCeiling",
            "TestWorkflowClaimPacingIdleDrainHoldsGrantOverlap",
            "TestWorkflowClaimPacingEngagedDrainCompletesExactlyOnce",
            "TestWorkflowClaimPacingSwitchPreservesEngagedAt",
            "TestWorkflowClaimPacingDepthCacheReuse",
        ),
        "go/internal/storage/postgres/workflow_control_fair_claim_live_test.go": (
            "TestWorkflowFairClaimLotteryMatchesSimulatedDraw",
            "TestWorkflowFairClaimPriorityPrecedesRotation",
            "TestWorkflowFairnessKeyDepthsReportsTopKeysAndDistinctCount",
            "TestWorkflowClaimDepthStaggeredStartHoldsGates",
        ),
        "go/internal/storage/postgres/workflow_control_quota_live_test.go": (
            "TestClaimQuotaOverBudgetThrottlesOnIdleSystem",
            "TestClaimQuotaRacingReplicasCannotOvershoot",
            "TestClaimQuotaStateSurvivesCoordinatorRestart",
            "TestClaimQuotaUnreadableFallsBackToFloorRate",
        ),
        "go/internal/storage/postgres/workflow_control_quota_partition_live_test.go": (
            "TestClaimQuotaScopesPartitionBudget",
        ),
        "go/internal/storage/postgres/workflow_control_quota_plan_live_test.go": (
            "TestClaimQuotaGatedPlanStaysIndexServed",
        ),
        "go/internal/storage/postgres/workflow_terminal_failure_ack_live_test.go": (
            "TestCollectorTerminalFailureAcknowledgementSemanticsLive",
        ),
        "go/internal/storage/postgres/workflow_terminal_failure_index_live_test.go": (
            "TestCollectorTerminalFailureReadUsesPartialIndexLive",
            "TestCollectorTerminalFailureIndexMigrationReappliesLive",
        ),
        "go/internal/storage/postgres/workflow_terminal_failure_status_live_test.go": (
            "TestCollectorTerminalFailureWindowSemanticsLive",
            "TestCollectorTerminalFailureStatusDecidesTerminalLive",
        ),
    },
}
