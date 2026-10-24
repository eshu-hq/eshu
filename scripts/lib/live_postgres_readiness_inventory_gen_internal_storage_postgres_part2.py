#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
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
        "go/internal/storage/postgres/repository_catalog_projection_live_test.go": (
            "TestLoadRepositoryCatalogProjectionMatchesWholePayloadDecode",
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
        "go/internal/storage/postgres/successor_guard_recheck_plan_live_test.go": (
            "TestGuardRecheckReadPlansLive",
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
            "TestWorkflowClaimPacingIdleDrainHoldsFloor",
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
