#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/admin (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/admin": {
        "go/internal/query/admin/activation_wake_interleave_live_test.go": (
            "TestActivationWakeInterleaveLive",
            "TestAdminReplayAfterCommittedWakeIsANoOpLive",
        ),
        "go/internal/query/admin/reopen_live_test.go": (
            "TestAdminHandler_ReopenLive",
        ),
        "go/internal/query/admin/reopen_rollover_live_test.go": (
            "TestAdminHandler_ReopenRolloverActsOnCurrentGenerationLive",
        ),
        "go/internal/query/admin/replay_claim_sweep_lock_order_live_test.go": (
            "TestAdminReplayAndClaimSweepBlockOneWayLive",
            "TestAdminReplayAndClaimSweepHeldRowRaceLive",
            "TestReplaysAndClaimSweepConcurrentNeverDeadlockLive",
        ),
        "go/internal/query/admin/replay_lock_order_live_test.go": (
            "TestAdminReplayLockOrderBlocksOneWayLive",
            "TestAdminReplayHeldRowRaceLive",
            "TestAdminReplayDropsRowClaimedBeforeItsLockLive",
        ),
        "go/internal/query/admin/scope_selector_live_test.go": (
            "TestAdminHandler_ScopeSelectorCollisionLive",
        ),
        "go/internal/query/admin/skip_lock_order_live_test.go": (
            "TestSkipRepositoryHeldRowRaceLive",
            "TestSkipRepositoryDropsRowChangedBeforeItsLockLive",
            "TestSkipRepositoryConcurrentWithReplaysAndSweepNeverDeadlockLive",
        ),
    },
}
