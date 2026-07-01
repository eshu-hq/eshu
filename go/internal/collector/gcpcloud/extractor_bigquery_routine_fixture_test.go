// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestBigQueryRoutineOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for BigQuery Routine through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes
// and the parent-dataset / remote-function-connection edges reach durable facts
// without any live GCP call, and that no routine definition body lands on a fact.
func TestBigQueryRoutineOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_bigquery_routine.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
	}

	gen := NewGeneration(attributesTestBoundary(), redact.Key{})
	if err := gen.AddPage(page.Resources); err != nil {
		t.Fatalf("add page: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("build generation: %v", err)
	}

	resourceCount := 0
	edges := map[string]int{}
	var enrichAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == bigQueryRoutineAssetName {
				enrichAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			edges[stringOrEmpty(env.Payload["relationship_type"])]++
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if enrichAttrs == nil {
		t.Fatalf("enrich routine carried no attributes")
	}
	if enrichAttrs["routine_type"] != "SCALAR_FUNCTION" {
		t.Errorf("enrich routine_type = %v, want SCALAR_FUNCTION", enrichAttrs["routine_type"])
	}
	if enrichAttrs["has_definition_body"] != true {
		t.Errorf("enrich has_definition_body = %v, want true", enrichAttrs["has_definition_body"])
	}
	for rel, want := range map[string]int{
		relationshipTypeRoutineInDataset:      2,
		relationshipTypeRoutineUsesConnection: 1,
	} {
		if edges[rel] != want {
			t.Errorf("%s edges = %d, want %d", rel, edges[rel], want)
		}
	}

	// No routine definition body may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"placeholderLogic", "definitionBody", "doWork"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked routine definition body token %q", token)
		}
	}
}
