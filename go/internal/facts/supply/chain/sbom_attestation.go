// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// SBOMAttestationFamilyDeclaration is the typed fact-family contract declaration for the
// sbom_attestation family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var SBOMAttestationFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "sbom_attestation",
	FactDir:                "go/internal/facts/supply/chain",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.1.0",
	ReducerDomain:          "sbom_attestation_attachment",
	ProjectionHook:         "sbom_attestation_attachment",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/supply-chain/sbom-attestations/attachments",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "attestation.signature_verification", PayloadSchema: "sdk/go/factschema/schema/attestation.signature_verification.v1.schema.json"},
		{Kind: "attestation.slsa_provenance", PayloadSchema: "sdk/go/factschema/schema/attestation.slsa_provenance.v1.schema.json"},
		{Kind: "attestation.statement", PayloadSchema: "sdk/go/factschema/schema/attestation.statement.v1.schema.json"},
		{Kind: "sbom.component", PayloadSchema: "sdk/go/factschema/schema/sbom.component.v1.schema.json"},
		{Kind: "sbom.dependency_relationship", PayloadSchema: "sdk/go/factschema/schema/sbom.dependency_relationship.v1.schema.json"},
		{Kind: "sbom.document", PayloadSchema: "sdk/go/factschema/schema/sbom.document.v1.schema.json"},
		{Kind: "sbom.external_reference", PayloadSchema: "sdk/go/factschema/schema/sbom.external_reference.v1.schema.json"},
		{Kind: "sbom.warning", PayloadSchema: "sdk/go/factschema/schema/sbom.warning.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-sbom-attestation", Repository: "eshu-hq/eshu", Path: "go/internal/collector/sbom", PlannedRepository: "eshu-hq/eshu-collector-sbom-attestation", AuthorityMode: "core_issued_grant", Kinds: []string{"attestation.signature_verification", "attestation.slsa_provenance", "attestation.statement", "sbom.dependency_relationship", "sbom.external_reference"}},
		{ID: "scanner-worker", Repository: "eshu-hq/eshu", Path: "go/internal/collector/scannerworker", AuthorityMode: "core_owned", Kinds: []string{"sbom.component", "sbom.document", "sbom.warning"}},
	},
})
