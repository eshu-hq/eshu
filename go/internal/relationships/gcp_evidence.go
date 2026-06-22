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
	sourceName := strings.TrimSpace(payloadString(envelope.Payload, "source_full_resource_name"))
	targetName := strings.TrimSpace(payloadString(envelope.Payload, "target_full_resource_name"))
	relationshipType := strings.TrimSpace(payloadString(envelope.Payload, "relationship_type"))
	supportState := gcpRelationshipSupportState(envelope)
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
		"source_asset_type":     strings.TrimSpace(payloadString(envelope.Payload, "source_asset_type")),
		"source_matched_alias":  sourceMatch.alias,
		"source_matched_value":  sourceName,
		"target_asset_type":     strings.TrimSpace(payloadString(envelope.Payload, "target_asset_type")),
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

func gcpRelationshipSupportState(envelope facts.Envelope) string {
	return strings.TrimSpace(payloadString(envelope.Payload, "support_state"))
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
