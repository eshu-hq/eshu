#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
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
        "go/internal/storage/postgres/reducer_queue_code_call_delta_scope_live_test.go": (
            "TestCodeCallDeltaScopeUnusableDeadLettersOnFirstAttemptLive",
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
        "go/internal/storage/postgres/repository_retirements_derived_explain_live_test.go": (
            "TestRetirementDerivedStatementPlansLive",
        ),
        "go/internal/storage/postgres/repository_retirements_derived_live_test.go": (
            "TestRetirementRetiresDerivedScopeLive",
            "TestRetirementDerivedScopeCapturesWinnersDirtyLive",
            "TestRetirementFenceBudgetAttributesDerivedScopeLive",
            "TestRetirementCaptureIncludesDerivedScopeLive",
        ),
        "go/internal/storage/postgres/repository_retirements_derived_race_live_test.go": (
            "TestRetirementRefusesLateDerivedCommitLive",
            "TestRetirementWaitsForInFlightFirstDerivedCommitLive",
            "TestRetirementPurgeBusyWhileAckHoldsDerivedScopeLive",
            "TestRetirementFenceWaitsForLiveDerivedProjectorLeaseLive",
            "TestRetirementRefusesStaleDerivedAckAfterFenceLive",
            "TestRetirementDerivedScopeFenceAndPurgeAreIdempotentLive",
            "TestDerivedCommitAndMaintenanceLeaderDoNotDeadlockLive",
        ),
        "go/internal/storage/postgres/run_reconciliation_factless_live_test.go": (
            "TestWorkflowControlStoreIntegrationReconcileConvergesFactlessClaim",
            "TestWorkflowControlStoreIntegrationReconcileHoldsFactCommittedClaim",
        ),
        "go/internal/storage/postgres/run_reconciliation_terminal_siblings_live_test.go": (
            "TestTerminalSiblingsStayOpenThroughReconcileGuardAndClaimLive",
            "TestTerminalSiblingsEveryPathBackToPendingKeepsRunOpenLive",
            "TestTerminalSiblingsFailedRunItemsStayFrozenLive",
            "TestTerminalSiblingsNoDuplicateUnderConcurrentLifecycleLive",
        ),
        "go/internal/storage/postgres/satisfied_by_read_delta_live_test.go": (
            "TestListActiveCrossplaneXRDFactsDeltaActiveScopeLive",
            "TestCrossplaneSatisfiedByDeltaKeepsUnchangedEdgesLive",
        ),
        "go/internal/storage/postgres/satisfied_by_read_index_live_test.go": (
            "TestCrossplaneXRDReadsUseThePartialIndexLive",
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
        "go/internal/storage/postgres/stored_advisory_names_live_test.go": (
            "TestListStoredAdvisoryPackageNamesReturnsDroppedNameLive",
            "TestListStoredAdvisoryPackageNamesChunksTheIDListLive",
        ),
        "go/internal/storage/postgres/successor_guard_recheck_plan_live_test.go": (
            "TestGuardRecheckReadPlansLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_authorized_loader_engaged_live_test.go": (
            "TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_disengaged_build_live_test.go": (
            "TestAuthorizedImpactLoaderDisengagedBuildsAdvisoryFindingLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_evidence_live_test.go": (
            "TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive",
            "TestSupplyChainImpactActiveEvidenceBufferBudgetLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_evidence_plan_live_test.go": (
            "TestSupplyChainImpactBranchPlansStayOnAnchorsLive",
            "TestSupplyChainImpactNewIndexInsertCostLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_fleet_gate_9875_live_test.go": (
            "TestFleetAcceptanceGateKEVReachesServiceRollup9875Live",
            "TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live",
        ),
        "go/internal/storage/postgres/supply_chain_impact_protected_static_publish_live_test.go": (
            "TestSupplyChainImpactProtectedStaticPublishLive",
            "TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive",
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
            "TestWorkflowClaimPacingFailsOpenOnLatchAdvanceFailure",
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
