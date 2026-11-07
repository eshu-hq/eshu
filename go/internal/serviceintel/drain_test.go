// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestQueryRootDrained fails while any non-test file in this package still
// imports the handler-heavy query root (row 8 of
// REMOVAL_CONSUMER_DRAIN_CHECKLIST.md). Answer packets come from the Ask
// family and every other shape is a versioned read contract, so this package
// must spell them through query/ask and querycontract.
func TestQueryRootDrained(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/eshu-hq/eshu/go/internal/query" {
				t.Errorf("%s still imports the query root; retarget to query/ask and querycontract", name)
			}
		}
	}
}

// TestDrainAdaptsDossierThroughContracts proves the service-story adapter
// still maps identity fields and carries the truth envelope when both arrive
// through querycontract instead of the query-root spellings.
func TestDrainAdaptsDossierThroughContracts(t *testing.T) {
	t.Parallel()
	truth := &querycontract.TruthEnvelope{
		Level: querycontract.TruthLevelExact,
		Basis: querycontract.TruthBasisAuthoritativeGraph,
	}
	input := FromServiceStory(map[string]any{
		"service_identity": map[string]any{
			"service_id":   "svc:checkout",
			"service_name": "checkout",
		},
	}, truth)
	if input.Subject.ServiceID != "svc:checkout" {
		t.Fatalf("FromServiceStory() service ID = %q, want %q", input.Subject.ServiceID, "svc:checkout")
	}
	if len(input.Sections) == 0 {
		t.Fatal("FromServiceStory() sections are empty, want the identity section")
	}
	if input.Sections[0].Truth != truth {
		t.Fatal("FromServiceStory() dropped the truth envelope")
	}
}
