// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"strings"
)

// discoverCollectorInstanceSelectors fills the collector_kind and instance_id
// classes from the first collector status row that carries both, so the pair
// names one real instance. Reading them as two independent first-matches could
// pair an unregistered kind (no instance) with a later row's instance, and the
// failed-scope list answers a kind-mismatched instance with nothing. The source
// is optional: a caller that sees no instance_id leaves both classes
// undiscovered and the probe reports the missing selector class. Discovery
// uses the admin token because the probes that consume the pair run under
// admin auth, and a scoped caller gets instance_id redacted.
func discoverCollectorInstanceSelectors(client *http.Client, apiBaseURL, adminToken string, selectors map[string]string) {
	payload, err := fetchOptionalSelectorSource(client, apiBaseURL, adminToken, "/api/v0/status/collectors")
	if err != nil {
		return
	}
	kind, instance := findFirstKindInstancePair(payload)
	if kind == "" || instance == "" {
		return
	}
	if _, exists := selectors["collector_kind"]; !exists {
		selectors["collector_kind"] = kind
	}
	if _, exists := selectors["instance_id"]; !exists {
		selectors["instance_id"] = instance
	}
}

// findFirstKindInstancePair walks a decoded payload in document order and
// returns the collector_kind and instance_id of the first object holding both.
func findFirstKindInstancePair(value any) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		kind, _ := typed["collector_kind"].(string)
		instance, _ := typed["instance_id"].(string)
		if strings.TrimSpace(kind) != "" && strings.TrimSpace(instance) != "" {
			return kind, instance
		}
		for _, child := range typed {
			if k, i := findFirstKindInstancePair(child); k != "" {
				return k, i
			}
		}
	case []any:
		for _, child := range typed {
			if k, i := findFirstKindInstancePair(child); k != "" {
				return k, i
			}
		}
	}
	return "", ""
}
