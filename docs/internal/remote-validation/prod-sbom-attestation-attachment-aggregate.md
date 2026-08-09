# prod-sbom-attestation-attachment-aggregate — production validation

Validation-Slug: prod-sbom-attestation-attachment-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.sbom_attestation_attachments.aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `supply_chain.sbom_attestation_attachments.aggregate` (tools
`count_sbom_attestation_attachments`, `get_sbom_attestation_attachment_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_subject_document_attachment_status_or_artifact_kind_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer SBOM/attestation attachment aggregate returning grouped counts by
`attachment_status`, `artifact_kind`, or `subject_digest`, replacing a
page-and-iterate caller workflow for ecosystem-totals questions.

## Committed reproducible evidence

**Aggregate rollup contract and scope forwarding** —
`go/internal/query/sbom_attestation_attachment_aggregates_test.go`:
`TestSBOMAttestationAttachmentAggregateCountReturnsRollups`,
`TestSBOMAttestationAttachmentAggregateInventoryReturnsBuckets`,
`TestSBOMAttestationAttachmentAggregateRoutesForwardSourceScopes`,
`TestSBOMAttestationAttachmentAggregateRoutesDoNotDropServiceScope`,
`TestSBOMAttestationAttachmentAggregateRoutesAcceptRepositoryScope`, and
`TestSBOMAttestationAttachmentAggregateRoutesReturn503WhenStoreMissing` (asserts the
route degrades explicitly rather than fabricating zero counts when the reducer
store is absent). Reproduce:

```bash
cd go && go test ./internal/query -run TestSBOMAttestationAttachmentAggregate -count=1
```

**Missing-evidence and rollup edge cases** —
`go/internal/query/sbom_attestation_attachment_aggregate_missing_evidence_test.go` and
`go/internal/query/sbom_attestation_attachment_aggregates_rollup_test.go`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestSBOMAttestationAttachment' -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` asserts `mcp_sbom_attachments` and
`sbom_attachments` counts (via `list_sbom_attestation_attachments` /
`get_sbom_attestation_attachment_inventory`) against a live deployed stack, driven
by an operator-supplied `ESHU_REMOTE_E2E_TARGET_STORY_FILE` and
`ESHU_REMOTE_E2E_API_BASE_URL`. The script's own local proof —
`scripts/test-verify-remote-e2e-target-story-artifact-anchors.sh` — exercises its
SBOM-anchor assertion logic against fake fixtures without live credentials.
Reproduce the local proof:

```bash
scripts/test-verify-remote-e2e-target-story-artifact-anchors.sh
```

Reproduce the live target-story check (requires a reachable deployed stack and
target-story fixture):

```bash
ESHU_REMOTE_E2E_TARGET_STORY_FILE=<path> ESHU_REMOTE_E2E_API_BASE_URL=<url> \
  scripts/verify_remote_e2e_target_story.sh
```

## Notes

No private data: the aggregate response carries counts and bucket labels only,
never raw SBOM document content or attestation payloads; this artifact cites only
committed tests/scripts, not any deployment-specific values.

Related: #5552 (burn-down).
