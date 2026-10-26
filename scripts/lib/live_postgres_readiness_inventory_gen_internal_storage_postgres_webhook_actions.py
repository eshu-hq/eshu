#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/webhook/actions (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/webhook/actions": {
        "go/internal/storage/postgres/webhook/actions/contention_live_test.go": (
            "TestActionsClaimSkipsHeldRowLocksLive",
        ),
        "go/internal/storage/postgres/webhook/actions/store_live_test.go": (
            "TestActionsDeliveryLeaseAndIdentityLive",
            "TestPendingRepositoryIncludesDeferredDeliveryLive",
            "TestActionsDeliveryMigrationMatchesBootstrapSchema",
            "TestActionsDeliveryConcurrentClaimsAreDisjointLive",
            "TestActionsClaimPersistsBoundedRepositoryBatchLive",
            "TestActionsDeliveryBlockedBudgetAndFailedRetryCapLive",
        ),
    },
}
