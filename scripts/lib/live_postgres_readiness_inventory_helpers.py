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
        "consumer_sha256": "b7c37aa8ee02cea3e76029a166a7bb34b99121f5571c6c54c404c73e709e5f0a",
    },
    "go/internal/query/supply/chain/runtime_context_scope_live_test.go": {
        "helper_sha256": "fcc2b6acd87a5014d100a3023cc5aeceee28b49bb41d9ec5d85cbf2d1893ef21",
        "consumers": [
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
                "consumer_sha256": "c8a7eb7a548b69af192dd346973266dbaa7217bb3a7337ceb18223474273ccdc",
            },
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_plan_live_test.go",
                "consumer_test": "TestSupplyChainImpactRuntimeFilterPlansLive",
                "consumer_sha256": "c4fc8a9c1a9e42ed3fbbed8d10cb818e37ab12173d5d43d5c94a043cd42130ea",
            },
            {
                "consumer_file": "go/internal/query/supply/chain/runtime_filter_workload_id_shape_live_test.go",
                "consumer_test": "TestSupplyChainImpactWorkloadIDFilterRepositoryIDShapeLive",
                "consumer_sha256": "f1f2421f855582c9f5f6b361e7a1fb1c28c9f2fe846fd8666b864d3565a1629c",
            },
        ],
    },
    "go/internal/query/supply/chain/runtime_normalization_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
        "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        "helper_sha256": "df9f855be621c120fc77611606100fc7e3db077bb81462855308367c2e53fc07",
        "consumer_sha256": "c8a7eb7a548b69af192dd346973266dbaa7217bb3a7337ceb18223474273ccdc",
    },
    "go/internal/query/supply/chain/runtime_repository_precedence_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/runtime_filter_live_test.go",
        "consumer_test": "TestSupplyChainImpactRuntimeFiltersEnforceScopedTruthLive",
        "helper_sha256": "6b2ed7ab4ede3d4a274c57aeff9b10efc59075eb94ac66ddb7aef707e19aa395",
        "consumer_sha256": "c8a7eb7a548b69af192dd346973266dbaa7217bb3a7337ceb18223474273ccdc",
    },
    "go/internal/query/supply/chain/suppression_authority_adversarial_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "574e514e6724665ec9bfbe89cfb30414e28ba48e686bcedd0173ff13ef772821",
        "consumer_sha256": "4a5217033a92d09ea483bd9ce98fda7aa122e5716c77345a7471b6965c35f7cb",
    },
    "go/internal/query/supply/chain/suppression_authority_assertions_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "7b5bfe8e62cf5ecceb720257b1bf54d587d379d4677b981d14e8ffd385bfb8f2",
        "consumer_sha256": "4a5217033a92d09ea483bd9ce98fda7aa122e5716c77345a7471b6965c35f7cb",
    },
    "go/internal/query/supply/chain/suppression_authority_scoped_live_test.go": {
        "consumer_file": "go/internal/query/supply/chain/suppression_authority_live_test.go",
        "consumer_test": "TestSupplyChainSuppressionAuthorityDirectAndMaterializedParityLive",
        "helper_sha256": "d011faaa67edd2515ef82f43d40706ca737f573d3e7e989984d0c704abed062d",
        "consumer_sha256": "4a5217033a92d09ea483bd9ce98fda7aa122e5716c77345a7471b6965c35f7cb",
    },
}
