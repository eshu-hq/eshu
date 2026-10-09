// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/search"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// searchPartialFieldList renders the wire fields of data.partial in the order
// the struct declares them, the way the three descriptions spell them.
func searchPartialFieldList(t *testing.T) string {
	t.Helper()
	typ := reflect.TypeOf(querycontract.SearchPartial{})
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("SearchPartial field %s has no json name", typ.Field(i).Name)
		}
		names = append(names, name)
	}
	return "(" + strings.Join(names, ", ") + ")"
}

// TestSearchPartialFieldListMatchesTheWireContract pins that the three places
// that spell out the data.partial fields list every field of
// querycontract.SearchPartial, in declaration order, so a field added to the
// struct cannot be left out of the operator-facing text again (#7730).
func TestSearchPartialFieldListMatchesTheWireContract(t *testing.T) {
	t.Parallel()
	want := searchPartialFieldList(t)

	entry, ok := envregistry.Default().Lookup("ESHU_CONTENT_SEARCH_BUDGET_MS")
	if !ok {
		t.Fatal("env registry has no ESHU_CONTENT_SEARCH_BUDGET_MS entry")
	}
	tool := requireToolDefinition(t, "search_file_content")
	texts := map[string]string{
		"env registry ESHU_CONTENT_SEARCH_BUDGET_MS": entry.Description,
		"MCP search_file_content description":        tool.Description,
		"OpenAPI /content/files/search description":  search.Content,
	}
	for place, text := range texts {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain the data.partial field list %s", place, want)
		}
	}
}
