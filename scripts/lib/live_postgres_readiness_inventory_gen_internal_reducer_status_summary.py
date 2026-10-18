#!/usr/bin/env python3
"""Readiness enrollment for ./internal/reducer/status/summary (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/reducer/status/summary": {
        "go/internal/reducer/status/summary/contention_live_test.go": (
            "TestWritersBesideTheProductionClaimLoopLive",
            "TestDrainActiveBackendsLive",
        ),
        "go/internal/reducer/status/summary/reader_live_test.go": (
            "TestReaderServesWhatTheWriterStoredEqualToLiveLive",
            "TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive",
        ),
        "go/internal/reducer/status/summary/runner_live_test.go": (
            "TestWriterRowEqualsLiveStatementLive",
            "TestWriterKilledMidPassKeepsTheOldRowLive",
            "TestWriterCountsAGuardRejectionLive",
            "TestWriterSkipsAMissingTableLive",
            "TestWriterReplacesARowFromAnotherStatementLive",
            "TestWriterPassRunsReadCommittedLive",
            "TestSecondWriterSkipsWhileTheFirstHoldsTheLockLive",
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
}
