#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/membership (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/membership": {
        "go/internal/storage/postgres/membership/known_scopes_live_test.go": (
            "TestKnownScopesHostFilterLive",
        ),
        "go/internal/storage/postgres/membership/known_scopes_prefix_live_test.go": (
            "TestKnownScopesPathPrefixLive",
        ),
        "go/internal/storage/postgres/membership/live_read_live_test.go": (
            "TestLiveFilterParityLive",
        ),
        "go/internal/storage/postgres/membership/observations_live_test.go": (
            "TestObservationStoreLive",
        ),
        "go/internal/storage/postgres/membership/observations_sweep_live_test.go": (
            "TestObservationStoreSweepLive",
        ),
    },
}
