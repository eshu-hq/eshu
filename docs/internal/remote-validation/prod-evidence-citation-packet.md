# prod-evidence-citation-packet — production validation

Validation-Slug: prod-evidence-citation-packet
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: evidence_citation.packet passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: evidence_citation.packet -> mcp:build_evidence_citation_packet

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `evidence_citation.packet` (tool
`build_evidence_citation_packet`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: bounded_handle_batch`,
`p95_latency_ms: 1200`, `max_truth_level: derived`.

## Claim validated

A bounded citation packet for source, docs, manifest, and deployment evidence
is built via Postgres content-store hydration of file and entity handles,
with explicit missing/truncated-handle reporting rather than a silently
incomplete packet.

## Committed reproducible evidence

**Handler contract, empty-handle rejection, and missing/truncated
reporting** — `go/internal/query/evidence_citation_test.go`:
`TestEvidenceHandlerBuildEvidenceCitationsPacketFromFileAndEntityHandles`,
`TestEvidenceHandlerCitationPacketRejectsEmptyHandles`, and
`TestEvidenceHandlerCitationPacketReportsMissingAndTruncatedHandles`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestEvidenceHandler -count=1
```

**Request normalization and truncation-probe bounds** —
`go/internal/query/evidence_citation_test.go`:
`TestNormalizeEvidenceCitationRequestPreservesDistinctFileCitations`,
`TestNormalizeEvidenceCitationRequestStopsAfterTruncationProbe`, and
`TestNormalizeEvidenceCitationRequestRejectsOversizedHandleArrays`.

**Content-store batch hydration** —
`go/internal/query/evidence_citation_test.go`:
`TestContentReaderEvidenceCitationFilesHydratesBatch`.

## Notes

No private data: cited tests use synthetic file/entity handle fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
