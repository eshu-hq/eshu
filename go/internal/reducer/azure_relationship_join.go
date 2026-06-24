// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

func buildAzureCloudResourceJoinIndex(envelopes []facts.Envelope) azureCloudResourceJoinIndex {
	index := azureCloudResourceJoinIndex{byResourceID: make(map[string]string, len(envelopes))}
	for _, env := range envelopes {
		if env.FactKind != facts.AzureCloudResourceFactKind || env.IsTombstone {
			continue
		}
		_, uid, ok := azureCloudResourceNodeRow(env)
		if !ok {
			continue
		}
		resourceID := azureNormalizedResourceID(env.Payload)
		if resourceID == "" {
			continue
		}
		if _, exists := index.byResourceID[resourceID]; !exists {
			index.byResourceID[resourceID] = uid
		}
	}
	return index
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
// resource id. It never derives nodes from relationship facts alone.
func ExtractAzureRelationshipEdgeRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ([]map[string]any, azureRelationshipEdgeTally) {
	tally := newAzureRelationshipEdgeTally()
	if len(relationshipEnvelopes) == 0 {
		return nil, tally
	}

	index := buildAzureCloudResourceJoinIndex(resourceEnvelopes)
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
		relationshipType := payloadString(env.Payload, "relationship_type")
		if relationshipType == "" {
			tally.record(azureMetricRelationshipTypeEmpty, azureJoinModeEmptyType)
			continue
		}
		targetType := payloadString(env.Payload, "target_resource_type")
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
		switch payloadString(env.Payload, "support_state") {
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

		sourceUID, sourceOK := index.resolve(azureRelationshipSourceID(env.Payload))
		if !sourceOK {
			tally.unresolvedSource[targetType]++
			tally.record(relationshipType, azureJoinModeUnresolved)
			continue
		}
		targetUID, targetOK := index.resolve(azureRelationshipTargetID(env.Payload))
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
		return nil, tally
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["relationship_type"]) + ":" + anyToString(rows[a]["source_uid"]) + "->" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["relationship_type"]) + ":" + anyToString(rows[b]["source_uid"]) + "->" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows, tally
}

func azureRelationshipSourceID(payload map[string]any) string {
	if normalized := payloadString(payload, "source_normalized_resource_id"); normalized != "" {
		return normalized
	}
	return payloadString(payload, "source_arm_resource_id")
}

func azureRelationshipTargetID(payload map[string]any) string {
	if normalized := payloadString(payload, "target_normalized_resource_id"); normalized != "" {
		return normalized
	}
	return payloadString(payload, "target_arm_resource_id")
}
