#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/security/alerts/unread (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/security/alerts/unread": {
        "go/internal/storage/postgres/security/alerts/unread/commit_live_test.go": (
            "TestLiveCommitPersistsReadStateWithGeneration",
            "TestLiveReplayOfPendingGenerationReplacesUnreadRepositories",
            "TestLiveFailedStateWriteRollsBackTheGeneration",
            "TestLiveGenerationRetentionRemovesReadStateWithItsGeneration",
        ),
        "go/internal/storage/postgres/security/alerts/unread/concurrency_live_test.go": (
            "TestLiveRetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget",
            "TestLiveSecondClaimOfTheSameTargetKeepsItsOwnState",
            "TestLiveClaimsRacingOnOneGenerationIDLeaveOneConsistentState",
        ),
    },
}
