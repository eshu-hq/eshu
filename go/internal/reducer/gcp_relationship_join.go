// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GCP relationship support states, mirrored from the gcpcloud collector
// (gcp_cloud_relationship support_state). They classify how completely the
// provider described the relationship and drive whether an edge may materialize.
const (
	// gcpRelationshipSupportSupported is a complete provider relationship; the
	// only state eligible to materialize an edge.
	gcpRelationshipSupportSupported = "supported"
	// gcpRelationshipSupportPartial means the target is opaque or outside the
	// readable boundary (e.g. cross-project). The collector's contract says a
	// reducer MUST treat the target as unresolved, so no edge is written.
	gcpRelationshipSupportPartial = "partial"
	// gcpRelationshipSupportUnsupported means the relationship type or tier is
	// not fully supported; it is provenance only and never materializes an edge.
	gcpRelationshipSupportUnsupported = "unsupported"
)

// Join/resolution modes for the GCP relationship edge projection. GCP resource
// identity is the globally-unique Cloud Asset Inventory full resource name, so
// endpoint resolution is a single exact map lookup — there is no ARN/bare-id/
// anchor ambiguity like the AWS path. The modes below are the bounded, stable
// labels the completion log groups by.
const (
	// gcpJoinModeFullResourceName resolved both endpoints to materialized
	// CloudResource nodes by exact full resource name.
	gcpJoinModeFullResourceName = "full_resource_name"
	// gcpJoinModeUnresolved labels a supported relationship whose source or
	// target was not a materialized CloudResource node in this scope generation.
	gcpJoinModeUnresolved = "unresolved"
	// gcpJoinModePartial labels a relationship the provider marked partial; the
	// target is treated as unresolved per the collector contract.
	gcpJoinModePartial = "partial"
	// gcpJoinModeUnsupported labels a provenance-only relationship the provider
	// marked unsupported.
	gcpJoinModeUnsupported = "unsupported"
	// gcpJoinModeInvalidType labels a relationship whose provider relationship
	// type is not a safe Cypher token; it is skipped and counted rather than
	// failing the batch.
	gcpJoinModeInvalidType = "invalid_type"
	// gcpJoinModeEmptyType labels a relationship fact with no relationship type.
	// The collector rejects this at emission; the reducer counts it defensively
	// so a durable-store anomaly is visible rather than a silent drop.
	gcpJoinModeEmptyType = "empty_type"
	// gcpJoinModeUnknownState labels a relationship whose support_state is
	// outside the bounded set. The collector normalizes/rejects support_state at
	// emission, so this is a fail-closed guard at the reducer trust boundary: an
	// unknown state is skipped and counted, never materialized as an edge.
	gcpJoinModeUnknownState = "unknown_state"
)

const (
	gcpMetricRelationshipTypeEmpty   = "missing_relationship_type"
	gcpMetricRelationshipTypeInvalid = "invalid_relationship_type"
)

type gcpRelTypeMode struct {
	relationshipType string
	mode             string
}

// gcpCloudResourceJoinIndex resolves a GCP relationship endpoint identity (the
// CAI full resource name) to the uid of a materialized CloudResource node. It is
// built once per scope generation from the gcp_cloud_resource facts so target
// resolution is O(1) per edge with no per-edge graph round trip. It never
// fabricates a uid from a relationship fact alone: an endpoint resolves only if
// that resource was scanned in the same scope (the trust-boundary rule).
type gcpCloudResourceJoinIndex struct {
	byFullResourceName map[string]string
}

