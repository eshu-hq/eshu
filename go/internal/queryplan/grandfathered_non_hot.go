// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

const grandfatheredNonHotBaseline = "220989280718f206e53fade8670c3b240d44a9b0"

// grandfatheredNonHotSourceDigests freezes the legacy prose dispositions that
// existed at 220989280718f206e53fade8670c3b240d44a9b0. New callsites cannot use
// non_hot_reason, and any source change forces the owning callsite through a
// typed non-hot audit or hot-path registration.
var grandfatheredNonHotSourceDigests = map[string]string{
	"codequery/handler.go:(*CodeHandler).runComplexityQuery":                                 "53fef37f7217c6b4e6aa26423fd1be4540f9e50cf8d0c0f0b5ce6f815635eaab",
	"codequery/callers.go:(*CodeHandler).nornicDBCallChainOneHopRows":                        "072603251b05d2e63cb29ed1deea87714e3b523d8e00f3e039a4b4143027c2a0",
	"codequery/callers.go:(*CodeHandler).callChainCandidateOneHopRows":                       "99714e60377715f1dd262e87d498648e808a367669fb4918c6048774e2ff7096",
	"codequery/registry_bundles.go:(*CodeHandler).handleSearchBundles":                       "70b0ca335b4a3d9ee3e34bc09e34a10d34b4704e1c9e366988eaf3408b26d76a",
	"codequery/relationship_handlers.go:(*CodeHandler).relationshipsGraphRow":                "5f06c0388917b255ecb9def87df274ec737d074f574a9c2d0a4e76981d3dee7d",
	"codequery/relationship_handlers.go:(*CodeHandler).transitiveRelationshipsGraphRow":      "3f078d6a49d10c2ca70b7e44d53f73b30465cd7a8e32d885ef48cccb3e99b909",
	"codequery/transitive_walk.go:(*CodeHandler).nornicDBTransitiveOneHopRows":               "79be400046fa15ea5feb891b6bfa334e23f61b90e34d411a3e924d7f120c37c6",
	"codequery/entity_labels.go:(*CodeHandler).nornicDBRelationshipEntityLabel":              "4309eda090298bffc0b957cebf4116c9c23225f4aec7121bc1e1f4ab5363f63f",
	"codequery/route_handlers.go:(*CodeHandler).routeToCallerEndpointRows":                   "b5cc5518d6e426f40f7d99b9ff04040b611d3ec3c181e68891d1cdea484516ce",
	"codequery/route_handlers.go:(*CodeHandler).routeToCallerHandlerLabel":                   "09b8ea96446aeae203f178584e3bf1bb3cbab358b0c6166474457c79e8f19f14",
	"codequery/route_handlers.go:(*CodeHandler).routeToCallerHandlerRows":                    "c5a3e0d2819fb89fcf5971b34e48b9efe05163fb4a53ff3b01c3bb52411fa266",
	"codequery/route_handlers.go:(*CodeHandler).routeToCallerImpactRows":                     "adda1007a5bc60c6351566a8baa4a9c1b290c807e58d39ade93e2afbfc355d50",
	"compare.go:(*CompareHandler).environmentSnapshot":                                       "246b5d64e58917ba4eb82213bf8374817c14398380f593dc9b2892e82dab033a",
	"compare.go:(*CompareHandler).fetchWorkload":                                             "cb8c6cefd899b3c46a8778cc13fb218c18564157d1e40882819c3c4b9a6684a9",
	"infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRelationshipCounts":           "22168d083366531f6a88eeee694dc46f6fbc46bfa2d0e4815b32bbce42cee411",
	"infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRepoEcosystemMap":             "f1fbee8ad896a2c3047c9eb0a317a009e1cd61222dd1facf39a33926c52b0875",
	"infra_graph_summary_packet.go:(*InfraHandler).graphSummaryRepoLanguages":                "40d0f7cbec027134499cc8398fbb75bda6819812002e675c5b11de57644b8e88",
	"infra_relationship_filter.go:(*InfraHandler).getRelationships":                          "8e93d2f5888cf3de38dafe0537c7c89c8821f5da4fb6ed580be70caea730a247",
	"infra_resource_aggregates.go:(GraphInfraResourceAggregateStore).CountInfraResources":    "9bd2b91998d9fc7f71d43e83f81ff50b5decdd35736ff06f9707478c82c37117",
	"infra_resource_aggregates.go:(GraphInfraResourceAggregateStore).InfraResourceInventory": "7e1258c40386cdd5205151bb85a028927ff5c99a1b1628a9ebbab1769440d082",
	"neo4j.go:(*Neo4jReader).RelationshipTypes":                                              "0a1d2ac1a82d38e0bd8766758df6b1f894f95c94a169c2419666ab521f8bcce4",
	"neo4j.go:(*Neo4jReader).RunSingle":                                                      "b77731433ac905d12ca935b11decf557c81280901ef22fcdfb0a3bf2dedf1227",
	"status.go:(*StatusHandler).getIndexStatus":                                              "573b83514e91247fca70bd919a4f22eb7491f2f600fb984c152d1906bd04b3f8",
}
