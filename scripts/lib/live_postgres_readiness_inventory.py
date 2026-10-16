#!/usr/bin/env python3
"""Authoritative package and test inventory for live PostgreSQL readiness.

This module assembles PACKAGES (data only) from the per-domain tables in
live_postgres_readiness_inventory_{storage,queue,query,surfaces}.py plus the
status-summary table defined here. The package-path constants live in
live_postgres_readiness_inventory_names.py and are re-exported so callers keep
importing them from here. The derived views (EXPECTED, TOTAL_TESTS) and the
checks over it live in live_postgres_readiness_records.py, which is what
callers import, so the table can grow without any one file passing the
500-line cap.
"""

from live_postgres_readiness_inventory_names import (
    IMPACT_PACKAGE,
    STORAGE_PACKAGE,
    TARGETED_PACKAGE,
    ACTIVATION_PACKAGE,
    MAINTENANCE_PACKAGE,
    MEMBERSHIP_PACKAGE,
    REDUCER_PACKAGE,
    QUERY_PACKAGE,
    SUMMARY_PACKAGE,
    WRITER_PACKAGE,
    ADMIN_PACKAGE,
    REACHABILITY_PACKAGE,
    CONFIG_ARTIFACTS_PACKAGE,
    UNSCOPED_SEARCH_PACKAGE,
    WEBHOOK_PACKAGE,
)
from live_postgres_readiness_inventory_query import QUERY_PACKAGES
from live_postgres_readiness_inventory_queue import QUEUE_PACKAGES
from live_postgres_readiness_inventory_storage import STORAGE_PACKAGES
from live_postgres_readiness_inventory_surfaces import SURFACE_PACKAGES

SUMMARY_PACKAGES = {
    SUMMARY_PACKAGE: {
        "go/internal/storage/postgres/status/summary/store_live_test.go": (
            "TestStatusSummaryMissingTableLive",
            "TestStatusSummaryMigrationLive",
            "TestStatusSummaryGuardLive",
            "TestStatusSummaryConcurrentWritersLive",
            "TestStatusSummaryCrashSafetyLive",
            "TestStatusSummaryWriterLockLive",
        ),
        "go/internal/storage/postgres/status/summary/conflict_live_test.go": (
            "TestStatusSummaryConflictWaitLive",
        ),
        "go/internal/storage/postgres/status/summary/bloat_live_test.go": (
            "TestStatusSummaryBloatLive",
            "TestStatusSummaryBloatIncompressibleLive",
        ),
        "go/internal/storage/postgres/status/summary/bloat_terraform_live_test.go": (
            "TestStatusSummaryBloatTerraformLive",
            "TestStatusSummaryBloatTerraformOpsQaScaleLive",
            "TestStatusSummaryBloatTerraformWorstCaseLive",
        ),
        "go/internal/storage/postgres/status/summary/select_live_test.go": (
            "TestStatusSummarySelectLive",
        ),
    },
}

# Expected files and tests per Go package (relative to the go/ module root).
# The runner invokes one go test per package, each with its own events file
# and package terminal event. The dict order is the runner's package order.
PACKAGES = {**STORAGE_PACKAGES, **QUEUE_PACKAGES, **QUERY_PACKAGES, **SUMMARY_PACKAGES, **SURFACE_PACKAGES}
