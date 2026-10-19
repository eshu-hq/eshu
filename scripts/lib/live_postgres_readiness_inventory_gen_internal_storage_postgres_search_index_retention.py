#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/search/index/retention (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/search/index/retention": {
        "go/internal/storage/postgres/search/index/retention/drain_live_test.go": (
            "TestDrainLiveRemovesOnlyTheSupersededGeneration",
            "TestDrainLiveLeavesNonTerminalGenerationsWhole",
            "TestDrainLiveWaitsForRunningSearchDocumentWork",
            "TestDrainLiveCancelledMidLoopConverges",
            "TestDrainLiveResweepRemovesARowInsertedBehindTheCursor",
            "TestDrainLiveSkipsAGenerationAnotherSessionHolds",
        ),
        "go/internal/storage/postgres/search/index/retention/foreign_key_drop_appliers_live_test.go": (
            "TestTermsForeignKeyDropLiveTwoAppliersBothSucceed",
            "TestTermsForeignKeyDropLiveApplierRetriesAmbientLockTimeout",
        ),
        "go/internal/storage/postgres/search/index/retention/foreign_key_drop_contention_live_test.go": (
            "TestTermsForeignKeyDropLiveBoundsConcurrentWriters",
        ),
        "go/internal/storage/postgres/search/index/retention/foreign_key_drop_live_test.go": (
            "TestTermsForeignKeyDropLiveLeavesNoGenerationForeignKey",
            "TestTermsForeignKeyDropLiveHandlesBothConstraintNames",
            "TestTermsForeignKeyDropLiveIsDeadlockFree",
        ),
        "go/internal/storage/postgres/search/index/retention/orphan_live_test.go": (
            "TestOrphanCensusLiveReadsZeroOnACleanTree",
            "TestOrphanSweepLiveRemovesOnlyMissingGenerations",
            "TestOrphanSweepLiveNeverDeletesAnExistingGeneration",
            "TestOrphanSweepLiveConcurrentSweepsDeleteOnce",
            "TestOrphanSweepLiveSweepsALateWriter",
            "TestOrphanLiveUnknownScopeWriteIsRejected",
        ),
        "go/internal/storage/postgres/search/index/retention/sweep_live_test.go": (
            "TestSweepSupersededLiveKeepsTheActiveReaderUnchanged",
            "TestSweepSupersededLiveSkipsPinnedAndInflight",
        ),
    },
}
