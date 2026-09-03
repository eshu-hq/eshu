// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

// Content-entity facts carry some parser-derived fields at the payload top
// level and the same fields nested under "entity_metadata", depending on which
// collector shape produced the fact. The accessors here read the top level
// first and fall back to the nested map, so a caller does not have to know
// which producer emitted the envelope it is holding.

// EntityMetadataKey is the payload key the nested parser metadata map lives
// under on a content_entity fact. It is named once here because both accessors
// below and every caller reasoning about the fallback read the same map.
const EntityMetadataKey = "entity_metadata"

// SemanticPayloadMetadataString returns payload[key] when it is a real string,
// falling back to payload["entity_metadata"][key]. It returns "" when neither
// carries a string value.
func SemanticPayloadMetadataString(payload map[string]any, key string) string {
	if value := SemanticPayloadString(payload, key); value != "" {
		return value
	}
	return SemanticPayloadString(PayloadMap(payload, EntityMetadataKey), key)
}

// SemanticPayloadMetadataStringSlice returns the string slice at payload[key],
// falling back to payload["entity_metadata"][key]. A non-empty top-level slice
// wins outright: the fallback is only consulted when the top level yields
// nothing, so a producer that emits both does not get the two concatenated.
func SemanticPayloadMetadataStringSlice(payload map[string]any, key string) []string {
	if values := SemanticPayloadStringSlice(payload, key); len(values) > 0 {
		return values
	}
	return SemanticPayloadStringSlice(PayloadMap(payload, EntityMetadataKey), key)
}
