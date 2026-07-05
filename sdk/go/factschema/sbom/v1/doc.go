// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "sbom_attestation" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_sbom.go).
//
// Eight fact kinds live here, spanning two sub-families that share one
// reducer domain (sbom_attestation_attachment):
//
//   - SBOM: Document (sbom.document), Component (sbom.component),
//     DependencyRelationship (sbom.dependency_relationship), ExternalReference
//     (sbom.external_reference), Warning (sbom.warning).
//   - Attestation: Statement (attestation.statement), SignatureVerification
//     (attestation.signature_verification), SLSAProvenance
//     (attestation.slsa_provenance).
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers or slices
// carrying omitempty, so an absent value decodes to nil and stays distinct
// from an observed zero.
//
// Every struct here is FLAT — none carries an untyped Attributes
// pass-through — because, unlike the AWS/GCP cloud-inventory families, no
// sbom/attestation fact kind is a polymorphic multi-shape envelope: each kind
// has one fixed field set across both collector paths (sbomdocument and
// sbomruntime).
//
// DependencyRelationship, ExternalReference, and SLSAProvenance are
// TYPED-BUT-NOT-YET-CONSUMED: their fact kinds are loaded by
// sbom_attestation_attachment.go alongside their siblings, but no reducer or
// storage read path decodes their payload today (SLSAProvenance additionally
// has no collector emitter yet). They are typed anyway so their identity join
// key is already established for when a consumer is added, matching how the
// terraform_state family left candidate/provider_binding/warning
// typed-but-deferred.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
