#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/backfill/targeted (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/backfill/targeted": {
        "go/internal/storage/postgres/backfill/targeted/closure_theory_live_test.go": (
            "TestTheoryExactPartitionEvidenceClosure",
        ),
        "go/internal/storage/postgres/backfill/targeted/differential_live_test.go": (
            "TestTargetedMaintenanceMatchesWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/interleave_live_test.go": (
            "TestTargetedMaintenanceInterleavingsMatchWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/outcomes_live_test.go": (
            "TestTargetedMaintenanceOutcomesMatchWholePass",
        ),
        "go/internal/storage/postgres/backfill/targeted/sql_live_test.go": (
            "TestBoundedRepositoryGenerationReadsMatchShippedRead",
        ),
        "go/internal/storage/postgres/backfill/targeted/terminal_live_test.go": (
            "TestActivationObligationInapplicableCollisionLoserLive",
        ),
    },
}
