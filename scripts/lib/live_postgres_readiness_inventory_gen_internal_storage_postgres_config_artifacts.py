#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/config/artifacts (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/config/artifacts": {
        "go/internal/storage/postgres/config/artifacts/derive_duplicate_live_test.go": (
            "TestDeriveDuplicateRowDigestFailsLoudlyLive",
            "TestDeriveRederiveReplacesRowsWithoutConflictLive",
        ),
        "go/internal/storage/postgres/config/artifacts/derive_lock_live_test.go": (
            "TestDeriveFailureRecordWaitsOnTheWriteLockLive",
            "TestDeriveFailureRecordRacingNewerWritesLive",
        ),
        "go/internal/storage/postgres/config/artifacts/derive_order_live_test.go": (
            "TestDeriveReadsVersionBeforeListLive",
            "TestDeriveHoldsTheLockOnlyInStepThreeLive",
            "TestDeriveOlderExtractorNeverReplacesNewerRowsLive",
            "TestDeriveRollbackLeavesRowsAndStateLive",
            "TestDeriveTimeoutAndPanicRecordFailureOnlyLive",
        ),
        "go/internal/storage/postgres/config/artifacts/derive_race_live_test.go": (
            "TestDeriveTwoDerivesOfOneRepositorySerializeLive",
            "TestDeriveRacingContentWritesNeverLabelsNewerContentLive",
            "TestDeriveTwoRepositoriesRunInParallelLive",
        ),
        "go/internal/storage/postgres/config/artifacts/derive_s4_live_test.go": (
            "TestDeriveStoredRowsEqualLivePathLive",
        ),
        "go/internal/storage/postgres/config/artifacts/dirty_live_test.go": (
            "TestListDirtyFindsTheWalkersWorkLive",
            "TestListDirtyAgreesWithClassifyLive",
        ),
        "go/internal/storage/postgres/config/artifacts/encoding_live_test.go": (
            "TestCheckEncodingReadsTheDatabaseEncodingLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_chunk_live_test.go": (
            "TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive",
            "TestVersionFenceTwoStatementTransactionRacingADeleteLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_churn_live_test.go": (
            "TestVersionFenceChurnIsOnePerTransactionLive",
            "TestVersionFenceChurnRejectsSeededPerRowFenceLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_commit_live_test.go": (
            "TestVersionFenceCommitTriggerIsDeferredLive",
            "TestVersionFenceBumpsOnceAtCommitLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_deadlock_live_test.go": (
            "TestVersionFenceOppositeOrderStatementsDoNotDeadlockLive",
            "TestVersionFenceArm7DetectsRowOrderFenceLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_interleave_live_test.go": (
            "TestVersionFenceR1WriteBatchesAfterDeriveReadLive",
            "TestVersionFenceTwoScopeWritesAfterDeriveReadLive",
            "TestVersionFencePruneAfterDeriveReadLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_live_test.go": (
            "TestCheckFenceFindsTheMigratedTriggersLive",
            "TestCheckFenceNamesEachDroppedTriggerLive",
            "TestCheckFenceIgnoresASameNamedTriggerOnAnotherTableLive",
            "TestCheckFenceTracksTriggerEnablementLive",
            "TestCheckFenceReportsAnUnmigratedDatabaseAsMissingLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_prune_live_test.go": (
            "TestVersionFenceDeletePlanSortsUnderModifyTableLive",
            "TestVersionFenceConcurrentPrunesShareRepositoriesLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_prune_write_live_test.go": (
            "TestVersionFencePruneAndWriteDisjointKeysLive",
            "TestVersionFencePruneAndWriteSharedDoomedKeyLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_rerun_live_test.go": (
            "TestVersionFenceMigrationReRunsUnderAConcurrentWriterLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_seed_live_test.go": (
            "TestVersionFenceMigrationsOnExistingContentLive",
            "TestVersionFenceSeedRacesAFencedWriteLive",
            "TestVersionFenceSeedOrderDoesNotDeadlockWithAWriterLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_seed_timing_live_test.go": (
            "TestVersionFenceSeedTimingLive",
        ),
        "go/internal/storage/postgres/config/artifacts/fence_trigger_live_test.go": (
            "TestVersionFenceBumpsOncePerRepositoryPerTransactionLive",
            "TestVersionFenceCreatesMissingRowOnFirstWriteLive",
            "TestVersionFenceConcurrentFirstWritesConvergeLive",
            "TestVersionFenceTriggersFireLastLive",
            "TestVersionFenceSurvivesATransactionIDRestartLive",
        ),
        "go/internal/storage/postgres/config/artifacts/schema_live_test.go": (
            "TestRowDigestMatchesGoRecomputationLive",
            "TestRowKeyRejectsADuplicateRowLive",
            "TestReadModelCatalogLive",
            "TestMigrationReRunsLive",
        ),
        "go/internal/storage/postgres/config/artifacts/walk_live_test.go": (
            "TestClaimPageWalksEveryRepositoryAndWrapsLive",
            "TestClaimPageAtAnExactPageBoundaryLive",
            "TestClaimPageGivesConcurrentClaimersDisjointPagesLive",
        ),
    },
}
