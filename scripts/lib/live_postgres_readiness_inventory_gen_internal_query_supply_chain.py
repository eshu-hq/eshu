#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/supply/chain (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/supply/chain": {
        "go/internal/query/supply/chain/runtime_filter_live_test.go": (
            "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        ),
        "go/internal/query/supply/chain/runtime_filter_plan_live_test.go": (
            "TestSupplyChainImpactRuntimeFilterPlansLive",
        ),
        "go/internal/query/supply/chain/runtime_filter_workload_id_shape_live_test.go": (
            "TestSupplyChainImpactWorkloadIDFilterRepositoryIDShapeLive",
        ),
        "go/internal/query/supply/chain/suppression_authority_live_test.go": (
            "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        ),
    },
}
