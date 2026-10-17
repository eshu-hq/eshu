#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/code/reachability (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/code/reachability": {
        "go/internal/storage/postgres/code/reachability/loader_completed_index_live_test.go": (
            "TestCodeReachabilityLoaderUsesCompletedIntentsIndex",
        ),
        "go/internal/storage/postgres/code/reachability/loader_edges_scope_live_test.go": (
            "TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly",
        ),
    },
}
