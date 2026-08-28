// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestCodeRelationshipRequestUsesNeutralContract(t *testing.T) {
	t.Parallel()

	var got routecontract.Request
	got, handled, err := codeRelationshipRequest("analyze_code_relationships", routecontract.Arguments{
		"query_type":         "find_all_callers",
		"target":             "checkout",
		"repo_id":            "repo-1",
		"context":            " 7 ",
		"relationship_types": []string{"CALLS", "IMPORTS"},
		"limit":              int64(19),
		"offset":             float64(3),
		"token_budget":       1200,
		"cross_repo":         true,
		"min_confidence":     float32(0.75),
	})
	if err != nil {
		t.Fatalf("codeRelationshipRequest() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("codeRelationshipRequest() handled = false, want true")
	}

	want := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/code/relationships/story",
		Body: map[string]any{
			"target":             "checkout",
			"repo_id":            "repo-1",
			"direction":          "incoming",
			"relationship_type":  "CALLS",
			"relationship_types": []any{"CALLS", "IMPORTS"},
			"include_transitive": true,
			"max_depth":          7,
			"limit":              19,
			"offset":             3,
			"token_budget":       1200,
			"cross_repo":         true,
			"min_confidence":     0.75,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codeRelationshipRequest() = %#v, want %#v", got, want)
	}
}
