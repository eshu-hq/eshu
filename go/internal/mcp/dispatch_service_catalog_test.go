// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestResolveRouteMapsServiceCatalogCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_service_catalog_correlations", map[string]any{
		"after_correlation_id": "catalog-correlation-1",
		"scope_id":             "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		"provider":             "backstage",
		"entity_ref":           "component:default/checkout",
		"repository_id":        "repo-checkout",
		"service_id":           "service-checkout",
		"workload_id":          "workload-checkout",
		"owner_ref":            "group:default/payments",
		"outcome":              "exact",
		"drift_status":         "matches",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/service-catalog/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id": "catalog-correlation-1",
		"scope_id":             "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		"provider":             "backstage",
		"entity_ref":           "component:default/checkout",
		"repository_id":        "repo-checkout",
		"service_id":           "service-checkout",
		"workload_id":          "workload-checkout",
		"owner_ref":            "group:default/payments",
		"outcome":              "exact",
		"drift_status":         "matches",
		"limit":                "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestServiceCatalogToolSchemaAdvertisesRepositorySelectors(t *testing.T) {
	t.Parallel()

	tools := serviceCatalogTools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("len(serviceCatalogTools()) = %d, want %d", got, want)
	}
	schema := tools[0].InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	repository := properties["repository_id"].(map[string]any)
	description := repository["description"].(string)
	for _, want := range []string{"Repository selector", "canonical ID", "name"} {
		if !strings.Contains(description, want) {
			t.Fatalf("repository_id description = %q, want %q", description, want)
		}
	}
	if strings.Contains(description, "remote URL") {
		t.Fatalf("repository_id description = %q, must not advertise unsupported remote URL selectors", description)
	}
}
