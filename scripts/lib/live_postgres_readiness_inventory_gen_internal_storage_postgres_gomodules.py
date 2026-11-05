#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/gomodules (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/gomodules": {
        "go/internal/storage/postgres/gomodules/fence_concurrency_live_test.go": (
            "TestSuccessorAckBeforeTheFenceMakesItANoOpLive",
            "TestFenceBeforeTheSuccessorAckEndsOnItLive",
            "TestFenceQueuesBehindAnAckHoldingTheScopeRowLive",
            "TestAckQueuesBehindAFenceHoldingAHolderLive",
            "TestFenceAndAckRaceEndOnTheSuccessorLive",
        ),
        "go/internal/storage/postgres/gomodules/fence_holders_live_test.go": (
            "TestFenceReopensExactlyTheHoldersOfItsRepositoryLive",
            "TestFenceRollsBackWhenAHolderReopenFailsLive",
            "TestFenceTimesOutBehindAHeldHolderAndRetriesLive",
            "TestFenceYieldsToTheRetirementFenceTableLockLive",
            "TestFiftySupersededGenerationsReadOnlyTheEvidenceCommitLive",
        ),
        "go/internal/storage/postgres/gomodules/fence_live_test.go": (
            "TestCommitSkewRuleOnThePageReadLive",
            "TestSkewInsideTwoRotationsIsNotFencedOnALargeFleetLive",
            "TestFencedRepositoryWithNoGoModDropsOutLive",
            "TestUnusableRemoteEvidenceRetractsAtTheBoundLive",
            "TestFenceLogsTheGenerationAndALaterTickConsumesItLive",
            "TestTerminalFailureReleasesTheNextPlanLive",
        ),
        "go/internal/storage/postgres/gomodules/store_live_test.go": (
            "TestListGoModuleRepositoriesRotatesEveryRepositoryLive",
            "TestListGoModuleRepositoriesSkipsInactiveScopesLive",
            "TestListGoModuleRepositoriesDetectsFromContentNotFactsLive",
            "TestGoModulePageAdmissionIsIdempotentLive",
            "TestListGoModuleRepositoriesPlansRepositoryThatLostItsGoModLive",
            "TestListGoModuleRepositoriesDropsRepositoryAfterRetractionLive",
        ),
    },
}
