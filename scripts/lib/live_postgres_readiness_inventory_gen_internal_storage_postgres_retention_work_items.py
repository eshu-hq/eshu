#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/retention/work/items (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/retention/work/items": {
        "go/internal/storage/postgres/retention/work/items/concurrency_live_test.go": (
            "TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle",
            "TestPruneWorkItemsLiveSkipsLockedRowsWithoutWaiting",
            "TestPruneWorkItemsLiveOpenBatchDoesNotBlockLifecycle",
        ),
        "go/internal/storage/postgres/retention/work/items/sweeper_live_test.go": (
            "TestPruneWorkItemsLiveKeepsRowsNeededForFailureReporting",
            "TestPruneWorkItemsLiveKeepsRegistryLastCompletedAt",
            "TestPruneWorkItemsLiveLeavesLiveAndYoungRowsAlone",
            "TestPruneWorkItemsLiveRespectsBatchBoundAndIsIdempotent",
        ),
    },
}
