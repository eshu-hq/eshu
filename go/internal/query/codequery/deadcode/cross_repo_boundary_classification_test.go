// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"reflect"
	"testing"
)

// TestCrossRepoDeadCodeBoundaryHoistKeepsClassification pins the buckets,
// reasons and hidden counts the repository-boundary fallback produces. The
// boundary evidence moved out of each row (#7129), but classification reads the
// in-memory evidence, not the row, so none of these answers may change. The
// expectations are the behavior before the hoist.
func TestCrossRepoDeadCodeBoundaryHoistKeepsClassification(t *testing.T) {
	t.Parallel()

	two := []map[string]any{boundaryRelationship("consumer-1"), boundaryRelationship("consumer-2")}
	tests := []struct {
		name          string
		relationships []map[string]any
		body          string
		allowed       []string
		wantBuckets   map[string][]string
		wantReasons   []string
		wantHidden    any
	}{
		{
			name:          "unscoped boundary keeps fallback rows unknown",
			relationships: two,
			body:          `{"repo_id":"repo-producer","limit":10}`,
			wantBuckets:   map[string][]string{"live_by_consumer": {"p-entity"}, "unknown": {"p-boundary-a", "p-boundary-b"}},
			wantReasons:   []string{"package_module_repo_needs_symbol_evidence"},
		},
		{
			name:          "consumer selector narrows the boundary",
			relationships: two,
			body:          `{"repo_id":"repo-producer","consumer_repo_ids":["consumer-1"],"limit":10}`,
			wantBuckets:   map[string][]string{"live_by_consumer": {"p-entity"}, "unknown": {"p-boundary-a", "p-boundary-b"}},
			wantReasons:   []string{"package_module_repo_needs_symbol_evidence"},
		},
		{
			name:          "ungranted boundary consumer is counted hidden",
			relationships: two,
			body:          `{"repo_id":"repo-producer","limit":10}`,
			allowed:       []string{"repo-producer", "consumer-1"},
			wantBuckets:   map[string][]string{"live_by_consumer": {"p-entity"}, "unknown": {"p-boundary-a", "p-boundary-b"}},
			wantReasons:   []string{"package_module_repo_needs_symbol_evidence", "permission_hidden_consumer"},
			wantHidden:    float64(1),
		},
		{
			name:        "no boundary leaves fallback rows dead",
			body:        `{"repo_id":"repo-producer","limit":10}`,
			wantBuckets: map[string][]string{"live_by_consumer": {"p-entity"}, "dead": {"p-boundary-a", "p-boundary-b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := postBoundaryHoistRequest(t, boundaryHoistStore(tt.relationships), tt.body, tt.allowed)
			buckets := data["candidate_buckets"].(map[string]any)
			for name, wantIDs := range tt.wantBuckets {
				var got []string
				for _, raw := range buckets[name].([]any) {
					row := raw.(map[string]any)
					got = append(got, row["entity_id"].(string))
					if name == "unknown" {
						if reasons := anyStrings(row["needs_evidence_reasons"]); !reflect.DeepEqual(reasons, tt.wantReasons) {
							t.Fatalf("%s reasons = %v, want %v", row["entity_id"], reasons, tt.wantReasons)
						}
						if row["hidden_consumer_evidence_count"] != tt.wantHidden {
							t.Fatalf("%s hidden count = %#v, want %#v", row["entity_id"], row["hidden_consumer_evidence_count"], tt.wantHidden)
						}
					}
				}
				if !reflect.DeepEqual(got, wantIDs) {
					t.Fatalf("bucket %s = %v, want %v", name, got, wantIDs)
				}
			}
			counts := data["bucket_counts"].(map[string]any)
			for name, wantIDs := range tt.wantBuckets {
				if counts[name] != float64(len(wantIDs)) {
					t.Fatalf("bucket_counts[%s] = %#v, want %d", name, counts[name], len(wantIDs))
				}
			}
		})
	}
}

func anyStrings(raw any) []string {
	var out []string
	for _, item := range raw.([]any) {
		out = append(out, item.(string))
	}
	return out
}
