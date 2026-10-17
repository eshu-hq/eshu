#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/maintenance (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/maintenance": {
        "go/internal/storage/postgres/maintenance/repository_reindex_live_test.go": (
            "TestRepositoryReindexStoreWatermarksLive",
        ),
        "go/internal/storage/postgres/maintenance/requests_live_test.go": (
            "TestStatusRequestStoreRequestReindexWatermarkMonotonicLive",
        ),
    },
}
