// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestNormalizeEntityMapRowsUsesRawTraversalProjection(t *testing.T) {
	t.Parallel()

	rows := normalizeEntityMapRows([]map[string]any{
		{
			"id":                "resource:db",
			"name":              "checkout-db",
			"repo_id":           "repo-checkout",
			"environment":       "prod",
			"entity_labels":     []any{"CloudResource"},
			"relationship_type": "USES",
		},
	}, entityMapTraversalSpec{
		direction:     "outgoing",
		relationships: []string{"USES"},
		minHops:       1,
		maxHops:       1,
	}, entityMapCandidate{})

	if got, want := len(rows), 1; got != want {
		t.Fatalf("row count = %d, want %d", got, want)
	}
	row := rows[0]
	for key, want := range map[string]string{
		"entity_id":         "resource:db",
		"entity_name":       "checkout-db",
		"repo_id":           "repo-checkout",
		"environment":       "prod",
		"direction":         "outgoing",
		"relationship_type": "USES",
	} {
		if got := StringVal(row, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got, want := entityMapResolveDepth(row, entityMapTraversalSpec{minHops: 1, maxHops: 1}), 1; got != want {
		t.Fatalf("depth = %d, want %d", got, want)
	}
}
