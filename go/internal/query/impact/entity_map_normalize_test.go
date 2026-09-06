// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestNormalizeEntityMapRowsUsesRawTraversalProjection(t *testing.T) {
	t.Parallel()

	rows := NormalizeEntityMapRows([]map[string]any{
		{
			"id":                "resource:db",
			"name":              "checkout-db",
			"repo_id":           "repo-checkout",
			"environment":       "prod",
			"entity_labels":     []any{"CloudResource"},
			"relationship_type": "USES",
		},
	}, EntityMapTraversalSpec{
		Direction:     "outgoing",
		Relationships: []string{"USES"},
		MinHops:       1,
		MaxHops:       1,
	}, EntityMapCandidate{})

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
		if got := querycontract.StringVal(row, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got, want := EntityMapResolveDepth(row, EntityMapTraversalSpec{MinHops: 1, MaxHops: 1}), 1; got != want {
		t.Fatalf("depth = %d, want %d", got, want)
	}
}
