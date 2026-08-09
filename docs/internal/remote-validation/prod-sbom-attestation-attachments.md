# prod-sbom-attestation-attachments — production validation

Validation-Slug: prod-sbom-attestation-attachments
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.sbom_attestation_attachments.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
