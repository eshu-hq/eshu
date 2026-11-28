#!/usr/bin/env python3
"""Readiness enrollment for ./cmd/read-api-latency-gate (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./cmd/read-api-latency-gate": {
        "go/cmd/read-api-latency-gate/seed_supports_backfill_live_test.go": (
            "TestBackfillSeededSupportsReadModelDrivesSeededSupportsToReadyLive",
        ),
    },
}
