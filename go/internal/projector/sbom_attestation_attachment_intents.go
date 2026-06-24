// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildSBOMAttestationAttachmentReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if !sbomAttestationAttachmentTriggerFact(envelope) {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainSBOMAttestationAttachment,
			EntityKey:    "sbom_attestation_attachment:" + scopeValue.ScopeID,
			Reason:       "sbom or attestation subject evidence observed",
			FactID:       envelope.FactID,
			SourceSystem: sbomAttestationAttachmentSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func sbomAttestationAttachmentTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.SBOMDocumentFactKind, facts.AttestationStatementFactKind, facts.OCIImageReferrerFactKind:
		return true
	default:
		return false
	}
}

func sbomAttestationAttachmentSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
