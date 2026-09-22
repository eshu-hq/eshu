// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// documentation fact family, which moved to [docs] in issue #6776 so the
// root directory drops back under the 40-file dirgate cap. Every entry is an
// alias or a thin forwarder with no behavior change: the value, the type
// identity, and the returned bytes are the same ones the root declared
// before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// semantic.go payload fields and schemaVersionFamilies wiring plus the
// collectors, reducers, projectors, and query surfaces that reach them as
// facts.X today. A later family move adds a stanza to this file and never
// creates a new compat_*.go, and each entry here is deleted once its last
// caller has moved to the docs package directly (see the importer-migration
// follow-up, #6950).

import "github.com/eshu-hq/eshu/go/internal/facts/docs"

// Stanza: acl.go (moved to docs/acl.go).
const (
	// SourceACLStateAllowed records that the source ACL was evaluated and permits
	// the observed read. Only a real ACL evaluation may assert this; a successful
	// read whose restrictions were not collected is partial. See
	// [docs.SourceACLStateAllowed].
	SourceACLStateAllowed = docs.SourceACLStateAllowed
	// SourceACLStateDenied records an observed permission-denied or 403 read. See
	// [docs.SourceACLStateDenied].
	SourceACLStateDenied = docs.SourceACLStateDenied
	// SourceACLStateMissing records that the source was not found, deleted, or
	// trashed at the origin. See [docs.SourceACLStateMissing].
	SourceACLStateMissing = docs.SourceACLStateMissing
	// SourceACLStatePartial records an incomplete or restricted ACL read that
	// must fail closed rather than be treated as allowed. See
	// [docs.SourceACLStatePartial].
	SourceACLStatePartial = docs.SourceACLStatePartial
	// SourceACLStateStale records a permitted but stale source revision. See
	// [docs.SourceACLStateStale].
	SourceACLStateStale = docs.SourceACLStateStale
)

// BoundedSourceACLState returns the bounded source-ACL-state carried on a
// document or source acl_summary so a collector can propagate it verbatim onto
// the derived documentation evidence facts (mention, claim, observation) for
// #2178. It returns the empty string when summary is nil or carries no bounded
// ACL claim, so an unobserved or non-bounded posture is omitted from the
// evidence fact rather than defaulted. It is factual propagation only: it
// copies the observed state verbatim, never upgrades a denied, partial,
// missing, or stale observation to allowed, and never synthesizes a value the
// source did not assert. See [docs.BoundedSourceACLState].
func BoundedSourceACLState(summary *DocumentationACLSummary) string {
	return docs.BoundedSourceACLState(summary)
}

// ValidSourceACLState reports whether value is one of the bounded source-ACL-
// state constants. The empty string is not valid here; callers that observe no
// ACL signal omit the field rather than store an empty value. See
// [docs.ValidSourceACLState].
func ValidSourceACLState(value string) bool {
	return docs.ValidSourceACLState(value)
}

