# prod-replatforming-ownership — production validation

Validation-Slug: prod-replatforming-ownership
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: replatforming.ownership.candidates passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: replatforming.ownership.candidates -> mcp:find_unmanaged_resource_owners

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
