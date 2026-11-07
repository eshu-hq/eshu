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
        "consumer_sha256": "1f2fe625df240f7090574df98fc1bfcc3b9562ddc901c17499ae6010a33c2a65",
    },
    "go/internal/query/supply/chain/runtime_context_scope_live_test.go": {
        "helper_sha256": "6ab6730caa6267a77dad04f6ad588e4e69f1f9965313240f2a1959b521a880cf",
        "consumers": [
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
                "consumer_sha256": "17cee4f2b4af8756d470661cdd001b10e25d0a6358e1c5a95e76c8479654d4ca",
            },
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_plan_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFilterPlansLive",
                "consumer_sha256": "9b80b29b8d2b855bf5f6e36bfab80e7ea99b6cce8da76cf570d16637cde92cb9",
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
        "helper_sha256": "55c539a5a68756531528779d3d78ed9006ad42a844622018adc76de77aa2b8bc",
        "consumer_sha256": "17cee4f2b4af8756d470661cdd001b10e25d0a6358e1c5a95e76c8479654d4ca",
    },
    "go/internal/query/supply/chain/runtime_repository_precedence_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
        "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        "helper_sha256": "1eff22510927a16f8a498b416ff64f2b461aeab1c2614e57d4f0f8320af9e130",
        "consumer_sha256": "17cee4f2b4af8756d470661cdd001b10e25d0a6358e1c5a95e76c8479654d4ca",
    },
    "go/internal/query/supply/chain/suppression_authority_adversarial_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "574e514e6724665ec9bfbe89cfb30414e28ba48e686bcedd0173ff13ef772821",
        "consumer_sha256": "7ba496387cc58051d8f795b2f162617dcdcd79bedf5a5a87beea3800414eb959",
    },
    "go/internal/query/supply/chain/suppression_authority_assertions_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "a67ca8ce4d45cf56bb3400ec4336b80048e96dd7e8ba0c2e379deb79e884a19c",
        "consumer_sha256": "7ba496387cc58051d8f795b2f162617dcdcd79bedf5a5a87beea3800414eb959",
    },
    "go/internal/query/supply/chain/suppression_authority_scoped_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "d011faaa67edd2515ef82f43d40706ca737f573d3e7e989984d0c704abed062d",
        "consumer_sha256": "7ba496387cc58051d8f795b2f162617dcdcd79bedf5a5a87beea3800414eb959",
    },
}
