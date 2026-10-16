#!/usr/bin/env python3
"""Expected live proofs for the backfill, webhook, activation, maintenance, membership and reducer packages (data only)."""

from live_postgres_readiness_inventory_names import (
    ACTIVATION_PACKAGE,
    MAINTENANCE_PACKAGE,
    MEMBERSHIP_PACKAGE,
    REDUCER_PACKAGE,
    TARGETED_PACKAGE,
    WEBHOOK_PACKAGE,
)

QUEUE_PACKAGES = {
    TARGETED_PACKAGE: {
        "go/internal/storage/postgres/backfill/targeted/terminal_live_test.go": (
            "TestActivationObligationInapplicableCollisionLoserLive",
        ),
        "go/internal/storage/postgres/backfill/targeted/differential_live_test.go": (
            "TestTargetedMaintenanceMatchesWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/interleave_live_test.go": (
            "TestTargetedMaintenanceInterleavingsMatchWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/outcomes_live_test.go": (
            "TestTargetedMaintenanceOutcomesMatchWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/sql_live_test.go": (
            "TestBoundedRepositoryGenerationReadsMatchShippedRead",
        ),
        "go/internal/storage/postgres/backfill/targeted/closure_theory_live_test.go": (
            "TestTheoryExactPartitionEvidenceClosure",
        ),
    },
    WEBHOOK_PACKAGE: {
        "go/internal/storage/postgres/webhook/ignored_decision_live_test.go": (
            "TestWebhookTriggerStoreUpsertDemotesQueuedToIgnored",
            "TestWebhookTriggerStoreReapFailsIgnoredDecisionClaims",
            "TestWebhookTriggerStoreIgnoredDecisionFailIsDecisionGuarded",
        ),
    },
    ACTIVATION_PACKAGE: {
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
        "go/internal/storage/postgres/activation/quiet_generation_live_test.go": (
            "TestQuietGenerationActivatesAfterMaintenanceSnapshotLive",
            "TestQuietGenerationActivatesWithControlArmMaintenanceLive",
        ),
        "go/internal/storage/postgres/activation/producer_obligation_live_test.go": (
            "TestProducerActivationQuietAckLeavesConsumerUnreplayedLive",
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
    },
    MAINTENANCE_PACKAGE: {
        "go/internal/storage/postgres/maintenance/requests_live_test.go": (
            "TestStatusRequestStoreRequestReindexWatermarkMonotonicLive",
        ),
        "go/internal/storage/postgres/maintenance/repository_reindex_live_test.go": (
            "TestRepositoryReindexStoreWatermarksLive",
        ),
    },
    MEMBERSHIP_PACKAGE: {
        "go/internal/storage/postgres/membership/known_scopes_live_test.go": (
            "TestKnownScopesHostFilterLive",
        ),
        "go/internal/storage/postgres/membership/known_scopes_prefix_live_test.go": (
            "TestKnownScopesPathPrefixLive",
        ),
        "go/internal/storage/postgres/membership/live_read_live_test.go": (
            "TestLiveFilterParityLive",
        ),
        "go/internal/storage/postgres/membership/observations_live_test.go": (
            "TestObservationStoreLive",
        ),
        "go/internal/storage/postgres/membership/observations_sweep_live_test.go": (
            "TestObservationStoreSweepLive",
        ),
    },
    REDUCER_PACKAGE: {
        "go/cmd/reducer/package_manifest_backfill_live_test.go": (
            "TestPackageManifestBackfillOnlyOneCandidateOwnsPass",
            "TestPackageManifestBackfillDoesNotStarveSingleConnectionPool",
        ),
    },
}