// buildGCPCloudResourceJoinIndex builds the bounded in-memory join index from
// the scope generation's gcp_cloud_resource fact envelopes, keyed by the
// globally-unique full resource name and reusing the same uid the node
// materialization committed. Each fact is decoded through the factschema seam
// (via gcpCloudResourceNodeRow); a payload missing a required identity field
// (full_resource_name, asset_type) is QUARANTINED per-fact via
// partitionDecodeFailures — that one fact is skipped and returned in the
// quarantined slice, while every valid resource is still indexed. A non-decode
// error is returned fatally. Mirrors buildCloudResourceJoinIndex
// (aws_relationship_join.go).
func buildGCPCloudResourceJoinIndex(envelopes []facts.Envelope) (gcpCloudResourceJoinIndex, []quarantinedFact, error) {
	index := gcpCloudResourceJoinIndex{
		byFullResourceName: make(map[string]string, len(envelopes)),
	}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		row, uid, ok, err := gcpCloudResourceNodeRow(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return gcpCloudResourceJoinIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok {
			// Mirrors node materialization: an incomplete identity is not a
			// materializable node, so it is not a join target either. This is a
			// present-but-empty value (a valid decode), distinct from an absent
			// required key, which quarantines above.
			continue
		}
		// row["resource_id"] is the decoded FullResourceName (gcpCloudResourceNodeRow
		// sets it from the typed struct, never a raw payload re-read), so this join
		// key is sourced from the same decode this loop already performed.
		fullResourceName := anyToString(row["resource_id"])
		if fullResourceName == "" {
			continue
		}
		// First writer wins; identity is the globally-unique full resource name,
		// so a later duplicate resolves to the same uid anyway.
		if _, exists := index.byFullResourceName[fullResourceName]; !exists {
			index.byFullResourceName[fullResourceName] = uid
		}
	}
	return index, quarantined, nil
}

func (i gcpCloudResourceJoinIndex) resolve(fullResourceName string) (string, bool) {
	if fullResourceName == "" {
		return "", false
	}
	uid, ok := i.byFullResourceName[fullResourceName]
	return uid, ok
}

// gcpRelationshipEdgeTally is the bounded accounting surface for the GCP edge
// projection. It serves the completion log: total counts per resolution mode and
// a per-target-type breakdown of unresolved relationships so an operator can
// answer "which GCP relationship target types are losing edges, and is it
// because the target resource was not scanned yet?"
type gcpRelationshipEdgeTally struct {
	// byRelTypeMode counts every relationship fact by (relationship_type,
	// join_mode) for the edge-projection counter. Valid provider relationship
	// types are safe Cypher tokens; malformed or missing types use bounded
	// sentinels rather than leaking raw provider strings into metric labels.
	byRelTypeMode map[gcpRelTypeMode]int
	// byMode counts every relationship fact by resolution mode (full_resource_name
	// for materialized edges, or unresolved/partial/unsupported/invalid_type for
	// the reasons an edge was not written).
	byMode map[string]int
	// unresolved counts supported relationships whose target did not resolve,
	// keyed by target_type for the completion log diagnostic.
	unresolved map[string]int
	// unresolvedSource counts supported relationships whose source did not
	// resolve, keyed by target_type (the relationship's own classification).
	unresolvedSource map[string]int
}

func newGCPRelationshipEdgeTally() gcpRelationshipEdgeTally {
	return gcpRelationshipEdgeTally{
		byRelTypeMode:    make(map[gcpRelTypeMode]int),
		byMode:           make(map[string]int),
		unresolved:       make(map[string]int),
		unresolvedSource: make(map[string]int),
	}
}

func (t gcpRelationshipEdgeTally) record(relationshipType, mode string) {
	t.byMode[mode]++
	t.byRelTypeMode[gcpRelTypeMode{relationshipType: relationshipType, mode: mode}]++
}

func (t gcpRelationshipEdgeTally) resolvedCount() int { return t.byMode[gcpJoinModeFullResourceName] }

func (t gcpRelationshipEdgeTally) skippedCount() int {
	total := 0
	for mode, count := range t.byMode {
		if mode != gcpJoinModeFullResourceName {
			total += count
		}
	}
	return total
}

