#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/status/summary (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/status/summary": {
        "go/internal/storage/postgres/status/summary/bloat_live_test.go": (
            "TestStatusSummaryBloatLive",
            "TestStatusSummaryBloatIncompressibleLive",
        ),
        "go/internal/storage/postgres/status/summary/bloat_terraform_live_test.go": (
            "TestStatusSummaryBloatTerraformLive",
            "TestStatusSummaryBloatTerraformQAScaleLive",
            "TestStatusSummaryBloatTerraformWorstCaseLive",
        ),
        "go/internal/storage/postgres/status/summary/conflict_live_test.go": (
            "TestStatusSummaryConflictWaitLive",
        ),
        "go/internal/storage/postgres/status/summary/select_live_test.go": (
            "TestStatusSummarySelectLive",
        ),
        "go/internal/storage/postgres/status/summary/store_live_test.go": (
            "TestStatusSummaryMissingTableLive",
            "TestStatusSummaryMigrationLive",
            "TestStatusSummaryGuardLive",
            "TestStatusSummaryConcurrentWritersLive",
            "TestStatusSummaryCrashSafetyLive",
            "TestStatusSummaryWriterLockLive",
        ),
    },
}
