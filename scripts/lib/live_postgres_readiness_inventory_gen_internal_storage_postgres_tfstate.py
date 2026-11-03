#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/tfstate (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/tfstate": {
        "go/internal/storage/postgres/tfstate/canonical_backends_8875_differential_live_test.go": (
            "TestTF8875CanonicalQueryDerivesFromShippedConstant",
            "TestTF8875CanonicalRepoLocalPathsMaterialized",
            "TestTF8875CanonicalBackendsIndexExists",
            "TestTF8875CanonicalRowDifferentialLive",
        ),
    },
}
