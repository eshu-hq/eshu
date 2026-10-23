#!/usr/bin/env python3
"""Readiness enrollment for ./internal/reducer/freshness/links (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/reducer/freshness/links": {
        "go/internal/reducer/freshness/links/lock_timeout_live_test.go": (
            "TestLockTimeoutsNeverCount",
        ),
        "go/internal/reducer/freshness/links/process_live_test.go": (
            "TestTwoProcessRunnersLinkEachActivationOnce",
        ),
        "go/internal/reducer/freshness/links/retention_process_live_test.go": (
            "TestLinkAndRetentionProcessesRace",
        ),
        "go/internal/reducer/freshness/links/runner_live_test.go": (
            "TestRunnerPoisonsAFailingLinkAfterMaxAttempts",
            "TestNonCountingOutcomesNeverPoison",
            "TestRunnerMovesOnPastABusySlot",
        ),
    },
}
