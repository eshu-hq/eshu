#!/usr/bin/env python3
"""Closed source dependencies exercised by enrolled readiness tests.

These are Go files with no Test root of their own. Whole-file hashes
bind the reviewed helper and consumer calls to the runtime proof.
A source edit needs renewed review and proof before either pin
changes. Generated; do not edit by hand.
"""

HELPER_PROOFS = {
    "go/internal/query/supply/chain/impact/suppression_plan_proof_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/impact/suppression_paths_performance_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionPathsPerformanceLive",
        "helper_sha256": "14a4f3983bdc2ba54930442d31b1140934af8cb31cecfdc541936e5122dae3be",
        "consumer_sha256": "baa21b17308079cec3fbe72617e5e1a0dc1e5f0bd537723869375b6358dca670",
    },
}
