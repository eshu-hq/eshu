// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// indexAttestationSLSAProvenance decodes one attestation.slsa_provenance
// envelope and, when it carries a non-empty statement_id, records it under
// that key in index.slsaProvenance. Split out of buildSBOMAttachmentIndex's
// switch (sbom_attestation_attachment_index.go) to keep that function under
// the package's function-length limit, mirroring
// indexSBOMDependencyRelationship and indexSBOMExternalReference. Returns the
// same (quarantinedFact, isQuarantine, fatal) triple as
// partitionDecodeFailures for the caller to fold into its own quarantine
// slice and fatal-error short-circuit.
func indexAttestationSLSAProvenance(
	index sbomAttachmentIndex,
	envelope facts.Envelope,
) (quarantinedFact, bool, error) {
	provenance, err := decodeAttestationSLSAProvenance(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	indexSLSAProvenanceEvidence(index, provenance, envelope.FactID)
	return quarantinedFact{}, false, nil
}

// indexSLSAProvenanceEvidence records one decoded attestation.slsa_provenance
// fact under its owning statement_id. When a second fact joins the same
// statement_id, the row whose factID sorts lexicographically smallest wins;
// the loser is discarded whole rather than merged field-by-field, so a
// duplicate emission can never splice fields from two different facts into
// one row.
func indexSLSAProvenanceEvidence(
	index sbomAttachmentIndex,
	provenance sbomv1.SLSAProvenance,
	factID string,
) {
	if provenance.StatementID == "" {
		return
	}
	candidate := sbomAttachmentSLSAProvenanceEvidence{
		factID:        factID,
		predicateType: derefString(provenance.PredicateType),
		builderID:     derefString(provenance.BuilderID),
		materials:     provenance.Materials,
		configSource:  provenance.ConfigSource,
	}
	existing, ok := index.slsaProvenance[provenance.StatementID]
	if ok && existing.factID <= candidate.factID {
		return
	}
	index.slsaProvenance[provenance.StatementID] = candidate
}
