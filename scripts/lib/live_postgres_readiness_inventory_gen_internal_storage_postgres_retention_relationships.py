#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/retention/relationships (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/retention/relationships": {
        "go/internal/storage/postgres/retention/relationships/classify_race_live_test.go": (
            "TestActivationCommittedBetweenSnapshotAndRowLockIsNotClaimedLive",
            "TestPassOverAnActivationRaceNeverCountsTheGenerationSweptLive",
        ),
        "go/internal/storage/postgres/retention/relationships/eligibility_live_test.go": (
            "TestEligibilityMatrixLive",
            "TestSweepFaultBetweenStatementsLeavesTheGenerationWholeLive",
            "TestChildrenWithoutAGenerationRowAreRelistedLive",
        ),
        "go/internal/storage/postgres/retention/relationships/guard_live_test.go": (
            "TestDeleteStatementsGuardThemselvesLive",
            "TestSweepStopsAtTheRowCapLive",
        ),
        "go/internal/storage/postgres/retention/relationships/held_rows_live_test.go": (
            "TestRowsHeldOnAGenerationWithoutAGenerationRowAreNotCountedSweptLive",
        ),
        "go/internal/storage/postgres/retention/relationships/large_generation_live_test.go": (
            "TestGenerationLargerThanTheRowLimitIsDrainedInBoundedBatchesLive",
            "TestLargeGenerationWithSomeRowsHeldEndsBusyOnceLive",
        ),
        "go/internal/storage/postgres/retention/relationships/lock_timeout_live_test.go": (
            "TestRelationLockHeldByAnotherSessionEndsTheBatchOnTheLockTimeoutLive",
        ),
        "go/internal/storage/postgres/retention/relationships/plan_live_test.go": (
            "TestRelationshipRetentionStatementsAreIndexServedLive",
        ),
        "go/internal/storage/postgres/retention/relationships/prune_claimed_live_test.go": (
            "TestClaimedWorkKeepsTheGenerationOutOfPruneAndSweepLive",
        ),
        "go/internal/storage/postgres/retention/relationships/purge_arrival_live_test.go": (
            "TestPurgeArrivingWhileTheBatchHoldsTheGenerationRowLive",
            "TestPurgeArrivingWhileTheBatchHoldsChildRowsLive",
            "TestSweepSweepsAGenerationAfterTheHoldingPurgeRollsBackLive",
        ),
        "go/internal/storage/postgres/retention/relationships/purge_interplay_live_test.go": (
            "TestSweepYieldsToAPurgeInFlightLive",
        ),
        "go/internal/storage/postgres/retention/relationships/served_contract_live_test.go": (
            "TestSweptResolvedIDAnswersTheExistingNotFoundLive",
        ),
        "go/internal/storage/postgres/retention/relationships/state_change_live_test.go": (
            "TestStateChangeAfterTheCensusIsIneligibleLive",
            "TestActivationBlocksBehindTheBatchRowLockLive",
            "TestActivationRaceKeepsEveryOutcomeConsistentLive",
            "TestFencedActivationCannotReviveAnOrphanIDLive",
        ),
    },
}
