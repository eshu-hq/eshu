// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	servicetools "github.com/eshu-hq/eshu/go/internal/mcp/service"
)

func TestReadOnlyToolsKeepsServiceRegistrationPositions(t *testing.T) {
	t.Parallel()

	wantCatalog := servicetools.CatalogTools()
	if got := serviceCatalogTools(); !reflect.DeepEqual(got, wantCatalog) {
		t.Fatal("root serviceCatalogTools wrapper drifted from service.CatalogTools")
	}
	wantContext := servicetools.ContextTools()
	rootContext := contextTools()
	if got, want := len(rootContext), 7; got != want {
		t.Fatalf("root context tool count = %d, want %d", got, want)
	}
	if got := rootContext[4:]; !reflect.DeepEqual(got, wantContext) {
		t.Fatal("root contextTools service definitions drifted from service.ContextTools")
	}
	wantIntelligence := servicetools.IntelligenceTools()
	if got := serviceIntelligenceTools(); !reflect.DeepEqual(got, wantIntelligence) {
		t.Fatal("root serviceIntelligenceTools wrapper drifted from service.IntelligenceTools")
	}

	tools := ReadOnlyTools()
	if got, want := len(tools), 162; got != want {
		t.Fatalf("ReadOnlyTools count = %d, want %d", got, want)
	}
	assertServiceRegistrationRange(t, tools, 74, []string{
		"get_ci_cd_run_correlation_inventory",
		"list_service_catalog_correlations",
		"list_codeowners_ownership",
	})
	assertServiceRegistrationRange(t, tools, 110, []string{
		"get_workload_story",
		"get_service_context",
		"get_service_story",
		"investigate_service",
		"get_service_intelligence_report",
		"get_file_content",
	})
	if got := tools[75:76]; !reflect.DeepEqual(got, wantCatalog) {
		t.Fatal("ReadOnlyTools catalog definition drifted from service.CatalogTools")
	}
	if got := tools[111:114]; !reflect.DeepEqual(got, wantContext) {
		t.Fatal("ReadOnlyTools context definitions drifted from service.ContextTools")
	}
	if got := tools[114:115]; !reflect.DeepEqual(got, wantIntelligence) {
		t.Fatal("ReadOnlyTools intelligence definition drifted from service.IntelligenceTools")
	}

	const wantHash = "8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b"
	hash := sha256.New()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantHash)
	}
}

func assertServiceRegistrationRange(t *testing.T, tools []ToolDefinition, start int, wantNames []string) {
	t.Helper()

	end := start + len(wantNames)
	if end > len(tools) {
		t.Fatalf("service registration range [%d:%d] exceeds %d tools", start, end, len(tools))
	}
	for i, want := range wantNames {
		if got := tools[start+i].Name; got != want {
			t.Fatalf("ReadOnlyTools service boundary name[%d] = %q, want %q", start+i, got, want)
		}
	}
}
