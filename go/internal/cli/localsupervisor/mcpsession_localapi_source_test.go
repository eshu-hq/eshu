// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// localAPIServingPackage is the in-tree binary directory that builds the
// eshu-local-api session serving binary (NornicDB or unset backend,
// ESHU_API_ADDR plus /healthz). The supervisor spawns it by binary name;
// this pin keeps the source tree the name resolves to from drifting back
// to the retired tree without anyone noticing.
const localAPIServingPackage = "go/cmd/eshu-local-api"

// retiredAPIServingPackage is the tree the serving sources moved out of.
// #11456 deletes it; until then it must hold no Go sources, only the
// removal-evidence Markdown the removal PR dispositions.
const retiredAPIServingPackage = "go/cmd/api"

// localAPIServingSources are the package-main serving files that must exist
// at the relocated home for the eshu-local-api binary to build. They are
// named relative to the serving package so this test never cites a vacated
// path.
var localAPIServingSources = []string{
	"main.go",
	"wiring.go",
	"wiring_router.go",
	"doc.go",
	filepath.Join("diagnostic", "doc.go"),
}

// localAPITestRepoRoot returns the repository root by walking up from the
// test working directory to go.mod and then taking its parent, so the
// go/-prefixed tree pins hold in CI and in a worktree alike.
func localAPITestRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test working directory")
		}
		dir = parent
	}
}

// TestSessionLocalAPIServingTreeRelocated pins the #11514 session half:
// the eshu-local-api serving subset lives at its own binary directory and
// the retired tree holds no Go sources. It fails while the sources still
// point at the old tree and passes after the move plus the consumer
// repoint.
func TestSessionLocalAPIServingTreeRelocated(t *testing.T) {
	t.Parallel()
	root := localAPITestRepoRoot(t)

	servingDir := filepath.Join(root, filepath.FromSlash(localAPIServingPackage))
	for _, rel := range localAPIServingSources {
		if _, err := os.Stat(filepath.Join(servingDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("serving source %s missing under %s: %v", rel, localAPIServingPackage, err)
		}
	}

	retiredDir := filepath.Join(root, filepath.FromSlash(retiredAPIServingPackage))
	entries, err := os.ReadDir(retiredDir)
	if err != nil {
		t.Fatalf("read retired tree: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("retired tree still holds Go source %s, want serving code only under %s", entry.Name(), localAPIServingPackage)
		}
	}
}
