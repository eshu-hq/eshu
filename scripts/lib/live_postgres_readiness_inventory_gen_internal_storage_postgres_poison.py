#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/poison (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/poison": {
        "go/internal/storage/postgres/poison/liveness_integration_live_test.go": (
            "TestPoisonLivenessIntegration",
        ),
        "go/internal/storage/postgres/poison/liveness_write_time_race_live_test.go": (
            "TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim",
        ),
    },
}
