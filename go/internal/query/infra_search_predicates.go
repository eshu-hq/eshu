// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func infraSearchHasScope(values ...string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

func infraSearchProviderFilterPredicate(labels []string) string {
	if infraLabelsAreCloudOnly(labels) {
		return "n.source_system = $provider"
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return "(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))"
	}
	return "n.provider = $provider"
}

func infraSearchResourceServiceFilterPredicate(labels []string) string {
	if infraLabelsAreCloudOnly(labels) {
		return "n.service_kind = $resource_service"
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return "(n.resource_service = $resource_service OR n.service_kind = $resource_service)"
	}
	return "n.resource_service = $resource_service"
}

func infraSearchProviderFromRow(row map[string]any, labels []string) string {
	if provider := StringVal(row, "provider"); provider != "" {
		return provider
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return StringVal(row, "source_system")
	}
	return ""
}

func infraLabelsAreCloudOnly(labels []string) bool {
	return len(labels) == 1 && labels[0] == "CloudResource"
}

func infraLabelsInclude(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
