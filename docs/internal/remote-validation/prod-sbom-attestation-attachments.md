# prod-sbom-attestation-attachments — production validation

Capability: `supply_chain.sbom_attestation_attachments.list` (tool
`list_sbom_attestation_attachments`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: digest_or_document_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer SBOM and attestation attachment lookup anchored by subject
digest, document id, or document digest.

## Committed reproducible evidence

**Bounded lookup, scope anchors, and missing-evidence explanation** —
`go/internal/query/sbom_attestation_attachments_test.go`:
`TestSupplyChainListSBOMAttestationAttachmentsRequiresScopeAndLimit`,
`TestSupplyChainListSBOMAttestationAttachmentsUsesBoundedStore`,
`TestSupplyChainListSBOMAttestationAttachmentsAcceptsWorkloadServiceAnchors`,
`TestSupplyChainListSBOMAttestationAttachmentsAcceptsRepositoryScope`,
`TestSBOMAttestationAttachmentQueryUsesActiveFactReadModel`,
`TestDecodeSBOMAttestationAttachmentRowPreservesAnchorTruth`, and
`TestSBOMAttestationAttachmentMissingEvidenceQueryExplainsScopedGaps` (proves
missing/stale evidence is surfaced, not dropped). Reproduce:

```bash
cd go && go test ./internal/query -run 'TestSupplyChainListSBOMAttestationAttachments|TestSBOMAttestationAttachmentQuery|TestDecodeSBOMAttestationAttachmentRow|TestSBOMAttestationAttachmentMissingEvidence' -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` asserts `sbom_attachments` and
`sbom_missing_evidence` counts against a live deployed stack. Local proof of the
script's own assertion logic —
`scripts/test-verify-remote-e2e-target-story-artifact-anchors.sh` — runs without
live credentials:

```bash
scripts/test-verify-remote-e2e-target-story-artifact-anchors.sh
```

## Notes

No private data: cited evidence covers subject digests and document ids only,
never raw SBOM or attestation document payloads.

Related: #5552 (burn-down).
