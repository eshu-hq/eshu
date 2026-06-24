// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import "strings"

func normalizedBackendFilters(filters []DiscoveryBackendFilter) []DiscoveryBackendFilter {
	normalized := make([]DiscoveryBackendFilter, 0, len(filters))
	seen := map[DiscoveryBackendFilter]struct{}{}
	for _, filter := range filters {
		item := DiscoveryBackendFilter{
			TargetScopeID: strings.TrimSpace(filter.TargetScopeID),
			BackendKind:   BackendKind(strings.ToLower(strings.TrimSpace(string(filter.BackendKind)))),
			Bucket:        strings.TrimSpace(filter.Bucket),
			Key:           strings.Trim(strings.TrimSpace(filter.Key), "/"),
			Region:        strings.ToLower(strings.TrimSpace(filter.Region)),
		}
		if item.TargetScopeID == "" && item.BackendKind == "" && item.Bucket == "" && item.Key == "" && item.Region == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}
