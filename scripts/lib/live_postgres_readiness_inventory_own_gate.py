#!/usr/bin/env python3
"""Own-gate (#7597) live PostgreSQL readiness proofs, kept beside the inventory.

These files live in their own table beside the per-domain inventory tables
(live_postgres_readiness_inventory_{storage,queue,query,surfaces}.py), and
live_postgres_readiness_records.py merges them into PACKAGES. A package
missing from PACKAGES gets nothing merged, so an emptied PACKAGES stays empty
and still fails closed.
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
    },
    ADMIN_PACKAGE: {
        "go/internal/query/admin/activation_wake_interleave_live_test.go": (
            "TestActivationWakeInterleaveLive",
        ),
    },
}
