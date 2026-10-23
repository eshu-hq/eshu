#!/usr/bin/env python3
"""Readiness enrollment for ./cmd/bootstrap-index (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./cmd/bootstrap-index": {
        "go/cmd/bootstrap-index/bootstrap_projector_base_missing_live_test.go": (
            "TestDrainProjectorBaseMissingSupersedesAndRequestsReindexLive",
        ),
    },
}
