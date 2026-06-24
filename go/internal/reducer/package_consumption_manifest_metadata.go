// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func packageManifestMetadataString(payload map[string]any, key string) string {
	if value := payloadStr(payload, key); value != "" {
		return value
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	return payloadStr(raw, key)
}

func packageManifestObservedVersion(payload map[string]any, packageManager string, lockfile bool) string {
	if !packageManifestCanObserveExactVersion(packageManager, lockfile) {
		return ""
	}
	for _, key := range []string{"resolved_version", "value"} {
		value := packageManifestMetadataString(payload, key)
		if version, ok := exactManifestDependencyVersion(value); ok {
			return version
		}
	}
	return ""
}

func packageManifestCanObserveExactVersion(packageManager string, lockfile bool) bool {
	switch packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(packageManager)) {
	case packageidentity.EcosystemCargo, packageidentity.EcosystemNuGet:
		return lockfile
	default:
		return true
	}
}

func packageManifestRequestedRange(payload map[string]any) string {
	return firstNonBlank(
		packageManifestMetadataString(payload, "requested_range"),
		packageManifestMetadataString(payload, "requested_version"),
		packageManifestMetadataString(payload, "value"),
	)
}

func packageManifestMetadataStrings(payload map[string]any, key string) []string {
	values := payloadOrderedStrings(payload, key)
	if len(values) > 0 {
		return values
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return nil
	}
	return payloadOrderedStrings(raw, key)
}

func payloadOrderedStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			text := strings.TrimSpace(payloadString(map[string]any{"value": value}, "value"))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

func packageManifestMetadataInt(payload map[string]any, key string) int {
	if value := packageManifestMetadataString(payload, key); value != "" {
		parsed, _ := strconv.Atoi(value)
		return parsed
	}
	return 0
}

func packageManifestMetadataBool(payload map[string]any, key string) *bool {
	raw := packageManifestMetadataString(payload, key)
	if raw == "" {
		return nil
	}
	value := strings.EqualFold(raw, "true")
	return &value
}

// packageManifestMetadataBoolValue returns the metadata boolean for key as a
// plain bool, defaulting to false when missing. It is used for flags whose
// absence and false value carry the same meaning, such as lockfile and
// source_ambiguous, where only true changes downstream behavior.
func packageManifestMetadataBoolValue(payload map[string]any, key string) bool {
	value := packageManifestMetadataBool(payload, key)
	return value != nil && *value
}
