#!/usr/bin/env python3
"""Readiness enrollment for ./cmd/ifa (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./cmd/ifa": {
        "go/cmd/ifa/enqueue_advisory_groups_supports_live_test.go": (
            "TestAdvisoryGroupCellAdmitsFindingsWithSupportsLive",
            "TestAdvisoryGroupCellFactsOnlyAdmissionMarksTheScopeDirtyLive",
        ),
    },
}