// gcpRelationshipTypeValid reports whether a provider relationship type is a safe
// Cypher token ([A-Za-z0-9_], non-empty). The cypher writer fails closed on an
// unsafe token; the extraction step pre-validates so one odd provider string is
// skipped and counted rather than failing the whole batched write.
func gcpRelationshipTypeValid(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

// ExtractGCPRelationshipEdgeRows builds canonical GCP relationship edge rows by
// resolving each gcp_cloud_relationship fact's endpoints against an in-memory
// index built from the scope generation's gcp_cloud_resource facts. It honors
// the provider support_state contract: only supported relationships materialize,
// partial relationships treat the target as unresolved, and unsupported
// relationships are provenance only. It never fabricates a node: an endpoint
// that is not a materialized CloudResource in this scope is counted in the tally
// and produces no row (graceful degradation).
//
// Both the gcp_cloud_resource facts (via buildGCPCloudResourceJoinIndex) and
// each gcp_cloud_relationship fact are decoded through the factschema seam, so
// a payload missing a required field (source_full_resource_name,
// target_full_resource_name, relationship_type) is QUARANTINED per-fact via
// partitionDecodeFailures rather than resolving an edge against an
// empty-string identity: that one fact is skipped and returned in the
// quarantined slice, while every valid fact still projects. A non-decode error
// is returned fatally. Mirrors ExtractAWSRelationshipEdgeRows
// (aws_relationship_join.go).
//
// Returned rows are deduplicated by (source_uid, relationship_type, target_uid)
// and sorted deterministically so the batched write is stable across retries and
// reprojections.
func ExtractGCPRelationshipEdgeRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ([]map[string]any, gcpRelationshipEdgeTally, []quarantinedFact, error) {
	tally := newGCPRelationshipEdgeTally()
	if len(relationshipEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildGCPCloudResourceJoinIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}

	type edgeKey struct {
		source           string
		relationshipType string
		target           string
	}
	seen := make(map[edgeKey]struct{}, len(relationshipEnvelopes))
	rows := make([]map[string]any, 0, len(relationshipEnvelopes))

	for _, env := range relationshipEnvelopes {
		if env.FactKind != facts.GCPCloudRelationshipFactKind {
			continue
		}
		relationship, err := decodeGCPCloudRelationship(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}

		relationshipType := relationship.RelationshipType
		if relationshipType == "" {
			tally.record(gcpMetricRelationshipTypeEmpty, gcpJoinModeEmptyType)
			continue
		}
		targetType := derefString(relationship.TargetAssetType)
		if targetType == "" {
			targetType = "unknown"
		}

		if !gcpRelationshipTypeValid(relationshipType) {
			tally.record(gcpMetricRelationshipTypeInvalid, gcpJoinModeInvalidType)
			continue
		}

		switch derefString(relationship.SupportState) {
		case gcpRelationshipSupportUnsupported:
			tally.record(relationshipType, gcpJoinModeUnsupported)
			continue
		case gcpRelationshipSupportPartial:
			tally.record(relationshipType, gcpJoinModePartial)
			continue
		case "", gcpRelationshipSupportSupported:
			// Blank normalizes to supported (the collector's own default), so
			// proceed to endpoint resolution below.
		default:
			// Fail closed: an unknown state never materializes an edge.
			tally.record(relationshipType, gcpJoinModeUnknownState)
			continue
		}

		sourceUID, sourceOK := index.resolve(relationship.SourceFullResourceName)
		if !sourceOK {
			tally.unresolvedSource[targetType]++
			tally.record(relationshipType, gcpJoinModeUnresolved)
			continue
		}
		targetUID, targetOK := index.resolve(relationship.TargetFullResourceName)
		if !targetOK {
			tally.unresolved[targetType]++
			tally.record(relationshipType, gcpJoinModeUnresolved)
			continue
		}
		if sourceUID == targetUID {
			// A self-loop carries no relationship truth; skip without counting it
			// as unresolved (both endpoints did resolve).
			continue
		}

		key := edgeKey{source: sourceUID, relationshipType: relationshipType, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		tally.record(relationshipType, gcpJoinModeFullResourceName)
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": relationshipType,
			"target_type":       targetType,
			"support_state":     gcpRelationshipSupportSupported,
			"resolution_mode":   gcpJoinModeFullResourceName,
		})
	}

	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}

	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["relationship_type"]) + ":" +
			anyToString(rows[a]["source_uid"]) + "->" +
			anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["relationship_type"]) + ":" +
			anyToString(rows[b]["source_uid"]) + "->" +
			anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}
