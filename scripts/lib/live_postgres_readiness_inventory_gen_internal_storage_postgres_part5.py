#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres": {
        "go/internal/storage/postgres/supply_chain_impact_advisory_group_winners_live_test.go": (
            "TestAdvisoryGroupWinnersQualifyingLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_authorized_loader_engaged_live_test.go": (
            "TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive",
            "TestAuthorizedLoaderEngagedKeepsAGoReachabilityWarningStaticLive",
            "TestAuthorizedLoaderEngagedRefusesAnImageAnalyzerWarningWithoutReceiptLive",
            "TestAuthorizedHandleEngagedLoadsAGoReachabilityWarningThroughTheGoStageLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_cve_chunks_live_test.go": (
            "TestSupplyChainImpactCVEChunksMatchTheWholeStatementLive",
            "TestSupplyChainImpactCVEChunkPlansStayOnAnchorLive",
            "TestSupplyChainImpactCVEChunksShareOneCachedStatementLive",
            "TestSupplyChainImpactCVEChunkCostLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_cve_chunks_walk_live_test.go": (
            "TestSupplyChainImpactCVEBoundStaysBelowTheWalkLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_cve_stats_live_test.go": (
            "TestSupplyChainImpactCVEStatsHoldProbePastTheWalkLive",
            "TestSupplyChainImpactSecurityAlertCVEProbeBeforeAfterStatsLive",
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
            "TestSupplyChainImpactUpstreamPlanHoldsAtEveryChunkSizeLive",
            "TestSupplyChainImpactUpstreamListReadsOnlySignalsLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_first_seen_11031_live_test.go": (
            "TestSupplyChainImpactFirstSeenStamp11031Live",
        ),
        "go/internal/storage/postgres/supply_chain_impact_fleet_gate_9875_live_test.go": (
            "TestFleetAcceptanceGateKEVReachesServiceRollup9875Live",
            "TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live",
        ),
        "go/internal/storage/postgres/supply_chain_impact_go_evidence_lock_live_test.go": (
            "TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive",
            "TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive",
            "TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive",
            "TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_go_evidence_lock_marker_live_test.go": (
            "TestFootprintViolationNamesTheSeededFootprints",
            "TestGoEvidenceRealHandlerPassFootprintLive",
            "TestGoEvidenceFenceQueuesBehindPassPausedBeforeItsMarkerLive",
            "TestGoEvidenceMarkerWaitsBehindAFenceStyleReopenLive",
            "TestGoEvidenceMarkerWaitsBehindATableLockWithoutDeadlockLive",
            "TestGoEvidenceMarkerRowHeldThreeSecondsFenceTimesOutLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_go_evidence_lock_protected_live_test.go": (
            "TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_go_evidence_lock_race_live_test.go": (
            "TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive",
            "TestGoEvidenceAckCommitsWhilePassHoldsTheSharedLockLive",
            "TestGoEvidenceMultiRepositoryPassWithParallelFencesLive",
            "TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive",
            "TestGoEvidenceFreeInterleavingLeavesNoStaleVerdictLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_go_evidence_lock_suppression_live_test.go": (
            "TestGoEvidenceSuppressionPassAndFenceNeverDeadlockLive",
            "TestGoEvidenceSuppressionCandidateReadIgnoresATableLockLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_protected_static_publish_live_test.go": (
            "TestSupplyChainImpactProtectedStaticPublishLive",
            "TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_drain_concurrency_live_test.go": (
            "TestSupplyChainImpactSupportsDrainConcurrentWithWritesLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_drain_live_test.go": (
            "TestSupplyChainImpactSupportsDrainEqualsFactReadOnAdversarialSeedsLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_many_repositories_live_test.go": (
            "TestSupplyChainAdvisoriesForReposManySmallRepositoriesLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_page_plan_live_test.go": (
            "TestSupplyChainImpactSupportsPagePlanGuardLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_reader_live_test.go": (
            "TestSecurityAlertReconciliationAndAdvisoryLoadersEqualFactReadsLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_supports_repository_arm_live_test.go": (
            "TestRepositoryArmBoundsLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_upstream_nvd_widen_live_test.go": (
            "TestSupplyChainImpactUpstreamWidenedProbeHoldsNVDScaleLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_hotkey_staging_shim_live_test.go": (
            "TestSupplyChainImpactWinnersHotkeyStagingShimLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_landing_distinct_total_cost_live_test.go": (
            "TestSupplyChainImpactWinnersLandingDistinctTotalCostLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_landing_multibatch_interleave_live_test.go": (
            "TestSupplyChainImpactWinnersLandingMultibatchInterleaveLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_staging_collapse_live_test.go": (
            "TestSupplyChainImpactWinnersStagingCollapseConservationLive",
            "TestSupplyChainImpactWinnersStagingCollapseDeletesExactlyOneRowLive",
            "TestSupplyChainImpactWinnersStagingReMarkSurvivesInflightSettleLive",
            "TestSupplyChainImpactWinnersStagingCollapseSplitKeepsStagingUnionHorizonLive",
            "TestSupplyChainImpactWinnersStagingInterleaveDeadlockFreeLive",
            "TestSupplyChainImpactWinnersStagingCollapseWorstPartitionCostLive",
            "TestSupplyChainImpactWinnersStagingCollapseExplainPinsKeyOrderLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_staging_late_seq_live_test.go": (
            "TestSupplyChainImpactWinnersStagingLateSeqSurvivesRangedCollapseLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_staging_plan_live_test.go": (
            "TestSupplyChainImpactWinnersStagingStatementsAvoidSeqScanLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_staging_work_mem_live_test.go": (
            "TestSupplyChainImpactWinnersStagingCollapseWorkMemScopedLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_winners_writer_txn_replay_live_test.go": (
            "TestSupplyChainImpactWinnersResweepClaimIsCrossBackendLive",
            "TestSupplyChainImpactWinnersWriterTxnReplayLive",
        ),
        "go/internal/storage/postgres/terms_foreign_key_migration_budget_live_test.go": (
            "TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive",
        ),
        "go/internal/storage/postgres/trigger_bootstrap_parity_live_test.go": (
            "TestWebhookTriggerStoreRunsOnBootstrapSchemaLive",
        ),
        "go/internal/storage/postgres/unchanged_skip_bench_live_test.go": (
            "TestUnchangedSkipGuardBeforeAfter",
        ),
        "go/internal/storage/postgres/unchanged_skip_fencing_live_test.go": (
            "TestUpsertFactBatchUnchangedGuardFencingMatrix",
            "TestUpsertFactBatchUnchangedGuardTombstones",
            "TestUpsertStreamingFactsAdmitsAllSkippedReplay",
            "TestUpsertFactBatchUnchangedGuardConcurrentReplay",
            "TestUpsertFactsUnchangedReplayAddsNoUpdates",
        ),
        "go/internal/storage/postgres/unchanged_skip_proof_live_test.go": (
            "TestUpsertFactBatchSkipsUnchangedRow",
            "TestUpsertFactBatchUnchangedGuardChangeMatrix",
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
