# prod-evidence-citation-packet — production validation

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
