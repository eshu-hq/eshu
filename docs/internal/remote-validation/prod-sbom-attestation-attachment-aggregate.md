# prod-sbom-attestation-attachment-aggregate — production validation

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
