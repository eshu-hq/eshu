#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/supply/chain/impact (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/supply/chain/impact": {
        "go/internal/query/supply/chain/impact/aggregates_winners_failclosed_live_test.go": (
            "TestAggregatesWinnersFailClosedLive",
            "TestAggregatesWinnersConcurrentResweepReadLive",
        ),
        "go/internal/query/supply/chain/impact/aggregates_winners_parity_live_test.go": (
            "TestAggregatesWinnersParityLive",
        ),
        "go/internal/query/supply/chain/impact/findings_winners_parity_live_test.go": (
            "TestFindingsWinnersPayloadParityLive",
            "TestFindingsWinnersPayloadBackfillServesWithoutResweepLive",
        ),
        "go/internal/query/supply/chain/impact/grouped_inventory_postgres_live_test.go": (
            "TestGroupedInventoryStoreWinnersLive",
        ),
        "go/internal/query/supply/chain/impact/multi_support_parity_live_test.go": (
            "TestMultiSupportWinnersRecordsEverySupport",
            "TestMultiSupportScopedGrantDivergenceLive",
            "TestMultiSupportUnscopedRankFilterDivergenceLive",
            "TestMultiSupportProfileDivergenceLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_central_pin_live_test.go": (
            "TestReadinessExcludesCentralVersionPinsLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_container_identity_live_test.go": (
            "TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_fetch_recency_live_test.go": (
            "TestReadinessAdvisoryFreshAfterSkippedUnchangedFetchLive",
            "TestReadinessAdvisoryStaleWhenNeverRefetchedLive",
            "TestReadinessKeepsAdvisorySourcesForARefusedShrinkFromTheExtractedCollectorLive",
            "TestReadinessGlobalLayoutWithoutLocalClaimColumnsUsesLegacyLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_govulncheck_source_live_test.go": (
            "TestReadinessReportsGovulncheckSourceStateLive",
            "TestReadinessScopesGovulncheckWarningToItsModuleLive",
            "TestReadinessKeepsStoredGovulncheckSourceStateLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_nvd_window_explain_live_test.go": (
            "TestSupplyChainImpactReadinessNVDWindowQueryPlanLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_orphan_package_keys_live_test.go": (
            "TestReadinessIgnoresOrphanPackageKeysLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_anchor_plan_live_test.go": (
            "TestSupplyChainImpactReadinessPackageAnchorLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_consumption_scope_live_test.go": (
            "TestReadinessPackageNameFoldMatchesLowerASCIILive",
            "TestSupplyChainImpactReadinessPackageConsumptionScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_package_manifest_repo_scope_live_test.go": (
            "TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive",
            "TestSupplyChainImpactReadinessRepoArmScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_planner_skip_explain_live_test.go": (
            "TestSupplyChainImpactReadinessPlannerSkipQueryPlanLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_release_scope_explain_live_test.go": (
            "TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive",
            "TestSupplyChainImpactReadinessFleetProbePlanLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scan_tier_explain_live_test.go": (
            "TestSupplyChainImpactReadinessScanTierQueryPlanLive",
            "TestSupplyChainImpactReadinessScanTierOSPackageCountDoesNotFanOutLive",
            "TestSupplyChainImpactReadinessScanTierSupersededGenerationLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scan_tier_rescan_live_test.go": (
            "TestSupplyChainImpactReadinessScanTierRescannedScopeLive",
        ),
        "go/internal/query/supply/chain/impact/readiness_scanner_authorization_live_test.go": (
            "TestReadinessScannerSourceBindingAuthorizationLive",
        ),
        "go/internal/query/supply/chain/impact/runtime_environment_store_live_test.go": (
            "TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive",
            "TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive",
            "TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive",
        ),
        "go/internal/query/supply/chain/impact/supports_equality_live_test.go": (
            "TestSupportsCandidateListEqualsLegacyLive",
        ),
        "go/internal/query/supply/chain/impact/supports_plan_shape_live_test.go": (
            "TestSupportsCandidatePlanDrivesFromTheGrantLive",
        ),
        "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go": (
            "TestSupplyChainSuppressionPathsPerformanceLive",
        ),
    },
}
