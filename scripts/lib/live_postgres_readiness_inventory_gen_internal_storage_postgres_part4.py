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
            "TestClaimBatchGatePriorityBoostsOlderBaselineWindowLive",
            "TestClaimBatchGatePriorityIgnoresCompletedYearLive",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_gate_undrained_set_live_test.go": (
            "TestClaimGateAndCheckpointNameTheSameUndrainedSetLive",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_rank_once_diff_live_test.go": (
            "TestClaimBatchRankOnceRewriteMatchesPreRewriteCandidateSetAndOrder",
        ),
        "go/internal/storage/postgres/reducer_queue_batch_wide_fail_live_test.go": (
            "TestFailBatchWideBlockingSetParksCleanlyLive",
            "TestFailBatchScaledAllWideParksCleanlyLive",
        ),
        "go/internal/storage/postgres/reducer_queue_code_call_delta_scope_live_test.go": (
            "TestCodeCallDeltaScopeUnusableDeadLettersOnFirstAttemptLive",
        ),
        "go/internal/storage/postgres/reducer_queue_domain_fairness_live_test.go": (
            "TestClaimBatchDoesNotStarveNewerDomainsBehindOlderBacklog",
            "TestClaimBatchFairnessRankExcludesNonClaimableRows",
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
        "go/internal/storage/postgres/reducer_queue_supports_not_ready_live_test.go": (
            "TestSupplyChainImpactSupportsNotReadyDefersNonCountingLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_ack_contention_live_test.go": (
            "TestWaitingAckParkContentionLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_ack_park_race_live_test.go": (
            "TestWaitingAckParkInterleaveLive",
            "TestWaitingAckParkOrdersLive",
            "TestWaitingAckCrashResidualAuditLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_audit_resolved_live_test.go": (
            "TestWaitingAuditResolvedCounterLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_backstop_claim_live_test.go": (
            "TestWaitingBackstopClaimCounterLive",
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
            "TestOldShapeWideWaitRowStillWakesAndAuditsLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_live_test.go": (
            "TestWaitingSetAsideParksWithBackstopLive",
            "TestWaitingBothOrdersEquivalenceLive",
            "TestWaitingAuditFindsMissedWakeLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_narrow_bound_live_test.go": (
            "TestNarrowedWaitLargeFenceParksThroughFailLive",
            "TestNarrowedWaitWorstCaseKeyFitsLive",
            "TestNarrowedWaitPrefixWakeSelectivityLive",
            "TestNarrowedWaitAdversarialOrderConvergesLive",
            "TestNarrowedWaitAuditReopensClearedPrefixLive",
        ),
        "go/internal/storage/postgres/reducer_waiting_narrow_cutkey_race_live_test.go": (
            "TestNarrowedWaitCutKeyRacesActivationLive",
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
    },
}
