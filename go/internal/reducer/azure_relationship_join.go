// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

const (
	azureRelationshipSupportSupported   = "supported"
	azureRelationshipSupportPartial     = "partial"
	azureRelationshipSupportUnsupported = "unsupported"
)

const (
	azureJoinModeARMResourceID = "arm_resource_id"
	azureJoinModeUnresolved    = "unresolved"
	azureJoinModePartial       = "partial"
	azureJoinModeUnsupported   = "unsupported"
	azureJoinModeInvalidType   = "invalid_type"
	azureJoinModeEmptyType     = "empty_type"
	azureJoinModeUnknownState  = "unknown_state"
	azureJoinModeSelfLoop      = "self_loop"
)

const (
	azureMetricRelationshipTypeEmpty   = "missing_relationship_type"
	azureMetricRelationshipTypeInvalid = "invalid_relationship_type"
)

const azureRelationshipTypeManagedBy = "managed_by"

type azureCloudResourceJoinIndex struct {
	byResourceID map[string]string
}

// buildAzureCloudResourceJoinIndex decodes each azure_cloud_resource fact
// through the contracts seam and indexes it by its normalized/ARM resource id
// for relationship endpoint resolution. A fact missing a required identity
// field is routed through partitionDecodeFailures into the returned
// quarantine list rather than silently joining under an empty-string key; any
// other decode error is returned fatally.
func buildAzureCloudResourceJoinIndex(envelopes []facts.Envelope) (azureCloudResourceJoinIndex, []quarantinedFact, error) {
	index := azureCloudResourceJoinIndex{byResourceID: make(map[string]string, len(envelopes))}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AzureCloudResourceFactKind || env.IsTombstone {
			continue
		}
		_, uid, resourceID, ok, err := azureCloudResourceNodeRow(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return azureCloudResourceJoinIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok || resourceID == "" {
			continue
		}
		if _, exists := index.byResourceID[resourceID]; !exists {
			index.byResourceID[resourceID] = uid
		}
	}
	return index, quarantined, nil
}

func (i azureCloudResourceJoinIndex) resolve(resourceID string) (string, bool) {
	if resourceID == "" {
		return "", false
	}
	uid, ok := i.byResourceID[resourceID]
	return uid, ok
}

type azureRelTypeMode struct {
	relationshipType string
	mode             string
}

type azureRelationshipEdgeTally struct {
	byRelTypeMode    map[azureRelTypeMode]int
	byMode           map[string]int
	unresolved       map[string]int
	unresolvedSource map[string]int
}

func newAzureRelationshipEdgeTally() azureRelationshipEdgeTally {
	return azureRelationshipEdgeTally{
		byRelTypeMode:    make(map[azureRelTypeMode]int),
		byMode:           make(map[string]int),
		unresolved:       make(map[string]int),
		unresolvedSource: make(map[string]int),
	}
}

func (t azureRelationshipEdgeTally) record(relationshipType, mode string) {
	t.byMode[mode]++
	t.byRelTypeMode[azureRelTypeMode{relationshipType: relationshipType, mode: mode}]++
}

func (t azureRelationshipEdgeTally) resolvedCount() int { return t.byMode[azureJoinModeARMResourceID] }

func (t azureRelationshipEdgeTally) skippedCount() int {
	total := 0
	for mode, count := range t.byMode {
		if mode != azureJoinModeARMResourceID {
			total += count
		}
	}
	return total
}

func azureRelationshipTypeValid(value string) bool {
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

func azureRelationshipTypeSupported(value string) bool {
	return value == azureRelationshipTypeManagedBy
}

// ExtractAzureRelationshipEdgeRows builds canonical Azure relationship edge
// rows by resolving both azure_cloud_relationship endpoints against the
// generation's materializable Azure CloudResource facts by exact normalized ARM
// resource id. It never derives nodes from relationship facts alone. Every
// resource and relationship fact decodes through the contracts seam; a fact
// missing a required identity field is quarantined into the returned list
// rather than joining or projecting under an empty-string identity segment,
// and any other decode error is returned fatally.
func ExtractAzureRelationshipEdgeRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ([]map[string]any, azureRelationshipEdgeTally, []quarantinedFact, error) {
	tally := newAzureRelationshipEdgeTally()
	if len(relationshipEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildAzureCloudResourceJoinIndex(resourceEnvelopes)
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
		if env.FactKind != facts.AzureCloudRelationshipFactKind || env.IsTombstone {
			continue
		}
		relationship, err := decodeAzureCloudRelationship(env)
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
			tally.record(azureMetricRelationshipTypeEmpty, azureJoinModeEmptyType)
			continue
		}
		targetType := derefString(relationship.TargetResourceType)
		if targetType == "" {
			targetType = "unknown"
		}
		if !azureRelationshipTypeValid(relationshipType) {
			tally.record(azureMetricRelationshipTypeInvalid, azureJoinModeInvalidType)
			continue
		}
		if !azureRelationshipTypeSupported(relationshipType) {
			tally.record(relationshipType, azureJoinModeUnsupported)
			continue
		}
		switch derefString(relationship.SupportState) {
		case azureRelationshipSupportUnsupported:
			tally.record(relationshipType, azureJoinModeUnsupported)
			continue
		case azureRelationshipSupportPartial:
			tally.record(relationshipType, azureJoinModePartial)
			continue
		case "", azureRelationshipSupportSupported:
		default:
			tally.record(relationshipType, azureJoinModeUnknownState)
			continue
		}

		sourceUID, sourceOK := index.resolve(azureRelationshipSourceID(relationship))
		if !sourceOK {
			tally.unresolvedSource[targetType]++
			tally.record(relationshipType, azureJoinModeUnresolved)
			continue
		}
		targetUID, targetOK := index.resolve(azureRelationshipTargetID(relationship))
		if !targetOK {
			tally.unresolved[targetType]++
			tally.record(relationshipType, azureJoinModeUnresolved)
			continue
		}
		if sourceUID == targetUID {
			tally.record(relationshipType, azureJoinModeSelfLoop)
			continue
		}
		key := edgeKey{source: sourceUID, relationshipType: relationshipType, target: targetUID}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		tally.record(relationshipType, azureJoinModeARMResourceID)
		rows = append(rows, map[string]any{
			"source_uid":        sourceUID,
			"target_uid":        targetUID,
			"relationship_type": relationshipType,
			"target_type":       targetType,
			"support_state":     azureRelationshipSupportSupported,
			"resolution_mode":   azureJoinModeARMResourceID,
		})
	}

	if len(rows) == 0 {
		return nil, tally, quarantined, nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["relationship_type"]) + ":" + anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["relationship_type"]) + ":" + anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally, quarantined, nil
}

// azureRelationshipSourceID returns the preferred join identity for a decoded
// relationship's source endpoint: SourceNormalizedResourceID when present,
// falling back to the required SourceARMResourceID otherwise.
func azureRelationshipSourceID(relationship azurev1.CloudRelationship) string {
	if normalized := derefString(relationship.SourceNormalizedResourceID); normalized != "" {
		return normalized
	}
	return relationship.SourceARMResourceID
}

// azureRelationshipTargetID returns the preferred join identity for a decoded
// relationship's target endpoint: TargetNormalizedResourceID when present,
// falling back to the required TargetARMResourceID otherwise.
func azureRelationshipTargetID(relationship azurev1.CloudRelationship) string {
	if normalized := derefString(relationship.TargetNormalizedResourceID); normalized != "" {
		return normalized
	}
	return relationship.TargetARMResourceID
}
