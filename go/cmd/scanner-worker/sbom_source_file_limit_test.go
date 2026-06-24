// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRepositorySBOMSourceCountsSupportedManifestInputsForFileLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "aaa.txt"), "not an SBOM input\n")
	writeTestFile(t, filepath.Join(root, "bbb.txt"), "also not an SBOM input\n")
	writeTestFile(t, filepath.Join(root, "package-lock.json"), `{
		"lockfileVersion": 3,
		"packages": {
			"": {"name": "test-app", "version": "1.0.0"}
		}
	}`)
	source, err := newRepositorySBOMSource([]sbomTargetConfig{{
		ScopeID:  "scanner-worker://repository/repo-private-name",
		RootPath: root,
	}})
	if err != nil {
		t.Fatalf("newRepositorySBOMSource() error = %v, want nil", err)
	}
	input := testSBOMClaimInput(t)
	input.Limits.MaxFiles = 1

	inventory, err := source.Collect(context.Background(), input)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil for one supported manifest", err)
	}
	if got, want := inventory.FileCount, int64(1); got != want {
		t.Fatalf("FileCount = %d, want %d supported manifest input", got, want)
	}
}
