#!/usr/bin/env python3
"""Readiness enrollment for ./cmd/reducer (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./cmd/reducer": {
        "go/cmd/reducer/package_manifest_backfill_live_test.go": (
            "TestPackageManifestBackfillOnlyOneCandidateOwnsPass",
            "TestPackageManifestBackfillDoesNotStarveSingleConnectionPool",
        ),
    },
}
