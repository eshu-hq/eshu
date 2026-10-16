#!/usr/bin/env python3
"""Expected live proofs for the writer, admin, reachability, config-artifact and search packages (data only)."""

from live_postgres_readiness_inventory_names import (
    ADMIN_PACKAGE,
    CONFIG_ARTIFACTS_PACKAGE,
    REACHABILITY_PACKAGE,
    UNSCOPED_SEARCH_PACKAGE,
    WRITER_PACKAGE,
)

SURFACE_PACKAGES = {
    WRITER_PACKAGE: {
        "go/internal/reducer/status/summary/runner_live_test.go": (
            "TestWriterRowEqualsLiveStatementLive",
            "TestWriterKilledMidPassKeepsTheOldRowLive",
            "TestWriterCountsAGuardRejectionLive",
            "TestWriterSkipsAMissingTableLive",
            "TestWriterReplacesARowFromAnotherStatementLive",
            "TestWriterPassRunsReadCommittedLive",
            "TestSecondWriterSkipsWhileTheFirstHoldsTheLockLive",
        ),
        "go/internal/reducer/status/summary/contention_live_test.go": (
            "TestWritersBesideTheProductionClaimLoopLive",
        ),
        "go/internal/reducer/status/summary/reader_live_test.go": (
            "TestReaderServesWhatTheWriterStoredEqualToLiveLive",
            "TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive",
        ),
        "go/internal/reducer/status/summary/scrape_live_test.go": (
            "TestScrapeServesTheStoredRowAndNeverTheLiveStatementLive",
            "TestScrapeStatementInventoryOnAnEmptyStoreLive",
            "TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive",
        ),
        "go/internal/reducer/status/summary/terraform_live_test.go": (
            "TestTerraformModelServedEqualToLiveLive",
            "TestWriterKilledMidCompanionKeepsThePrimaryRowLive",
        ),
    },
    ADMIN_PACKAGE: {
        "go/internal/query/admin/reopen_live_test.go": (
            "TestAdminHandler_ReopenLive",
        ),
        "go/internal/query/admin/scope_selector_live_test.go": (
            "TestAdminHandler_ScopeSelectorCollisionLive",
        ),
        "go/internal/query/admin/reopen_rollover_live_test.go": (
            "TestAdminHandler_ReopenRolloverActsOnCurrentGenerationLive",
        ),
    },
    REACHABILITY_PACKAGE: {
        "go/internal/storage/postgres/code/reachability/loader_completed_index_live_test.go": (
            "TestCodeReachabilityLoaderUsesCompletedIntentsIndex",
        ),
        "go/internal/storage/postgres/code/reachability/loader_edges_scope_live_test.go": (
            "TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly",
        ),
    },
    CONFIG_ARTIFACTS_PACKAGE: {
        "go/internal/storage/postgres/config/artifacts/dirty_live_test.go": (
            "TestListDirtyFindsTheWalkersWorkLive",
            "TestListDirtyAgreesWithClassifyLive",
        ),
        "go/internal/storage/postgres/config/artifacts/encoding_live_test.go": (
            "TestCheckEncodingReadsTheDatabaseEncodingLive",
        ),
        "go/internal/storage/postgres/config/artifacts/schema_live_test.go": (
            "TestRowDigestMatchesGoRecomputationLive",
            "TestRowKeyRejectsADuplicateRowLive",
            "TestReadModelCatalogLive",
            "TestMigrationReRunsLive",
        ),
        "go/internal/storage/postgres/config/artifacts/walk_live_test.go": (
            "TestClaimPageWalksEveryRepositoryAndWrapsLive",
            "TestClaimPageAtAnExactPageBoundaryLive",
            "TestClaimPageGivesConcurrentClaimersDisjointPagesLive",
        ),
    },
    UNSCOPED_SEARCH_PACKAGE: {
        "go/internal/query/search/unscoped/large_document_live_test.go": (
            "TestResumeChainCrossesLargeDocumentLive",
            "TestLargeDocumentLowercaseRecheckMatchesILIKEOracleLive",
            "TestLargeDocumentCorrectOrDisclosedLive",
        ),
    },
}
