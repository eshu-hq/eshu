#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/admin/store (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/admin/store": {
        "go/internal/query/admin/store/reopen_stamp_intent_age_live_test.go": (
            "TestSharedIntentReopenAgeThroughStatusLive",
            "TestSharedIntentReopenRacesLive",
        ),
        "go/internal/query/admin/store/reopen_stamp_live_test.go": (
            "TestAdminReopenWritersStampReopenedAtLive",
        ),
    },
}
