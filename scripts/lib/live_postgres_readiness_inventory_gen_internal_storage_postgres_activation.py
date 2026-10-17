#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/activation (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/activation": {
        "go/internal/storage/postgres/activation/ack_live_test.go": (
            "TestActivationObligationAtomicAckLive",
        ),
        "go/internal/storage/postgres/activation/claim_live_test.go": (
            "TestActivationObligationClaimSkipsLockedRowsLive",
            "TestActivationObligationClaimIgnoresFinishedRowsLive",
            "TestActivationObligationFinalizeLeaseExpiryRollsBackLive",
        ),
        "go/internal/storage/postgres/activation/composed_live_test.go": (
            "TestActivationObligationConsumerAndEpochPassOverlapLive",
            "TestActivationObligationReplicasLive",
        ),
        "go/internal/storage/postgres/activation/consumer_live_test.go": (
            "TestActivationObligationConsumerOrderingLive",
            "TestActivationObligationConsumerLateFailureLive",
            "TestActivationObligationConsumerClaimRecoveryLive",
        ),
        "go/internal/storage/postgres/activation/lease_restart_live_test.go": (
            "TestActivationObligationLeaseExpiresMidMaintenanceLive",
            "TestActivationObligationRestartBeforePhasePublicationLive",
        ),
        "go/internal/storage/postgres/activation/matrix_live_test.go": (
            "TestActivationObligationConsumerQueueIsolationLive",
            "TestActivationObligationConsumerTerminalPreservedLive",
            "TestActivationObligationConsumerRollbackAndIdentityLive",
            "TestActivationObligationConsumerNoPhaseSubstitutionLive",
            "TestActivationObligationConsumerSupersessionLive",
            "TestActivationObligationWakeIsNotStarvedByOtherClassRowsLive",
        ),
        "go/internal/storage/postgres/activation/producer_obligation_contract_live_test.go": (
            "TestProducerActivationSettleIsExactlyOnceLive",
            "TestProducerActivationLeaseFencesStaleSettleLive",
            "TestProducerActivationUnrelatedScopeIsNotReopenedLive",
            "TestProducerActivationSettleMatchesEpochPassLive",
            "TestProducerActivationPruneAndStatsLive",
            "TestProducerActivationDriftReopenLive",
            "TestProducerActivationRetentionCascadeLive",
            "TestProducerActivationInsertConflictBranchesLive",
        ),
        "go/internal/storage/postgres/activation/producer_obligation_live_test.go": (
            "TestProducerActivationQuietAckLeavesConsumerUnreplayedLive",
        ),
        "go/internal/storage/postgres/activation/quiet_generation_live_test.go": (
            "TestQuietGenerationActivatesAfterMaintenanceSnapshotLive",
            "TestQuietGenerationActivatesWithControlArmMaintenanceLive",
        ),
        "go/internal/storage/postgres/activation/recovery_live_test.go": (
            "TestActivationObligationConsumerRestartAndDuplicatesLive",
            "TestActivationObligationConsumerWakeBatchCapLive",
            "TestActivationObligationCatchUpLive",
            "TestActivationObligationCatchUpReowesObsoleteOfActiveLive",
        ),
        "go/internal/storage/postgres/activation/redelivery_live_test.go": (
            "TestActivationObligationRedeliveryAndCatchUpRacesLive",
            "TestActivationObligationSupersessionBetweenClaimAndFinalizeLive",
        ),
        "go/internal/storage/postgres/activation/retention_live_test.go": (
            "TestActivationObligationRetentionCascadeLive",
            "TestActivationObligationPruneLive",
            "TestActivationObligationPruneIsNotStarvedByInapplicableRowsLive",
        ),
        "go/internal/storage/postgres/activation/scope_lock_live_test.go": (
            "TestActivationObligationIngestionCommitRacesFinalizeLive",
        ),
        "go/internal/storage/postgres/activation/targeted_live_test.go": (
            "TestActivationObligationRealCatalogChangeIsHeldThenCompletedLive",
            "TestActivationObligationRealCollisionLoserIsInapplicableLive",
            "TestActivationObligationBlockedMaintenanceIsCancelledBeforeTheLeaseLive",
        ),
        "go/internal/storage/postgres/activation/terminal_live_test.go": (
            "TestActivationObligationInapplicableWithoutRepositoryFactLive",
            "TestActivationObligationCatalogChangedIsHeldLive",
            "TestActivationObligationNullActivePointerIsObsoleteLive",
            "TestActivationObligationRetireInapplicableIsFencedLive",
            "TestActivationObligationInapplicableRetireIsLeaseFencedLive",
        ),
    },
}
