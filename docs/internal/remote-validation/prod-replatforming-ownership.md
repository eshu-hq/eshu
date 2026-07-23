# prod-replatforming-ownership — production validation

Capability: `replatforming.ownership.candidates` (tool
`find_unmanaged_resource_owners`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: account_or_scope`, `p95_latency_ms: 5000`,
`max_truth_level: derived`.

## Claim validated

Bounded ownership packet per active AWS runtime-drift finding with
owner/repository/module/service/environment candidates; ambiguous or
missing attribution is surfaced explicitly and never promoted to a single
owner; raw tags stay provenance-only and never become an owner candidate.

## Committed reproducible evidence

**Packet composition, single-vs-ambiguous candidates, tag provenance, safety
gating** — `go/internal/query/replatforming_ownership_test.go`:
`TestBuildOwnershipPacketCloudOnlyNoServiceMatch`,
`TestBuildOwnershipPacketSingleServiceIsDerivedNotExact`,
`TestBuildOwnershipPacketAmbiguousServiceCandidatesCarryReasons`,
`TestBuildOwnershipPacketStateOnlyExposesModuleAndConfig`,
`TestBuildOwnershipPacketTagsAreProvenanceNeverOwner`,
`TestBuildOwnershipPacketRejectedFindingNeverImportReady`,
`TestBuildOwnershipPacketSummaryReportsCandidatesAndAccount`. Reproduce:

```bash
cd go && go test ./internal/query -run TestBuildOwnershipPacket -count=1
```

**Handler-level composition, profile gating, raw-tag non-leakage** —
`go/internal/query/replatforming_ownership_handler_test.go`:
`TestOwnershipPacketsUnsupportedOnLightweightProfile`,
`TestOwnershipPacketsRequiresScopeOrAccount`,
`TestReplatformingOwnershipPacketsComposesCandidatesAndPreservesTruth`,
`TestOwnershipPacketsDoNotLeakRawTagValues`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOwnershipPackets -count=1
cd go && go test ./internal/query -run TestReplatformingOwnershipPackets -count=1
```

**OpenAPI contract declaration** —
`go/internal/query/openapi_replatforming_ownership_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
