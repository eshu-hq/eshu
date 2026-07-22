# prod-visualization-packet — production validation

Capability: `visualization.packet_derivation` (tool
`derive_visualization_packet`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_packet`, `p95_latency_ms: 400`,
`max_truth_level: derived`.

## Claim validated

Pure in-memory transform of a caller-supplied source response
(`service_story`, `evidence_citation`, or `incident_context`) into a
node/edge-bounded visualization packet, with node and edge caps; the packet
carries the source response's truth envelope while the derivation response
envelope stays `derived`. No graph or content read.

## Committed reproducible evidence

**Deterministic ordering, truncation caps, and privacy invariants across all
three source response kinds** — `go/internal/query/visualization_packet_test.go`:
`TestServiceStoryVisualizationDeterministicOrdering`,
`TestServiceStoryVisualizationStableIDsAcrossRuns`,
`TestServiceStoryVisualizationTruncatesNodes`,
`TestServiceStoryVisualizationPrivacyInvariant`,
`TestServiceStoryVisualizationUnsupported`,
`TestEvidenceCitationVisualizationDeterministicOrdering`,
`TestEvidenceCitationVisualizationPrivacyInvariant`,
`TestEvidenceCitationVisualizationTruncates`,
`TestEvidenceCitationVisualizationUnsupported`,
`TestIncidentVisualizationDeterministicAndTruthLabels`,
`TestIncidentVisualizationUnsupported`, and
`TestVisualizationPacketPreservesTruth` (proves the packet carries the source
response's truth level rather than downgrading or upgrading it). Reproduce:

```bash
cd go && go test ./internal/query -run 'TestServiceStoryVisualization|TestEvidenceCitationVisualization|TestIncidentVisualization|TestVisualizationPacketPreservesTruth' -count=1
```

**Route-level packet derivation and canonical merge behavior** —
`go/internal/query/visualization_packet_surface_test.go`:
`TestVisualizationDeriveRouteBuildsServiceStoryPacket`,
`TestVisualizationDeriveRouteSupportsEvidenceCitationAndIncidentContext`,
`TestVisualizationDeriveRouteReturnsUnsupportedPacketForEmptyKnownView`,
`TestVisualizationDeriveRouteRejectsUnknownView`, and
`TestOpenAPISpecIncludesVisualizationDeriveRoute`; and
`go/internal/query/visualization_packet_merge_test.go`:
`TestServiceStoryVisualizationCanonicalCollapseIsOrderIndependent` and
`TestServiceStoryVisualizationCarriesKnownSourceDroppedEdgeCount`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestVisualizationDeriveRoute|TestOpenAPISpecIncludesVisualizationDeriveRoute|TestServiceStoryVisualizationCanonicalCollapseIsOrderIndependent|TestServiceStoryVisualizationCarriesKnownSourceDroppedEdgeCount' -count=1
```

## Notes

No private data: cited tests assert node/edge caps and privacy invariants
directly; the capability performs no graph or content read of its own.

Related: #5552 (burn-down).
