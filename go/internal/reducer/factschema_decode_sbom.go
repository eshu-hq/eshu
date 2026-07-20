// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// decodeSBOMDocument decodes one sbom.document envelope into the typed
// sbomv1.Document struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// document_id field or is otherwise malformed. It is the single decode site
// for the sbom.document kind on the reducer side: sbomAttachmentIndex decodes
// through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent orphaned document.
func decodeSBOMDocument(env facts.Envelope) (sbomv1.Document, error) {
	document, err := factschema.DecodeSBOMDocument(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.Document{}, newFactDecodeError(factschema.FactKindSBOMDocument, err)
	}
	return document, nil
}

// decodeSBOMComponent decodes one sbom.component envelope into the typed
// sbomv1.Component struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// document_id field or is otherwise malformed. It is the single decode site
// for the WIRED sbom.component consumer (sbomAttachmentIndex); the
// supply_chain_impact matcher (supplyChainSBOMComponentFromEnvelope) still
// reads this kind raw, pending its own migration.
func decodeSBOMComponent(env facts.Envelope) (sbomv1.Component, error) {
	component, err := factschema.DecodeSBOMComponent(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.Component{}, newFactDecodeError(factschema.FactKindSBOMComponent, err)
	}
	return component, nil
}

// decodeSBOMDependencyRelationship decodes one sbom.dependency_relationship
// envelope into the typed sbomv1.DependencyRelationship struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required document_id field or is otherwise
// malformed. It is the single decode site for the WIRED
// sbom.dependency_relationship consumer (sbomAttachmentIndex).
func decodeSBOMDependencyRelationship(env facts.Envelope) (sbomv1.DependencyRelationship, error) {
	relationship, err := factschema.DecodeSBOMDependencyRelationship(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.DependencyRelationship{}, newFactDecodeError(factschema.FactKindSBOMDependencyRelationship, err)
	}
	return relationship, nil
}

// decodeSBOMExternalReference decodes one sbom.external_reference envelope
// into the typed sbomv1.ExternalReference struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required document_id field or is otherwise malformed. It is the single
// decode site for the WIRED sbom.external_reference consumer
// (sbomAttachmentIndex).
func decodeSBOMExternalReference(env facts.Envelope) (sbomv1.ExternalReference, error) {
	reference, err := factschema.DecodeSBOMExternalReference(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.ExternalReference{}, newFactDecodeError(factschema.FactKindSBOMExternalReference, err)
	}
	return reference, nil
}

// decodeSBOMWarning decodes one sbom.warning envelope into the typed
// sbomv1.Warning struct through the contracts seam. sbomv1.Warning has zero
// required fields (two collector paths emit mutually-exclusive identity
// keys — see sbom/v1/document.go), so this only returns an error for an
// unsupported schema major or an undecodable payload shape, never a
// missing-required-field quarantine.
func decodeSBOMWarning(env facts.Envelope) (sbomv1.Warning, error) {
	warning, err := factschema.DecodeSBOMWarning(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.Warning{}, newFactDecodeError(factschema.FactKindSBOMWarning, err)
	}
	return warning, nil
}

// decodeAttestationStatement decodes one attestation.statement envelope into
// the typed sbomv1.Statement struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// statement_id field or is otherwise malformed. It is the single decode site
// for the attestation.statement kind on the reducer side.
func decodeAttestationStatement(env facts.Envelope) (sbomv1.Statement, error) {
	statement, err := factschema.DecodeAttestationStatement(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.Statement{}, newFactDecodeError(factschema.FactKindAttestationStatement, err)
	}
	return statement, nil
}

// decodeAttestationSignatureVerification decodes one
// attestation.signature_verification envelope into the typed
// sbomv1.SignatureVerification struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// statement_id field or is otherwise malformed. It is the single decode site
// for this kind on the reducer side.
func decodeAttestationSignatureVerification(env facts.Envelope) (sbomv1.SignatureVerification, error) {
	verification, err := factschema.DecodeAttestationSignatureVerification(factschemaEnvelope(env))
	if err != nil {
		return sbomv1.SignatureVerification{}, newFactDecodeError(factschema.FactKindAttestationSignatureVerification, err)
	}
	return verification, nil
}
