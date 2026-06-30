// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

func TestSemanticExtractionContractUsesRegisteredEvidenceTools(t *testing.T) {
	t.Parallel()
	overlay, err := capabilitycatalog.LoadSurfaceOverlay(filepath.Join(repoSpecsDir(t), capabilitycatalog.SurfaceOverlayFileName))
	if err != nil {
		t.Fatalf("LoadSurfaceOverlay() error = %v, want nil", err)
	}
	var contract capabilitycatalog.CollectorContract
	for _, rec := range overlay.Surfaces {
		if rec.Category == capabilitycatalog.SurfaceCollector && rec.Name == "semantic_extraction" {
			contract = rec.CollectorContract
			break
		}
	}
	if len(contract.ReadSurfaces) == 0 {
		t.Fatal("semantic_extraction read surfaces missing")
	}

	registered := map[string]struct{}{}
	for _, tool := range mcp.ReadOnlyTools() {
		registered[tool.Name] = struct{}{}
	}
	declared := map[string]struct{}{}
	for _, surface := range contract.ReadSurfaces {
		declared[surface] = struct{}{}
	}
	for _, tool := range []string{"list_semantic_documentation_observations", "list_semantic_code_hints"} {
		if _, ok := registered[tool]; !ok {
			t.Fatalf("MCP registry missing %q", tool)
		}
		if _, ok := declared[tool]; !ok {
			t.Fatalf("semantic_extraction read surfaces missing %q: %v", tool, contract.ReadSurfaces)
		}
	}
	if _, ok := declared["list_semantic_evidence"]; ok {
		t.Fatalf("semantic_extraction read surfaces include stale MCP tool list_semantic_evidence: %v", contract.ReadSurfaces)
	}
}
