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
        "helper_sha256": "be5a316245add75a72f0c9364f582a34c49bdae144ee959c96f2f3c6e6ec4fb4",
        "consumer_sha256": "0829951ad48a0d661c4e40992ac2795eabbab0074e49b7de2f49c9b5a052313a",
    },
    "go/internal/query/supply/chain/runtime_context_scope_live_test.go": {
        "helper_sha256": "8f0dc787498cc756ecf04afcba8f8128f5e70d55ebc32da764fb34adcc74545a",
        "consumers": [
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
                "consumer_sha256": "6905fa2907b1241d2242b2efa5d9e0a4dc6753ead3ef15ec4c16d1dd04457e97",
            },
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_plan_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFilterPlansLive",
                "consumer_sha256": "03a8883085e0d277ebc854dc79974676c5b3447373479da770a796d5833e7a88",
            },
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_workload_id_shape_live_test.go",
                "consumer_test": "TestSupplyChainImpactWorkloadIDFilterRepositoryIDShapeLive",
                "consumer_sha256": "c2944b44657dbb7475c0ad327c3b9beb5838db17863150d85e546631e188accc",
            },
        ],
    },
    "go/internal/query/supply/chain/runtime_normalization_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
        "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        "helper_sha256": "d240178abf57f9864e572b36b1a16158343461c7a5dee3486e1d0c2904206981",
        "consumer_sha256": "6905fa2907b1241d2242b2efa5d9e0a4dc6753ead3ef15ec4c16d1dd04457e97",
    },
    "go/internal/query/supply/chain/runtime_repository_precedence_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
        "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        "helper_sha256": "5450289f668e113c394bf2c799c2ad31d519d24a2af7657f8882b0fc7efa9349",
        "consumer_sha256": "6905fa2907b1241d2242b2efa5d9e0a4dc6753ead3ef15ec4c16d1dd04457e97",
    },
    "go/internal/query/supply/chain/suppression_authority_adversarial_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "574e514e6724665ec9bfbe89cfb30414e28ba48e686bcedd0173ff13ef772821",
        "consumer_sha256": "1bc0eb7a2b41092e889fa286f642a6f45eab9fa5b712fd58944d445795b9be50",
    },
    "go/internal/query/supply/chain/suppression_authority_assertions_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "a67ca8ce4d45cf56bb3400ec4336b80048e96dd7e8ba0c2e379deb79e884a19c",
        "consumer_sha256": "1bc0eb7a2b41092e889fa286f642a6f45eab9fa5b712fd58944d445795b9be50",
    },
    "go/internal/query/supply/chain/suppression_authority_scoped_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "ce9e7f8c2fcdb8a22de51f0e0061e14224c7cc0e0cf961658d0c4bfd4e493a93",
        "consumer_sha256": "1bc0eb7a2b41092e889fa286f642a6f45eab9fa5b712fd58944d445795b9be50",
    },
}
