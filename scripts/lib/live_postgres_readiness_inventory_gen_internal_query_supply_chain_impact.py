#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/supply/chain/impact (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/supply/chain/impact": {
        "go/internal/query/supply/chain/impact/readiness_central_pin_live_test.go": (
            "TestReadinessExcludesCentralVersionPinsLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_container_identity_live_test.go": (
            "TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_orphan_package_keys_live_test.go": (
            "TestReadinessIgnoresOrphanPackageKeysLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_anchor_plan_live_test.go": (
            "TestSupplyChainImpactReadinessPackageAnchorLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_consumption_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageConsumptionScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_manifest_repo_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive",
            "TestSupplyChainImpactReadinessRepoArmScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_planner_skip_explain_live_test.go": (
            "TestSupplyChainImpactReadinessPlannerSkipQueryPlanLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scan_tier_explain_live_test.go": (
            "TestSupplyChainImpactReadinessScanTierQueryPlanLive",
            "TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive",
        ),
        "go/internal/query/supply/chain/impact/runtime_environment_store_live_test.go": (
            "TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive",
            "TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive",
            "TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive",
        ),
        "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go": (
            "TestSupplyChainSuppressionPathsPerformanceLive",
        ),
    },
}
