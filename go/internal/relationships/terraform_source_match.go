// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "strings"

func isPrivateTerraformRegistryModuleSource(source string) bool {
	_, ok := privateTerraformRegistryProvider(source)
	return ok
}

func privateTerraformRegistryProvider(source string) (string, bool) {
	normalized := normalizeTerraformModuleSource(source)
	if normalized == "" || strings.HasPrefix(normalized, "tfr:///") {
		return "", false
	}

	segments := strings.Split(normalized, "/")
	if len(segments) < 4 {
		return "", false
	}
	if !strings.Contains(segments[0], ".") {
		return "", false
	}

	moduleName := strings.TrimSpace(segments[len(segments)-2])
	provider := strings.TrimSpace(segments[len(segments)-1])
	if moduleName == "" || provider == "" {
		return "", false
	}

	return provider, true
}

func normalizeTerraformModuleSource(source string) string {
	normalized := strings.ToLower(strings.TrimSpace(source))
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, "?"); idx >= 0 {
		normalized = normalized[:idx]
	}
	if idx := strings.Index(normalized, "//"); idx > 0 {
		normalized = normalized[:idx]
	}
	return strings.TrimSpace(normalized)
}
