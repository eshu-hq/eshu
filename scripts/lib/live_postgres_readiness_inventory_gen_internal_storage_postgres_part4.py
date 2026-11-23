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
        "go/internal/storage/postgres/reducer_fact_owner_live_test.go": (
            "TestReducerFactStatementsRefuseAForeignScopeRowLive",
            "TestReducerFactOwnerRaceTwoScopesLive",
            "TestEnqueueRefusesAForeignIntentWorkItemIDLive",
        ),
        "go/internal/storage/postgres/reducer_fact_owner_plan_live_test.go": (
            "TestReducerOwnerLegPreStatementsAreDerived",
            "TestReducerOwnerLegCostLive",
        ),
        "go/internal/storage/postgres/reducer_heartbeat_startup_window_live_test.go": (
            "TestReducerServiceClosesLeaseExpiryStartupWindowAgainstPostgres",
        ),
        "go/internal/storage/postgres/reducer_queue_advisory_coalescing_live_test.go": (
            "TestAdvisoryCoalescingLifecycleLive",
            "TestAdvisoryCoalescingTerminalReopenLive",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_ack_reclaim_live_test.go": (
            "TestReducerQueueAckBatchFencesSupersededClaimLive",
            "TestReducerQueueExpiryInterleaveLive",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_gate_priority_live_test.go": (
            "TestClaimBatchGatePriorityBoostsDrainingGenerationLive",
            "TestClaimBatchGatePriorityCapsAtOneBoostedRowLive",
            "TestClaimBatchGatePriorityExpiresOnDrainLive",
            "TestClaimBatchGatePriorityIgnoresSupersededGenerationLive",
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
        "go/internal/storage/postgres/reducer_queue_retouch_live_test.go": (
            "TestReducerQueueRetouchLive",
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
        "go/internal/storage/postgres/reducer_waiting_backward_evidence_contention_live_test.go": (
            "TestBackwardEvidenceWakeContentionLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_backward_evidence_live_test.go": (
            "TestBackwardEvidenceParkThenPublishWakesLive",
            "TestBackwardEvidencePublishThenParkReopensLive",
            "TestBackwardEvidenceUnrelatedPublishesStayParkedLive",
            "TestBackwardEvidenceOverlapPostCommitRewakeLive",
            "TestBackwardEvidenceMissedWakeAuditReopensLive",
            "TestBackwardEvidenceWokenRowConvergesLive",
            "TestBackwardEvidenceOverlapWithoutRewakeStrandsUntilAuditLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_cross_scope_orders_live_test.go": (
            "TestCrossScopeProducerBothOrdersLive",
            "TestCrossScopeProducerMultiProducerLive",
            "TestCrossScopeProducerWakeNonInterferenceLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_cross_scope_park_live_test.go": (
            "TestCrossScopeProducerWaitParksDomainOnlyLive",
            "TestCrossScopeProducerWaitReopensOnReducerAckLive",
            "TestCrossScopeProducerWaitReopensOnProjectorActivationLive",
            "TestCrossScopeProducerAuditSatisfiesQuiescentWaitLive",
            "TestCrossScopeProducerWaitForCauseThreadsDomainsLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_cross_scope_race_live_test.go": (
            "TestCrossScopeProducerConcurrentWakeRaceLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_key_bound_converge_live_test.go": (
            "TestWideVerdictWaitersConvergeUnderContentionLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_key_bound_live_test.go": (
            "TestFailParksAWideVerdictSetLive",
            "TestServiceSurvivesAWideVerdictDeferralLive",
            "TestWorstCaseAdmittedWaitRowInsertsOnBothIndexesLive",
            "TestUnnamedBlockerActivationDoesNotReopenAWideWaitLive",
            "TestOldShapeWideWaitRowStillWakesAndAuditsLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_live_test.go": (
            "TestWaitingSetAsideParksWithBackstopLive",
            "TestWaitingBothOrdersEquivalenceLive",
            "TestWaitingAuditFindsMissedWakeLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_narrow_interleave_live_test.go": (
            "TestWaitParkDuringInFlightActivationIsWokenLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_narrow_live_test.go": (
            "TestNarrowedWaitSelectiveWakeLive",
            "TestNarrowedWaitAuditReopensSatisfiedLive",
            "TestNarrowedWaitConcurrentConvergenceLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_race_live_test.go": (
            "TestWaitingConcurrentClaimAndWakeLive",
            "TestWaitingDeferShareImprovementLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_rendezvous_contended_live_test.go": (
            "TestRendezvousContendedLatencyLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_rendezvous_live_test.go": (
            "TestParkWaitingOnOneScopeDoesNotStallAnotherScopesActivationLive",
            "TestParkWithManyBlockingScopesTakesBoundedLocksLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_wake_locality_live_test.go": (
            "TestProducerTerminalWakeForeignImpactAckLeavesReconciliationParkedLive",
            "TestProducerTerminalWakeCatalogRuntimeCrossScopeLive",
            "TestProducerTerminalWakeCatalogRuntimeSeededSameScopeLosesWakeLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_wake_locality_matrix_live_test.go": (
            "TestProducerTerminalWakeLocalityParkAfterAckRaceLive",
            "TestProducerTerminalWakeLocalityDuplicateAckLive",
            "TestProducerTerminalWakeLocalityAuditReopensMissedWakeLive",
            "TestProducerTerminalWakeLocalityMixedAckBatchLive",
            "TestProducerTerminalWakeLocalitySupersededGenerationLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_winners_wake_live_test.go": (
            "TestWinnersKeyedTickWakesParkedAdvisoryWaitLive",
            "TestWinnersAuditReopenedWaitConvergesOnNextTickLive",
        ),
        "go/internal/storage/postgres/relationship_activation_wake_live_test.go": (
            "TestReplayOfDeadLetteredProductionItemIsNotACollisionLive",
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
        "go/internal/storage/postgres/relationship_store_restricted_fence_live_test.go": (
            "TestRestrictedFenceBlockingScopesLive",
            "TestRestrictedFenceReadMatchesGlobalLive",
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
        "go/internal/storage/postgres/repository_retirements_census_live_test.go": (
            "TestRetirementPurgeCensus2eTablesLive",
            "TestRetirementCensusCascadeChildrenAreForeignKeyed",
            "TestRetirementConfigArtifactVersionRowIsRecreatedAtCommitLive",
        ),
        "go/internal/storage/postgres/repository_retirements_census_lock_live_test.go": (
            "TestRetirementPurgeRelationshipGenerationsLockOrderLive",
            "TestRetirementPurgeInfraAdvisoryLockOrderLive",
            "TestRetirementPurgeConfigArtifactAdvisoryLockOrderLive",
            "TestRetirementPurgeServiceMaterializationLockOrderLive",
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
        "go/internal/storage/postgres/repository_retirements_purge_lockbusy_live_test.go": (
            "TestRetirementPurgeDeriveLockBusyLive",
            "TestPurgeDeriveLockBudgetRestoresLockTimeoutLive",
        ),
        "go/internal/storage/postgres/repository_retirements_purge_requestbudget_live_test.go": (
            "TestRetirementPurgeRequestDeriveLockBudgetLive",
        ),
        "go/internal/storage/postgres/retirement_table_census_live_test.go": (
            "TestRetirementTableCensusClassifiesEveryScopeKeyedTable",
            "TestRetirementTableCensusSeededViolationProvesSensitivity",
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
        "go/internal/storage/postgres/run_watermark_guard_live_test.go": (
            "TestCICDRunWatermarkGuardAcrossGenerationsLive",
            "TestCICDRunWatermarkConcurrentGenerationsKeepTheNewestRunLive",
        ),
        "go/internal/storage/postgres/satisfied_by_read_delta_live_test.go": (
            "TestListActiveCrossplaneXRDFactsDeltaActiveScopeLive",
            "TestCrossplaneSatisfiedByDeltaKeepsUnchangedEdgesLive",
        ),
        "go/internal/storage/postgres/satisfied_by_read_index_live_test.go": (
            "TestCrossplaneXRDReadsUseThePartialIndexLive",
        ),
        "go/internal/storage/postgres/satisfied_by_redrive_base_leg_live_test.go": (
            "TestCrossplaneRedriveDiscoveryReshapeDifferentialLive",
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
        "go/internal/storage/postgres/supply_chain_impact_advisory_group_winners_live_test.go": (
            "TestAdvisoryGroupWinnersQualifyingLive",
        ),
        "go/internal/storage/postgres/supply_chain_impact_authorized_loader_engaged_live_test.go": (
            "TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive",
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
        "go/internal/storage/postgres/supply_chain_impact_fleet_gate_9875_live_test.go": (
            "TestFleetAcceptanceGateKEVReachesServiceRollup9875Live",
            "TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live",
        ),
        "go/internal/storage/postgres/supply_chain_impact_protected_static_publish_live_test.go": (
            "TestSupplyChainImpactProtectedStaticPublishLive",
            "TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive",
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
