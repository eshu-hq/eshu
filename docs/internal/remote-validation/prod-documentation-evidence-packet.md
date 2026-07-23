# prod-documentation-evidence-packet — production validation

Capability: `documentation_evidence_packet.read` (tool
`get_documentation_evidence_packet`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

An immutable documentation evidence packet is served by `finding_id`, with
explicit visibility-denied handling and permission-envelope responses rather
than a silent empty result.

## Committed reproducible evidence

**Handler contract and visibility gating** —
`go/internal/query/documentation_test.go`:
`TestDocumentationHandlerReturnsEvidencePacketStates`,
`TestDocumentationHandlerDeniesEvidencePacketWhenVisibilityIsBlocked`, and
`TestDocumentationHandlerPermissionDeniedUsesEnvelopeWhenRequested`.
Reproduce:

```bash
cd go && go test ./internal/query -run 'TestDocumentationHandler.*EvidencePacket|TestContentReaderDocumentationEvidencePacket' -count=1
```

**Content-store hydration** —
`go/internal/query/documentation_test.go`:
`TestContentReaderDocumentationEvidencePacketDeniesBlockedVisibility`.

## Notes

No private data: cited tests use synthetic finding fixtures; no production
credentials or deployment-specific values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
