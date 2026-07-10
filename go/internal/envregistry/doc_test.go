// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"os"
	"path/filepath"
	"testing"
)

// envReferenceDocRelPath is the generated reference document, relative to the
// repository root (the parent of the Go module root).
const envReferenceDocRelPath = "docs/public/reference/env-registry.md"

// TestEnvRegistryReferenceDocUpToDate fails when the committed reference doc
// drifts from the registry. Regenerate with:
//
//	bash scripts/generate-env-registry-doc.sh
func TestEnvRegistryReferenceDocUpToDate(t *testing.T) {
	docPath := filepath.Join(repositoryRoot(t), envReferenceDocRelPath)
	want := Default().RenderMarkdown()

	if os.Getenv("ESHU_UPDATE_ENV_DOC") != "" {
		if err := os.WriteFile(docPath, []byte(want), 0o644); err != nil {
			t.Fatalf("write generated doc: %v", err)
		}
		t.Logf("regenerated %s", envReferenceDocRelPath)
		return
	}

	got, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with bash scripts/generate-env-registry-doc.sh)", envReferenceDocRelPath, err)
	}
	if string(got) != want {
		t.Fatalf("%s is out of date; regenerate with bash scripts/generate-env-registry-doc.sh", envReferenceDocRelPath)
	}
}

// repositoryRoot returns the parent of the Go module root, i.e. the repository
// root that contains the docs/ tree.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(goModuleRoot(t))
}
