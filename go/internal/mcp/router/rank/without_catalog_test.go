// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestNewWithoutCatalogRanksWithNilEntries proves the catalog-free build
// ranks at Rank time without catalog entries: candidates come back with the
// conservative CostHigh default, the output matches New(tools, nil), and the
// index is reused with no rebuild (#9521 blocker 3: the extracted eshu-mcp
// server cannot import internal/ask/catalog, so it mirrors this
// constructor against its own registry types).
func TestNewWithoutCatalogRanksWithNilEntries(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_one", "Handles zebra processing."),
		def("alpha_two", "Handles other processing."),
	}
	q := router.Query{Intent: "zebra", Limit: 5}

	r := NewWithoutCatalog(tools)
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after NewWithoutCatalog = %d, want 1", got)
	}
	got, err := r.Rank(context.Background(), q, tools, nil, allowAll)
	if err != nil {
		t.Fatalf("Rank with nil entries: %v", err)
	}
	if len(got) == 0 || got[0].Name != "alpha_one" {
		t.Fatalf("top = %v, want alpha_one first", names(got))
	}
	for _, c := range got {
		if c.Cost != catalog.CostHigh {
			t.Fatalf("candidate %q Cost = %q, want %q (catalog-free default)", c.Name, c.Cost, catalog.CostHigh)
		}
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after Rank with nil entries = %d, want 1 (catalog-free index must be reused)", got)
	}

	want, err := New(tools, nil).Rank(context.Background(), q, tools, nil, allowAll)
	if err != nil {
		t.Fatalf("Rank via New(tools, nil): %v", err)
	}
	if len(want) != len(got) {
		t.Fatalf("NewWithoutCatalog returned %d candidates, New(tools, nil) returned %d", len(got), len(want))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("candidate %d = %+v, want %+v (must equal New(tools, nil))", i, got[i], want[i])
		}
	}
}
