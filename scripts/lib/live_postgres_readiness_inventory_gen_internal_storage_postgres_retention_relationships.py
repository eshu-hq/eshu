#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/retention/relationships (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/retention/relationships": {
        "go/internal/storage/postgres/retention/relationships/eligibility_live_test.go": (
            "TestEligibilityMatrixLive",
            "TestSweepFaultBetweenStatementsLeavesTheGenerationWholeLive",
            "TestChildrenWithoutAGenerationRowAreRelistedLive",
        ),
        "go/internal/storage/postgres/retention/relationships/guard_live_test.go": (
            "TestDeleteStatementsGuardThemselvesLive",
            "TestSweepStopsAtTheRowCapLive",
        ),
        "go/internal/storage/postgres/retention/relationships/prune_claimed_live_test.go": (
            "TestClaimedWorkKeepsTheGenerationOutOfPruneAndSweepLive",
        ),
        "go/internal/storage/postgres/retention/relationships/purge_interplay_live_test.go": (
            "TestSweepYieldsToAPurgeInFlightLive",
            "TestPurgeArrivingMidBatchDoesNotDeadlockLive",
        ),
        "go/internal/storage/postgres/retention/relationships/state_change_live_test.go": (
            "TestStateChangeAfterTheCensusIsIneligibleLive",
            "TestActivationBlocksBehindTheBatchRowLockLive",
            "TestActivationRaceKeepsEveryOutcomeConsistentLive",
            "TestFencedActivationCannotReviveAnOrphanIDLive",
        ),
    },
}
