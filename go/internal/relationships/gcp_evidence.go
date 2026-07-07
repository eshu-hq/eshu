// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	gcpRelationshipSupported = "supported"
	gcpRelationshipExtractor = "gcp-cloud-relationship"
)

func discoverGCPCloudRelationshipEvidence(
	envelope facts.Envelope,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	if envelope.IsTombstone || envelope.FactKind != facts.GCPCloudRelationshipFactKind {
		return nil
	}
	relationship, err := decodeGCPCloudRelationshipEnvelope(envelope)
	if err != nil {
		return nil
	}
	sourceName := strings.TrimSpace(relationship.SourceFullResourceName)
	targetName := strings.TrimSpace(relationship.TargetFullResourceName)
	relationshipType := strings.TrimSpace(relationship.RelationshipType)
	supportState := gcpRelationshipSupportState(relationship.SupportState)
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
		"source_asset_type":     gcpRelationshipOptionalString(relationship.SourceAssetType),
		"source_matched_alias":  sourceMatch.alias,
		"source_matched_value":  sourceName,
		"target_asset_type":     gcpRelationshipOptionalString(relationship.TargetAssetType),
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
		relationship, err := decodeGCPCloudRelationshipEnvelope(envelope)
		if err != nil {
			continue
		}
		sourceName := strings.TrimSpace(relationship.SourceFullResourceName)
		targetName := strings.TrimSpace(relationship.TargetFullResourceName)
		relationshipType := strings.TrimSpace(relationship.RelationshipType)
		if sourceName == "" || targetName == "" || relationshipType == "" {
			continue
		}
		if gcpRelationshipSupportState(relationship.SupportState) != gcpRelationshipSupported {
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
		relationship, err := decodeGCPCloudRelationshipEnvelope(envelope)
		if err != nil {
			continue
		}
		if gcpRelationshipSupportState(relationship.SupportState) != gcpRelationshipSupported {
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

func gcpRelationshipSupportState(value *string) string {
	return gcpRelationshipOptionalString(value)
}

func gcpRelationshipOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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
