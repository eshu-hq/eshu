// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "documentation" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_documentation.go).
//
// Eight fact kinds live here, all owned by go/internal/reducer's
// documentation_materialization domain and its documentation_evidence
// projection hook:
//
//   - Source (documentation_source), Document (documentation_document),
//     Section (documentation_section), Link (documentation_link) describe
//     the source-neutral document model a documentation collector observes.
//   - EntityMention (documentation_entity_mention) and ClaimCandidate
//     (documentation_claim_candidate) describe what the collector observed
//     in that model: a possible reference to a code/infra entity, or a
//     non-authoritative claim about one.
//   - Finding (documentation_finding) and EvidencePacket
//     (documentation_evidence_packet) are emitted by go/internal/doctruth
//     (the verifier), not a documentation collector, and record a read-only
//     verification outcome plus its immutable evidence packet.
//
// Only two kinds have a real reducer decode site today:
// documentation_entity_mention (ExtractDocumentationEdgeRows,
// go/internal/reducer/documentation_edge_materialization.go) and
// documentation_document (buildDocumentationDeltaScope,
// go/internal/reducer/documentation_edge_delta_scope.go). The remaining six
// are TYPED-BUT-NOT-YET-CONSUMED: source/section/link/claim_candidate have
// no reducer or storage-loader field-level read at all (the query read
// model and go/internal/storage/postgres filter on them only by fact_kind
// column or through JSONB containment, never a decoded field), and
// finding/evidence_packet are read only by the query layer's raw
// fact_records.payload->>'field' SQL, which is out of scope for this
// migration (see the reducer AGENTS.md's documentation-family caveat). They
// are typed anyway so their identity join key is already established for a
// future consumer, matching how the sbom_attestation family left
// DependencyRelationship/ExternalReference/SLSAProvenance typed-but-deferred.
//
// documentation_section carries its OWN schema version
// (facts.DocumentationSectionFactSchemaVersion, "1.1.0") distinct from every
// other kind in this family (facts.DocumentationFactSchemaVersion, "1.0.0"):
// it added source-native content fields for updater diffing after the base
// family was first defined. The parent package's decode seam
// (decode_documentation.go) dispatches Section through its own schema-major
// path, never conflating it with the shared 1.0.0 kinds.
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming
// the field, never a zero-value struct. Optional fields are pointers or
// slices carrying omitempty, so an absent value decodes to nil and stays
// distinct from an observed zero.
//
// Every struct here is FLAT — none carries an untyped Attributes
// pass-through — because no documentation fact kind is a polymorphic
// multi-shape envelope: each kind has one fixed field set across the
// documentation collector paths.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
