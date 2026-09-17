// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomattestation

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// sbomAttestationAttachmentCandidateFactKinds are the fact kinds
// sbomAttestationAttachmentTriggerFact accepts.
var sbomAttestationAttachmentCandidateFactKinds = []string{
	facts.SBOMDocumentFactKind, facts.AttestationStatementFactKind, facts.OCIImageReferrerFactKind,
}

// BuildSBOMAttestationAttachmentReducerIntent enqueues one reducer intent that
// asks the reducer to attach the scope generation's SBOM documents and
// attestation statements to canonical image subjects. An sbom.document,
// attestation.statement, or OCI referrer fact is a trigger — each carries a
// subject anchor — and the intent anchors to the earliest such fact in
// original input order across the three kinds, so the reducer claim is stable
// across reprojections of the same generation. Component-only SBOM evidence
// never triggers: components, dependency edges, external references, and
// warnings only enrich the reducer decision once a document-scoped intent
// exists. Only envelope.FactKind is read — no payload is decoded here, and
// subject-digest admission stays with the reducer's
// DomainSBOMAttestationAttachment handler. A generation with none of the three
// kinds enqueues nothing.
func BuildSBOMAttestationAttachmentReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(sbomAttestationAttachmentTriggerFact, sbomAttestationAttachmentCandidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSBOMAttestationAttachment,
		EntityKey:    "sbom_attestation_attachment:" + scopeID,
		Reason:       "sbom or attestation subject evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// sbomAttestationAttachmentTriggerFact accepts the subject-anchor fact kinds:
// an SBOM document, an attestation statement, or an OCI referrer whose payload
// carries both subject and referrer digests.
func sbomAttestationAttachmentTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.SBOMDocumentFactKind, facts.AttestationStatementFactKind, facts.OCIImageReferrerFactKind:
		return true
	default:
		return false
	}
}
