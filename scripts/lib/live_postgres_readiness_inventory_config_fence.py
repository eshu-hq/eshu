#!/usr/bin/env python3
"""Config-artifact version-fence live PostgreSQL readiness proofs (#8155).

live_postgres_readiness_inventory.py is over the 500-line cap and must not
grow, so these files live here and live_postgres_readiness_records.py merges
them into PACKAGES, as it does the own-gate sibling. A package missing from
PACKAGES gets nothing merged, so an emptied PACKAGES stays empty and still
fails closed.
"""

from live_postgres_readiness_inventory import CONFIG_ARTIFACTS_PACKAGE

_FENCE_DIR = "go/internal/storage/postgres/config/artifacts"

CONFIG_FENCE_PACKAGES = {
    CONFIG_ARTIFACTS_PACKAGE: {
        f"{_FENCE_DIR}/fence_trigger_live_test.go": (
            "TestVersionFenceBumpsOncePerRepositoryPerTransactionLive",
            "TestVersionFenceCreatesMissingRowOnFirstWriteLive",
            "TestVersionFenceConcurrentFirstWritesConvergeLive",
            "TestVersionFenceTriggersFireLastLive",
            "TestVersionFenceSurvivesATransactionIDRestartLive",
        ),
        f"{_FENCE_DIR}/fence_churn_live_test.go": (
            "TestVersionFenceChurnIsOnePerTransactionLive",
            "TestVersionFenceChurnRejectsSeededPerRowFenceLive",
        ),
        f"{_FENCE_DIR}/fence_deadlock_live_test.go": (
            "TestVersionFenceOppositeOrderStatementsDoNotDeadlockLive",
            "TestVersionFenceArm7DetectsRowOrderFenceLive",
        ),
        f"{_FENCE_DIR}/fence_prune_live_test.go": (
            "TestVersionFenceDeletePlanSortsUnderModifyTableLive",
            "TestVersionFenceConcurrentPrunesShareRepositoriesLive",
        ),
        f"{_FENCE_DIR}/fence_interleave_live_test.go": (
            "TestVersionFenceR1WriteBatchesAfterDeriveReadLive",
            "TestVersionFenceTwoScopeWritesAfterDeriveReadLive",
            "TestVersionFencePruneAfterDeriveReadLive",
        ),
        f"{_FENCE_DIR}/fence_prune_write_live_test.go": (
            "TestVersionFencePruneAndWriteDisjointKeysLive",
            "TestVersionFencePruneAndWriteSharedDoomedKeyLive",
        ),
        f"{_FENCE_DIR}/fence_chunk_live_test.go": (
            "TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive",
            "TestVersionFenceTwoStatementTransactionRacingADeleteLive",
        ),
        f"{_FENCE_DIR}/fence_seed_live_test.go": (
            "TestVersionFenceMigrationsOnExistingContentLive",
            "TestVersionFenceSeedRacesAFencedWriteLive",
            "TestVersionFenceSeedOrderDoesNotDeadlockWithAWriterLive",
        ),
        f"{_FENCE_DIR}/fence_rerun_live_test.go": (
            "TestVersionFenceMigrationReRunsUnderAConcurrentWriterLive",
        ),
        f"{_FENCE_DIR}/fence_commit_live_test.go": (
            "TestVersionFenceCommitTriggerIsDeferredLive",
            "TestVersionFenceBumpsOnceAtCommitLive",
        ),
        f"{_FENCE_DIR}/fence_seed_timing_live_test.go": (
            "TestVersionFenceSeedTimingLive",
        ),
        f"{_FENCE_DIR}/fence_live_test.go": (
            "TestCheckFenceFindsTheMigratedTriggersLive",
            "TestCheckFenceNamesEachDroppedTriggerLive",
            "TestCheckFenceIgnoresASameNamedTriggerOnAnotherTableLive",
            "TestCheckFenceTracksTriggerEnablementLive",
            "TestCheckFenceReportsAnUnmigratedDatabaseAsMissingLive",
        ),
    },
}
