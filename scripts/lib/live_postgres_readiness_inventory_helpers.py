#!/usr/bin/env python3
"""Closed source dependencies exercised by enrolled PostgreSQL readiness tests.

These are Go files with no Test root of their own. Whole-file hashes bind the
reviewed helper and consumer calls to the runtime proof. A source edit needs
renewed review and proof before either pin changes.
"""

HELPER_PROOFS = {
    "go/internal/query/supply/chain/impact/suppression_plan_proof_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionPathsPerformanceLive",
        "helper_sha256": "2f5767807ad4d47896193e2fe4bd291161aab845c61983dbfef8659338cfaa7e",
        "consumer_sha256": "0f9bba8d97de2d4cb3c32b9571ef86e1eab56486558cf4fc4765f26a0f1a32ad",
    },
}
