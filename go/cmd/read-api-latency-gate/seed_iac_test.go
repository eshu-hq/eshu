// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

func TestBuildIaCFactsProducesRequestedCount(t *testing.T) {
	facts := BuildIaCFacts("scope-1", "gen-1", 1000)
	if len(facts) != 1000 {
		t.Fatalf("len(facts) = %d, want 1000", len(facts))
	}
}

func TestBuildIaCFactsAreUniquelyIdentified(t *testing.T) {
	facts := BuildIaCFacts("scope-1", "gen-1", 500)
	seen := make(map[string]bool, len(facts))
	for _, f := range facts {
		if seen[f.EntityID] {
			t.Fatalf("duplicate entity_id %q — currentInventoryCTE's DISTINCT ON (payload->>'entity_id') would silently collapse these", f.EntityID)
		}
		seen[f.EntityID] = true
		if seen[f.FactID] {
			t.Fatalf("duplicate fact_id %q", f.FactID)
		}
		seen[f.FactID] = true
	}
}

func TestBuildIaCFactsUsesTerraformEntityTypes(t *testing.T) {
	facts := BuildIaCFacts("scope-1", "gen-1", 500)
	wantTypes := map[string]bool{"TerraformResource": false, "TerraformModule": false, "TerraformDataSource": false}
	for _, f := range facts {
		if _, ok := wantTypes[f.EntityType]; !ok {
			t.Fatalf("entity_type %q is not one of the three currentInventoryCTE filters on (TerraformResource, TerraformModule, TerraformDataSource)", f.EntityType)
		}
		wantTypes[f.EntityType] = true
	}
	for entityType, seen := range wantTypes {
		if !seen {
			t.Errorf("no fact used entity_type %q", entityType)
		}
	}
}

func TestBuildIaCFactsPayloadSizeMix(t *testing.T) {
	// #6793 root cause per the drive's diagnosis: currentInventoryCTE pays a
	// jsonb detoast cost because most payloads are small (~1KB) but a
	// minority are large enough (~15KB) to be TOASTed out-of-line. A
	// fixed-size payload would not reproduce that cost shape.
	facts := BuildIaCFacts("scope-1", "gen-1", 1000)
	var small, large int
	for _, f := range facts {
		payload := iacFactPayload(f)
		size := len(mustMarshalJSON(t, payload))
		switch {
		case size < 4096:
			small++
		case size > 8192:
			large++
		}
	}
	if small == 0 {
		t.Errorf("expected some small (~1KB) payloads, found none")
	}
	if large == 0 {
		t.Errorf("expected some large (~15KB) payloads, found none")
	}
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
