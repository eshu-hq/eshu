// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "testing"

func TestSnapshotEntityMetadataKeepsFunctionPackageImportPath(t *testing.T) {
	t.Parallel()

	metadata := snapshotEntityMetadata(map[string]any{
		"name":                "handle",
		"line_number":         12,
		"lang":                "go",
		"package_import_path": "example.com/repo/handlers",
	})

	if got, want := metadata["package_import_path"], "example.com/repo/handlers"; got != want {
		t.Fatalf("package_import_path = %#v, want %#v", got, want)
	}
	if _, present := metadata["name"]; present {
		t.Fatalf("reserved name leaked into metadata: %+v", metadata)
	}
	if _, present := metadata["line_number"]; present {
		t.Fatalf("reserved line_number leaked into metadata: %+v", metadata)
	}
}
