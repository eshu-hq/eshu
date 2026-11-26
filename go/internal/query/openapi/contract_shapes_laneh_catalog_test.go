// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneHCatalog pins the Lane H gap-3 fix: the shared
// CatalogWorkload component documents the conditional `tier`, `category`,
// and `domain` keys stamped by
// repository.Handler.enrichCatalogWorkloadsFromCorrelations when a
// correlated service-catalog manifest exists for the workload's repository.
// Both workloads[] and services[] reference this component (services rows
// are the kind=="service" subset via serviceCatalogRows), so one component
// fix covers both collections.
//
// `language` is deliberately NOT in the schema: catalogWorkload declares
// `json:"language,omitempty"` but no constructor and no enrichment path
// ever stamps it (enrich sets Tier/Category/Domain only), so the key never
// appears on the wire and documenting it would mislead console mocks.
func TestContractShapesLaneHCatalog(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec missing components")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("components missing schemas")
	}
	workload, ok := schemas["CatalogWorkload"].(map[string]any)
	if !ok {
		t.Fatal("schemas missing CatalogWorkload")
	}
	props, ok := workload["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Fatal("CatalogWorkload has no properties")
	}

	for _, key := range []string{"tier", "category", "domain"} {
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("CatalogWorkload missing conditional property %q", key)
			continue
		}
		desc, _ := node["description"].(string)
		if !strings.Contains(desc, "enrichCatalogWorkloadsFromCorrelations") {
			t.Errorf("CatalogWorkload.%s description does not name its enrichCatalogWorkloadsFromCorrelations owner", key)
		}
		if !strings.Contains(desc, "absent") {
			t.Errorf("CatalogWorkload.%s description carries no presence note (want \"absent when no correlated manifest\")", key)
		}
	}
	if _, ok := props["language"]; ok {
		t.Errorf("CatalogWorkload documents \"language\", but no code path ever stamps catalogWorkload.Language so the key never appears on the wire")
	}
}