// Stanza: documentation.go (moved to docs/documentation.go).
const (
	// DocumentationClaimAuthorityDocumentEvidence marks claims as evidence about
	// document text only. See [docs.ClaimAuthorityDocumentEvidence].
	DocumentationClaimAuthorityDocumentEvidence = docs.ClaimAuthorityDocumentEvidence
	// DocumentationClaimCandidateFactKind identifies one non-authoritative
	// documentation claim candidate. See [docs.ClaimCandidateFactKind].
	DocumentationClaimCandidateFactKind = docs.ClaimCandidateFactKind
	// DocumentationDocumentFactKind identifies one documentation document. See
	// [docs.DocumentFactKind].
	DocumentationDocumentFactKind = docs.DocumentFactKind
	// DocumentationEntityMentionFactKind identifies one entity mention in
	// documentation. See [docs.EntityMentionFactKind].
	DocumentationEntityMentionFactKind = docs.EntityMentionFactKind
	// DocumentationEvidencePacketFactKind identifies one immutable documentation
	// evidence packet. See [docs.EvidencePacketFactKind].
	DocumentationEvidencePacketFactKind = docs.EvidencePacketFactKind
	// DocumentationFactSchemaVersion is the first documentation fact schema for
	// documentation fact kinds that have not introduced kind-specific schema
	// versions. See [docs.FactSchemaVersion].
	DocumentationFactSchemaVersion = docs.FactSchemaVersion
	// DocumentationFindingFactKind identifies one read-only documentation truth
	// finding. See [docs.FindingFactKind].
	DocumentationFindingFactKind = docs.FindingFactKind
	// DocumentationLinkFactKind identifies one link observed in documentation.
	// See [docs.LinkFactKind].
	DocumentationLinkFactKind = docs.LinkFactKind
	// DocumentationMentionResolutionAmbiguous means the mention has multiple
	// candidate entities. See [docs.MentionResolutionAmbiguous].
	DocumentationMentionResolutionAmbiguous = docs.MentionResolutionAmbiguous
	// DocumentationMentionResolutionExact means the mention resolved to one
	// entity. See [docs.MentionResolutionExact].
	DocumentationMentionResolutionExact = docs.MentionResolutionExact
	// DocumentationMentionResolutionUnmatched means the mention did not resolve
	// to an entity. See [docs.MentionResolutionUnmatched].
	DocumentationMentionResolutionUnmatched = docs.MentionResolutionUnmatched
	// DocumentationSectionFactKind identifies one section in a document revision.
	// See [docs.SectionFactKind].
	DocumentationSectionFactKind = docs.SectionFactKind
	// DocumentationSectionFactSchemaVersion is the documentation section schema
	// version that adds source-native content fields for updater diffing. See
	// [docs.SectionFactSchemaVersion].
	DocumentationSectionFactSchemaVersion = docs.SectionFactSchemaVersion
	// DocumentationSourceFactKind identifies one documentation source. See
	// [docs.SourceFactKind].
	DocumentationSourceFactKind = docs.SourceFactKind
)

// DocumentationACLSummary records bounded access metadata reported by a
// documentation source. See [docs.ACLSummary].
type DocumentationACLSummary = docs.ACLSummary

// DocumentationClaimCandidatePayload describes a non-authoritative claim found
// in documentation. See [docs.ClaimCandidatePayload].
type DocumentationClaimCandidatePayload = docs.ClaimCandidatePayload

// DocumentationClaimCandidateStableID returns a stable ID for one
// documentation claim candidate. See [docs.ClaimCandidateStableID].
func DocumentationClaimCandidateStableID(payload DocumentationClaimCandidatePayload) string {
	return docs.ClaimCandidateStableID(payload)
}

// DocumentationDocumentPayload describes one source-neutral documentation
// document revision. See [docs.DocumentPayload].
type DocumentationDocumentPayload = docs.DocumentPayload

// DocumentationDocumentStableID returns a stable ID for a documentation
// document revision. See [docs.DocumentStableID].
func DocumentationDocumentStableID(payload DocumentationDocumentPayload) string {
	return docs.DocumentStableID(payload)
}

// DocumentationEntityMentionPayload describes one possible entity mention in
// documentation. See [docs.EntityMentionPayload].
type DocumentationEntityMentionPayload = docs.EntityMentionPayload

// DocumentationEntityMentionStableID returns a stable ID for one entity
// mention. See [docs.EntityMentionStableID].
func DocumentationEntityMentionStableID(payload DocumentationEntityMentionPayload) string {
	return docs.EntityMentionStableID(payload)
}

// DocumentationEvidencePacketStableID returns a stable ID for one evidence
// packet. See [docs.EvidencePacketStableID].
func DocumentationEvidencePacketStableID(packetID string, packetVersion string) string {
	return docs.EvidencePacketStableID(packetID, packetVersion)
}

// DocumentationEvidenceRef references evidence used by a documentation
// payload. See [docs.EvidenceRef].
type DocumentationEvidenceRef = docs.EvidenceRef

// DocumentationFactKinds returns every documentation fact kind owned by Eshu
// core. It is the single source for the documentation family so the core fact-
// kind registry and the schema-version registry cannot drift. See
// [docs.FactKinds].
func DocumentationFactKinds() []string {
	return docs.FactKinds()
}

// DocumentationFindingStableID returns a stable ID for one documentation
// finding. See [docs.FindingStableID].
func DocumentationFindingStableID(findingID string, findingVersion string) string {
	return docs.FindingStableID(findingID, findingVersion)
}

