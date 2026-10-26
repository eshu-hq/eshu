#!/usr/bin/env python3
"""Readiness enrollment for ./internal/coordinator (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/coordinator": {
        "go/internal/coordinator/service_cicd_run_burst_live_test.go": (
            "TestCICDServiceClaimGroupBurstAndLateArrivalLive",
            "TestCICDServiceConcurrentCoordinatorsShareClaimGroupLive",
            "TestCICDServiceClaimGroupPartialACKCrashRecoveryLive",
            "TestCICDServiceGroupRecoversPartiallyAdmittedScopesLive",
            "TestCICDServiceGroupExactRecoveryRejectsOtherTenantLive",
        ),
    },
}
