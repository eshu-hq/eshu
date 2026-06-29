// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

func TestGoldenSnapshotMCPShapesRouteEveryRegisteredTool(t *testing.T) {
	snap, err := goldengate.LoadSnapshot(filepath.Join("..", "..", "..", "testdata", "golden", "e2e-20repo-snapshot.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	for _, tool := range ReadOnlyTools() {
		shape, ok := snap.QueryShapes.MCP[tool.Name]
		if !ok {
			t.Errorf("query_shapes.mcp missing %s", tool.Name)
			continue
		}
		if _, err := resolveRoute(tool.Name, shape.Arguments); err != nil {
			t.Errorf("query_shapes.mcp[%s] arguments do not resolve: %v", tool.Name, err)
		}
	}
}
