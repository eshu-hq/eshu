#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/search/unscoped (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/search/unscoped": {
        "go/internal/query/search/unscoped/large_document_live_test.go": (
            "TestResumeChainCrossesLargeDocumentLive",
            "TestLargeDocumentLowercaseRecheckMatchesILIKEOracleLive",
            "TestLargeDocumentCorrectOrDisclosedLive",
        ),
    },
}