// DocumentationLinkPayload describes one link observed in a document section.
// See [docs.LinkPayload].
type DocumentationLinkPayload = docs.LinkPayload

// DocumentationLinkStableID returns a stable ID for one documentation link.
// See [docs.LinkStableID].
func DocumentationLinkStableID(payload DocumentationLinkPayload) string {
	return docs.LinkStableID(payload)
}

// DocumentationOwnerRef identifies an owner reference reported by a
// documentation source. See [docs.OwnerRef].
type DocumentationOwnerRef = docs.OwnerRef

// DocumentationSchemaVersion returns the schema version a core consumer
// supports for a documentation fact kind. The section kind carries its own
// version; the remaining documentation kinds share the base documentation
// schema version. See [docs.SchemaVersion].
func DocumentationSchemaVersion(factKind string) (string, bool) {
	return docs.SchemaVersion(factKind)
}

// DocumentationSectionPayload describes one bounded section in a document
// revision. See [docs.SectionPayload].
type DocumentationSectionPayload = docs.SectionPayload

// DocumentationSectionStableID returns a stable ID for a documentation section
// revision. See [docs.SectionStableID].
func DocumentationSectionStableID(payload DocumentationSectionPayload) string {
	return docs.SectionStableID(payload)
}

// DocumentationSourcePayload describes a documentation source such as
// Confluence or Git Markdown. See [docs.SourcePayload].
type DocumentationSourcePayload = docs.SourcePayload

// DocumentationSourceStableID returns a stable ID for a documentation source.
// See [docs.SourceStableID].
func DocumentationSourceStableID(payload DocumentationSourcePayload) string {
	return docs.SourceStableID(payload)
}

// Stanza: encode.go (moved to docs/encode.go).

// EncodeDocumentationClaimCandidate maps the internal documentation claim
// candidate identity payload to the SDK factschema encoder used for emitted
// fact payloads. See [docs.EncodeClaimCandidate].
func EncodeDocumentationClaimCandidate(payload DocumentationClaimCandidatePayload) (map[string]any, error) {
	return docs.EncodeClaimCandidate(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationDocument maps the internal documentation document
// identity payload to the SDK factschema encoder used for emitted fact
// payloads. See [docs.EncodeDocument].
func EncodeDocumentationDocument(payload DocumentationDocumentPayload) (map[string]any, error) {
	return docs.EncodeDocument(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationEntityMention maps the internal documentation entity
// mention identity payload to the SDK factschema encoder used for emitted fact
// payloads. See [docs.EncodeEntityMention].
func EncodeDocumentationEntityMention(payload DocumentationEntityMentionPayload) (map[string]any, error) {
	return docs.EncodeEntityMention(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationEvidencePacket maps the verifier's documentation evidence
// packet map through the SDK factschema encoder and preserves verifier-owned
// extension fields that the typed contract intentionally leaves open. See
// [docs.EncodeEvidencePacket].
func EncodeDocumentationEvidencePacket(payload map[string]any) (map[string]any, error) {
	return docs.EncodeEvidencePacket(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationFinding maps the verifier's documentation finding map
// through the SDK factschema encoder and preserves verifier-owned extension
// fields that the typed contract intentionally leaves open. See
// [docs.EncodeFinding].
func EncodeDocumentationFinding(payload map[string]any) (map[string]any, error) {
	return docs.EncodeFinding(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationLink maps the internal documentation link identity
// payload to the SDK factschema encoder used for emitted fact payloads. See
// [docs.EncodeLink].
func EncodeDocumentationLink(payload DocumentationLinkPayload) (map[string]any, error) {
	return docs.EncodeLink(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationSection maps the internal documentation section identity
// payload to the SDK factschema encoder used for emitted fact payloads. See
// [docs.EncodeSection].
func EncodeDocumentationSection(payload DocumentationSectionPayload) (map[string]any, error) {
	return docs.EncodeSection(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}

// EncodeDocumentationSource maps the internal documentation source identity
// payload to the SDK factschema encoder used for emitted fact payloads. See
// [docs.EncodeSource].
func EncodeDocumentationSource(payload DocumentationSourcePayload) (map[string]any, error) {
	return docs.EncodeSource(payload) //nolint:wrapcheck // passthrough compat alias: preserve the original error unchanged across the move (no added context).
}
