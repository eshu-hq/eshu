// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// SBOMDocumentFactKind identifies one parsed or attempted SBOM document.
	SBOMDocumentFactKind = "sbom.document"
	// SBOMComponentFactKind identifies one component from an SBOM document.
	SBOMComponentFactKind = "sbom.component"
	// SBOMDependencyRelationshipFactKind identifies one SBOM dependency edge.
	SBOMDependencyRelationshipFactKind = "sbom.dependency_relationship"
	// SBOMExternalReferenceFactKind identifies one SBOM external reference.
	SBOMExternalReferenceFactKind = "sbom.external_reference"
	// AttestationStatementFactKind identifies one in-toto statement envelope.
	AttestationStatementFactKind = "attestation.statement"
	// AttestationSLSAProvenanceFactKind identifies one SLSA provenance predicate.
	AttestationSLSAProvenanceFactKind = "attestation.slsa_provenance"
	// AttestationSignatureVerificationFactKind identifies one signature verification result.
	AttestationSignatureVerificationFactKind = "attestation.signature_verification"
	// SBOMWarningFactKind identifies non-fatal SBOM or attestation warnings.
	SBOMWarningFactKind = "sbom.warning"

	// SBOMAttestationSchemaVersionV1 is the SBOM/attestation fact schema
	// version. Bumped 1.0.0 -> 1.1.0 (#5456) for the additive-optional
	// materials/config_source fields added to attestation.slsa_provenance
	// (sdk/go/factschema/sbom/v1.SLSAProvenance); no existing field, kind, or
	// required set changed, so every other kind in this shared family stays
	// backward compatible under the bump.
	SBOMAttestationSchemaVersionV1 = "1.1.0"
)

var sbomAttestationFactKinds = []string{
	SBOMDocumentFactKind,
	SBOMComponentFactKind,
	SBOMDependencyRelationshipFactKind,
	SBOMExternalReferenceFactKind,
	AttestationStatementFactKind,
	AttestationSLSAProvenanceFactKind,
	AttestationSignatureVerificationFactKind,
	SBOMWarningFactKind,
}

var sbomAttestationSchemaVersions = map[string]string{
	SBOMDocumentFactKind:                     SBOMAttestationSchemaVersionV1,
	SBOMComponentFactKind:                    SBOMAttestationSchemaVersionV1,
	SBOMDependencyRelationshipFactKind:       SBOMAttestationSchemaVersionV1,
	SBOMExternalReferenceFactKind:            SBOMAttestationSchemaVersionV1,
	AttestationStatementFactKind:             SBOMAttestationSchemaVersionV1,
	AttestationSLSAProvenanceFactKind:        SBOMAttestationSchemaVersionV1,
	AttestationSignatureVerificationFactKind: SBOMAttestationSchemaVersionV1,
	SBOMWarningFactKind:                      SBOMAttestationSchemaVersionV1,
}

// SBOMAttestationFactKinds returns the accepted SBOM and attestation fact
// kinds in their source contract order.
func SBOMAttestationFactKinds() []string {
	return slices.Clone(sbomAttestationFactKinds)
}

// SBOMAttestationSchemaVersion returns the schema version for an SBOM or
// attestation fact kind.
func SBOMAttestationSchemaVersion(factKind string) (string, bool) {
	version, ok := sbomAttestationSchemaVersions[factKind]
	return version, ok
}
