// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

const (
	gcpRelationshipSupported = "supported"
	gcpRelationshipExtractor = "gcp-cloud-relationship"
)

// gcpRelationshipPersistedVersionlessSchemaVersion is the sentinel the Postgres
// persist layer stamps for a fact whose collector emitted no SchemaVersion
// (emptyToDefault(envelope.SchemaVersion, "0.0.0") in
// go/internal/storage/postgres/facts.go). A gcp_cloud_relationship fact loaded
// back for evidence discovery therefore carries this value rather than the empty
// string, so decodeGCPCloudRelationship normalizes it (and the empty string an
// in-memory fact carries before persistence) to the family's real major-1 schema
// version. It is never a real schema version any collector emits. Mirrors the
// reducer's persistedVersionlessSchemaVersion
// (go/internal/reducer/factschema_decode.go).
const gcpRelationshipPersistedVersionlessSchemaVersion = "0.0.0"

// decodeGCPCloudRelationship decodes a gcp_cloud_relationship envelope's payload
// through the Contract System v1 typed seam (factschema.DecodeGCPCloudRelationship)
// instead of reading raw payload keys. It returns the typed gcpv1.Relationship and
// true when the payload decodes, or the zero struct and false on a classified
// decode error — a missing required identity field (source_full_resource_name,
// target_full_resource_name, relationship_type) or an unsupported schema major.
//
// On a decode error the caller MUST produce no evidence. This mirrors the reducer's
// decodeGCPCloudRelationship contract (go/internal/reducer/factschema_decode.go): a
// malformed payload is never read as a zero-value/empty-string identity. The
// relationships package holds no queue or graph handle and so cannot itself
// dead-letter (see AGENTS.md "No graph writes"); returning false so the extractor
// emits no evidence is its correct, contract-aligned response — the package's own
// "produce no evidence rather than a speculative match" invariant — while the
// authoritative input_invalid dead-letter is still emitted later when the reducer
// decodes the same fact for its own domain.
//
// A version-less envelope (empty SchemaVersion for an in-memory fact, or the
// persist-layer "0.0.0" sentinel for a fact loaded from Postgres) is normalized to
// the family's real major-1 schema version before decode, so a version-less fact
// still decodes rather than dead-lettering as an unsupported major, matching the
// reducer's factschemaEnvelope normalization and the raw read's prior
// version-agnostic behavior on the corpus.
//
// This extractor reads only the named identity fields (SourceFullResourceName,
// TargetFullResourceName, RelationshipType) and the named optional pointers
// (SourceAssetType, TargetAssetType, SupportState); it never reads
// Relationship.Attributes. It therefore decodes with
// factschema.WithoutAttributesRemainder() (issue #4865), which skips rebuilding
// the discarded Attributes remainder map — every named field decodes
// identically to a default decode, and the unread Attributes field is left nil.
// The reducer's own decode site (go/internal/reducer/factschema_decode.go) does
// consume Attributes and so keeps the default (full-remainder) decode.
func decodeGCPCloudRelationship(envelope facts.Envelope) (gcpv1.Relationship, bool) {
	schemaVersion := envelope.SchemaVersion
	if schemaVersion == "" || schemaVersion == gcpRelationshipPersistedVersionlessSchemaVersion {
		schemaVersion = facts.GCPCloudRelationshipSchemaVersion
	}
	relationship, err := factschema.DecodeGCPCloudRelationship(factschema.Envelope{
		FactKind:      envelope.FactKind,
		SchemaVersion: schemaVersion,
		Payload:       envelope.Payload,
	}, factschema.WithoutAttributesRemainder())
	if err != nil {
		return gcpv1.Relationship{}, false
	}
	return relationship, true
}

// trimDerefString returns the trimmed dereferenced value of an optional payload
// string pointer, or "" when the pointer is nil. It maps the gcpv1.Relationship
// optional pointer fields (SourceAssetType, TargetAssetType, SupportState) back to
// the trimmed-string shape the extractor previously obtained from
// strings.TrimSpace(payloadString(...)): an absent optional key decoded to nil
// yields "" exactly as an absent map key did.
func trimDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func discoverGCPCloudRelationshipEvidence(
	envelope facts.Envelope,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	if envelope.IsTombstone || envelope.FactKind != facts.GCPCloudRelationshipFactKind {
		return nil
	}
	relationship, ok := decodeGCPCloudRelationship(envelope)
	if !ok {
		return nil
	}
	sourceName := strings.TrimSpace(relationship.SourceFullResourceName)
	targetName := strings.TrimSpace(relationship.TargetFullResourceName)
	relationshipType := strings.TrimSpace(relationship.RelationshipType)
	supportState := gcpRelationshipSupportState(relationship)
	if sourceName == "" || targetName == "" || relationshipType == "" {
		return nil
	}
	if supportState != gcpRelationshipSupported {
		return nil
	}

	sourceMatch, ok := uniqueGCPResourceCatalogMatch(sourceName, "", matcher)
	if !ok {
		return nil
	}
	targetMatch, ok := uniqueGCPResourceCatalogMatch(targetName, sourceMatch.entry.RepoID, matcher)
	if !ok || targetMatch.entry.RepoID == sourceMatch.entry.RepoID {
		return nil
	}

	details := map[string]any{
		"gcp_fact_kind":         facts.GCPCloudRelationshipFactKind,
		"gcp_relationship_type": relationshipType,
		"gcp_support_state":     supportState,
		"source_asset_type":     trimDerefString(relationship.SourceAssetType),
		"source_matched_alias":  sourceMatch.alias,
		"source_matched_value":  sourceName,
		"target_asset_type":     trimDerefString(relationship.TargetAssetType),
	}
	if envelope.StableFactKey != "" {
		details["source_fact_key"] = envelope.StableFactKey
	}

	return matchCatalog(
		sourceMatch.entry.RepoID,
		targetName,
		gcpRelationshipSourcePath(envelope),
		EvidenceKindGCPCloudRelationship,
		RelDependsOn,
		DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindGCPCloudRelationship),
		"GCP provider relationship links two catalog-matched resource identities",
		gcpRelationshipExtractor,
		matcher,
		seen,
		details,
	)
}

