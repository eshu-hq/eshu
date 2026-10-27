#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query": {
        "go/internal/query/cloud_resource_legacy_keys_postgres_live_test.go": (
            "TestCloudResourceListSuppressesHistoricalAppSyncKeysLive",
        ),
        "go/internal/query/code_topic_entity_name_first_live_test.go": (
            "TestInvestigateCodeTopicEntityNameFirstPostgresLive",
        ),
        "go/internal/query/code_topic_fleet_endpoint_live_test.go": (
            "TestCodeTopicFleetCapacityEndpointPostgresLive",
        ),
        "go/internal/query/code_topic_snapshot_export_endpoint_live_test.go": (
            "TestCodeTopicSnapshotExportQualificationEndpointPostgresLive",
        ),
        "go/internal/query/content_reader_dead_code_incoming_bound_live_test.go": (
            "TestDeadCodeIncomingEntityIDsActiveRunBoundLive",
            "TestCrossRepoDeadCodeConsumerCoverageLive",
        ),
        "go/internal/query/content_reader_dead_code_refresh_probe_live_test.go": (
            "TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive",
        ),
        "go/internal/query/content_reader_dead_code_root_paths_live_test.go": (
            "TestCrossRepoDeadCodeConsumerRootPathsLive",
        ),
        "go/internal/query/content_reader_entity_metadata_by_ids_live_test.go": (
            "TestEntityMetadataByIDsLive",
        ),
        "go/internal/query/content_reader_entity_search_unscoped_live_test.go": (
            "TestSearchEntitiesUnscopedMatchesOldStatementLive",
            "TestEntityUnscopedSearchPlanShapesLive",
            "TestSearchEntitiesUnscopedEdgeRowsAreExactLive",
        ),
        "go/internal/query/content_reader_file_contents_live_test.go": (
            "TestGetFileContentsMatchesSingleReadsLive",
            "TestGetFileContentsPlanUsesAnIndexLive",
        ),
        "go/internal/query/content_reader_nonblank_live_test.go": (
            "TestNonBlankRepoFilePathsMatchesTrimSpaceLive",
            "TestNonBlankSpaceSetMatchesGoOverEveryRuneLive",
        ),
        "go/internal/query/content_reader_repository_catalog_live_test.go": (
            "TestMatchRepositoriesByIDsLive",
            "TestMatchRepositoriesByIDsPlanLive",
        ),
        "go/internal/query/content_reader_search_scoped_union_probe_live_test.go": (
            "TestUnionAllProbeDerivationGuard",
            "TestUnionAllProbePoisonLegKeepsBindPositions",
            "TestUnionAllProbeWorkAndEquivalenceLive",
            "TestRareEmptySingleScopeBitmapInvestigationLive",
        ),
        "go/internal/query/content_reader_search_scoped_workbound_live_test.go": (
            "TestScopedSearchShippedDerivationGuard",
            "TestScopedSearchCustomGenericEquivalenceLive",
        ),
        "go/internal/query/content_reader_search_unscoped_live_test.go": (
            "TestSearchFilesUnscopedMatchesOldStatementLive",
            "TestSearchFilesUnscopedCancelledTailResumesToExactAnswerLive",
            "TestUnscopedSearchPlanShapesLive",
            "TestSearchFilesUnscopedEdgeRowsAreExactLive",
            "TestSearchFilesUnscopedTailFilledPageKeepsMoreLive",
        ),
        "go/internal/query/content_reader_wide_window_live_test.go": (
            "TestWideWindowChunkPlansUseAnIndexLive",
            "TestWideWindowReadsMatchSingleReadsLive",
        ),
        "go/internal/query/security_alert_collection_state_postgres_live_test.go": (
            "TestSecurityAlertCollectionStateSemanticsLive",
            "TestSecurityAlertCollectionStateBoundsLive",
            "TestSecurityAlertCollectionStatePlanLive",
        ),
        "go/internal/query/security_alert_target_set_postgres_live_test.go": (
            "TestSecurityAlertTargetSetAllowlistChangeLive",
            "TestSecurityAlertTargetSetLegacyAndEmptyLive",
        ),
        "go/internal/query/security_alert_unread_state_postgres_live_test.go": (
            "TestSecurityAlertUnreadRepositoriesLive",
            "TestSecurityAlertUnreadRepositoriesPlanLive",
        ),
        "go/internal/query/status_terraform_selection_live_test.go": (
            "TestStatusRoutesTerraformSelectionLive",
        ),
    },
}
