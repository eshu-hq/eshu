// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// DecodeSBOMDocument decodes one sbom.document envelope into the typed
// sbomv1.Document struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// document_id field or is otherwise malformed. It is the single decode site
// for the sbom.document kind on the reducer side: sbomAttachmentIndex decodes
// through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent orphaned document.
func DecodeSBOMDocument(env facts.Envelope) (sbomv1.Document, error) {
	document, err := factschema.DecodeSBOMDocument(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.Document{}, factdecode.NewFactDecodeError(factschema.FactKindSBOMDocument, err)
	}
	return document, nil
}

// DecodeSBOMComponent decodes one sbom.component envelope into the typed
// sbomv1.Component struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// document_id field or is otherwise malformed. It is the single decode site
// for the WIRED sbom.component consumer (sbomAttachmentIndex); the
// supply_chain_impact matcher (supplyChainSBOMComponentFromEnvelope) still
// reads this kind raw, pending its own migration.
func DecodeSBOMComponent(env facts.Envelope) (sbomv1.Component, error) {
	component, err := factschema.DecodeSBOMComponent(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.Component{}, factdecode.NewFactDecodeError(factschema.FactKindSBOMComponent, err)
	}
	return component, nil
}

// DecodeSBOMDependencyRelationship decodes one sbom.dependency_relationship
// envelope into the typed sbomv1.DependencyRelationship struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required document_id field or is otherwise
// malformed. It is the single decode site for the WIRED
// sbom.dependency_relationship consumer (sbomAttachmentIndex).
func DecodeSBOMDependencyRelationship(env facts.Envelope) (sbomv1.DependencyRelationship, error) {
	relationship, err := factschema.DecodeSBOMDependencyRelationship(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.DependencyRelationship{}, factdecode.NewFactDecodeError(factschema.FactKindSBOMDependencyRelationship, err)
	}
	return relationship, nil
}

// DecodeSBOMExternalReference decodes one sbom.external_reference envelope
// into the typed sbomv1.ExternalReference struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing
// its required document_id field or is otherwise malformed. It is the single
// decode site for the WIRED sbom.external_reference consumer
// (sbomAttachmentIndex).
func DecodeSBOMExternalReference(env facts.Envelope) (sbomv1.ExternalReference, error) {
	reference, err := factschema.DecodeSBOMExternalReference(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.ExternalReference{}, factdecode.NewFactDecodeError(factschema.FactKindSBOMExternalReference, err)
	}
	return reference, nil
}

// DecodeSBOMWarning decodes one sbom.warning envelope into the typed
// sbomv1.Warning struct through the contracts seam. sbomv1.Warning has zero
// required fields (two collector paths emit mutually-exclusive identity
// keys — see sbom/v1/document.go), so this only returns an error for an
// unsupported schema major or an undecodable payload shape, never a
// missing-required-field quarantine.
func DecodeSBOMWarning(env facts.Envelope) (sbomv1.Warning, error) {
	warning, err := factschema.DecodeSBOMWarning(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.Warning{}, factdecode.NewFactDecodeError(factschema.FactKindSBOMWarning, err)
	}
	return warning, nil
}

// DecodeAttestationStatement decodes one attestation.statement envelope into
// the typed sbomv1.Statement struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// statement_id field or is otherwise malformed. It is the single decode site
// for the attestation.statement kind on the reducer side.
func DecodeAttestationStatement(env facts.Envelope) (sbomv1.Statement, error) {
	statement, err := factschema.DecodeAttestationStatement(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.Statement{}, factdecode.NewFactDecodeError(factschema.FactKindAttestationStatement, err)
	}
	return statement, nil
}

// DecodeAttestationSignatureVerification decodes one
// attestation.signature_verification envelope into the typed
// sbomv1.SignatureVerification struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing its required
// statement_id field or is otherwise malformed. It is the single decode site
// for this kind on the reducer side.
func DecodeAttestationSignatureVerification(env facts.Envelope) (sbomv1.SignatureVerification, error) {
	verification, err := factschema.DecodeAttestationSignatureVerification(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.SignatureVerification{}, factdecode.NewFactDecodeError(factschema.FactKindAttestationSignatureVerification, err)
	}
	return verification, nil
}

// DecodeAttestationSLSAProvenance decodes one attestation.slsa_provenance
// envelope into the typed sbomv1.SLSAProvenance struct through the contracts
// seam, returning a self-classifying *factDecodeError when the payload is
// missing its required statement_id field or is otherwise malformed. It is
// the single decode site for this kind on the reducer side.
func DecodeAttestationSLSAProvenance(env facts.Envelope) (sbomv1.SLSAProvenance, error) {
	provenance, err := factschema.DecodeAttestationSLSAProvenance(FactschemaEnvelope(env))
	if err != nil {
		return sbomv1.SLSAProvenance{}, factdecode.NewFactDecodeError(factschema.FactKindAttestationSLSAProvenance, err)
	}
	return provenance, nil
}