// GCPRelationshipRepoLink names the source and target repository identities a
// supported gcp_cloud_relationship fact resolves to against a catalog. It mirrors
// the source-match-then-target-match resolution discoverGCPCloudRelationshipEvidence
// performs, so callers that must reason about which repos a GCP edge connects
// (for example, scope-bounded backfill catalog construction in issue #3500) get
// exactly the same repo identities the evidence discovery would emit.
type GCPRelationshipRepoLink struct {
	// SourceRepoID is the repository the source resource resolved to.
	SourceRepoID string
	// TargetRepoID is the repository the target resource resolved to.
	TargetRepoID string
}

// ResolveGCPRelationshipRepoLinks returns the source/target repository links for
// every supported, catalog-matched gcp_cloud_relationship fact in envelopes. It
// applies the same support-state filter and source-match-then-target-match
// ordering as evidence discovery, so a caller can learn which already-onboarded
// source repos a GCP edge connects to a target without re-deriving the matcher
// rules. Facts that are tombstoned, unsupported, malformed, or that do not yield
// a unique source-then-target catalog match (including self-edges) are skipped.
func ResolveGCPRelationshipRepoLinks(
	envelopes []facts.Envelope,
	catalog []CatalogEntry,
) []GCPRelationshipRepoLink {
	if len(envelopes) == 0 || len(catalog) == 0 {
		return nil
	}
	// Defer the O(catalog) matcher build until we know a supported GCP
	// relationship fact exists. The common per-commit case has no GCP
	// relationship facts, so this keeps the scope-bounded backfill (issue #3500)
	// from paying a fleet-sized matcher build when there is nothing to resolve.
	if !hasSupportedGCPRelationshipFact(envelopes) {
		return nil
	}
	matcher := newCatalogMatcher(catalog)
	links := make([]GCPRelationshipRepoLink, 0)
	for i := range envelopes {
		envelope := envelopes[i]
		if envelope.IsTombstone || envelope.FactKind != facts.GCPCloudRelationshipFactKind {
			continue
		}
		relationship, ok := decodeGCPCloudRelationship(envelope)
		if !ok {
			continue
		}
		sourceName := strings.TrimSpace(relationship.SourceFullResourceName)
		targetName := strings.TrimSpace(relationship.TargetFullResourceName)
		relationshipType := strings.TrimSpace(relationship.RelationshipType)
		if sourceName == "" || targetName == "" || relationshipType == "" {
			continue
		}
		if gcpRelationshipSupportState(relationship) != gcpRelationshipSupported {
			continue
		}
		sourceMatch, ok := uniqueGCPResourceCatalogMatch(sourceName, "", matcher)
		if !ok {
			continue
		}
		targetMatch, ok := uniqueGCPResourceCatalogMatch(targetName, sourceMatch.entry.RepoID, matcher)
		if !ok || targetMatch.entry.RepoID == sourceMatch.entry.RepoID {
			continue
		}
		links = append(links, GCPRelationshipRepoLink{
			SourceRepoID: sourceMatch.entry.RepoID,
			TargetRepoID: targetMatch.entry.RepoID,
		})
	}
	return links
}

// hasSupportedGCPRelationshipFact reports whether envelopes contain at least one
// live, supported gcp_cloud_relationship fact with both resource names and a
// relationship type. It is the cheap O(envelopes) guard that lets callers skip
// the O(catalog) matcher build when no GCP edge could resolve.
func hasSupportedGCPRelationshipFact(envelopes []facts.Envelope) bool {
	for i := range envelopes {
		envelope := envelopes[i]
		if envelope.IsTombstone || envelope.FactKind != facts.GCPCloudRelationshipFactKind {
			continue
		}
		relationship, ok := decodeGCPCloudRelationship(envelope)
		if !ok {
			continue
		}
		if gcpRelationshipSupportState(relationship) != gcpRelationshipSupported {
			continue
		}
		if strings.TrimSpace(relationship.SourceFullResourceName) == "" ||
			strings.TrimSpace(relationship.TargetFullResourceName) == "" ||
			strings.TrimSpace(relationship.RelationshipType) == "" {
			continue
		}
		return true
	}
	return false
}

func gcpRelationshipSupportState(relationship gcpv1.Relationship) string {
	return trimDerefString(relationship.SupportState)
}

func uniqueGCPResourceCatalogMatch(
	candidate string,
	sourceRepoID string,
	matcher *catalogMatcher,
) (catalogMatch, bool) {
	matches := matcher.match(candidate, sourceRepoID)
	if len(matches) != 1 {
		return catalogMatch{}, false
	}
	return matches[0], true
}

func gcpRelationshipSourcePath(envelope facts.Envelope) string {
	if sourceURI := strings.TrimSpace(envelope.SourceRef.SourceURI); sourceURI != "" {
		return sourceURI
	}
	if recordID := strings.TrimSpace(envelope.SourceRef.SourceRecordID); recordID != "" {
		return "gcp://relationship/" + recordID
	}
	if stableKey := strings.TrimSpace(envelope.StableFactKey); stableKey != "" {
		return "gcp://relationship/" + stableKey
	}
	return "gcp://relationship"
}
