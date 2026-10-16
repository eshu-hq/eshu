#!/usr/bin/env python3
"""Own-gate (#7597) and replay lock-order (#8205) live PostgreSQL readiness proofs.

Both families pin the one reducer work-row lock order (work_item_id
COLLATE "C"), so they are kept together beside the inventory.

live_postgres_readiness_inventory.py is over the 500-line cap and must not
grow, so these files live here and live_postgres_readiness_records.py merges
them into PACKAGES. A package missing from PACKAGES gets nothing merged, so
an emptied PACKAGES stays empty and still fails closed.
"""

from live_postgres_readiness_inventory import ADMIN_PACKAGE, STORAGE_PACKAGE

OWN_GATE_PACKAGES = {
    STORAGE_PACKAGE: {
        "go/internal/storage/postgres/reducer_queue_producer_terminal_live_test.go": (
            "TestOwnGateProducerDeadLetteredConsumerDeadLettersLive",
            "TestOwnGateProducerRetryingConsumerKeepsDeferringLive",
        ),
        "go/internal/storage/postgres/relationship_activation_wake_live_test.go": (
            "TestActivationWakesOwnGateConsumersLive",
        ),
        "go/internal/storage/postgres/relationship_activation_wake_race_live_test.go": (
            "TestActivationWaitsForAnInflightFinalFailLive",
            "TestFinalFailAfterTheActivationLockIsWokenLive",
            "TestActivationLockTimeoutFailsRetryablyLive",
        ),
        "go/internal/storage/postgres/reducer_queue_producer_refinalize_live_test.go": (
            "TestOwnGateWaitsThroughRefinalizeReResolutionLive",
        ),
        "go/internal/storage/postgres/recovery_replay_lock_order_live_test.go": (
            "TestRecoveryReplayConcurrentReplaysNeverDeadlockLive",
        ),
        "go/internal/storage/postgres/recovery_replay_lock_order_interleave_live_test.go": (
            "TestRecoveryReplayTwoSessionsBlockOneWayLive",
            "TestRecoveryReplayHeldRowRaceLive",
            "TestRecoveryReplayDropsRowChangedBeforeItsLockLive",
        ),
    },
    ADMIN_PACKAGE: {
        "go/internal/query/admin/activation_wake_interleave_live_test.go": (
            "TestActivationWakeInterleaveLive",
        ),
        "go/internal/query/admin/replay_lock_order_live_test.go": (
            "TestAdminReplayLockOrderBlocksOneWayLive",
            "TestAdminReplayHeldRowRaceLive",
            "TestAdminReplayDropsRowClaimedBeforeItsLockLive",
        ),
    },
}
