# prod-documentation-packet-freshness — production validation

Capability: `documentation_evidence_packet.freshness` (tool
`check_documentation_evidence_packet_freshness`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 800`, `max_truth_level: exact`.

## Claim validated

Documentation evidence packet freshness is served by `packet_id`, reusing the
same permission-gated packet read model as
`documentation_evidence_packet.read`.

## Committed reproducible evidence

**Handler contract** — `go/internal/query/documentation_test.go`:
`TestDocumentationHandlerReturnsPacketFreshness`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDocumentationHandlerReturnsPacketFreshness -count=1
```

## Notes

No private data: the cited test uses a synthetic finding/packet fixture; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
