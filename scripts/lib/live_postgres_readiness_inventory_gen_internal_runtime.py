#!/usr/bin/env python3
"""Readiness enrollment for ./internal/runtime (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/runtime": {
        "go/internal/runtime/role_migration_live_test.go": (
            "TestInstalledRolePolicyAttestsRuntimePhysicalConnection",
        ),
    },
}
