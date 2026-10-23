// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// envReferenceDirRelPath is the directory that holds the generated reference
// pages, relative to the repository root (the parent of the Go module root).
const envReferenceDirRelPath = "docs/public/reference"

// TestEnvRegistryReferenceDocUpToDate fails when any committed reference page
// (the index or a per-family page) drifts from the registry, when a generated
// page is missing, or when a stale file sits in the generated pages directory.
// Regenerate with:
//
//	bash scripts/generate-env-registry-doc.sh
func TestEnvRegistryReferenceDocUpToDate(t *testing.T) {
	refDir := filepath.Join(repositoryRoot(t), filepath.FromSlash(envReferenceDirRelPath))
	pages, err := Default().RenderPages()
	if err != nil {
		t.Fatalf("render reference pages: %v", err)
	}
	want := map[string]string{}
	for _, p := range pages {
		want[p.Path] = p.Content
	}

	if os.Getenv("ESHU_UPDATE_ENV_DOC") != "" {
		updateReferencePages(t, refDir, pages)
		return
	}

	for _, p := range pages {
		got, err := os.ReadFile(filepath.Join(refDir, filepath.FromSlash(p.Path)))
		if err != nil {
			t.Errorf("read %s/%s: %v (regenerate with bash scripts/generate-env-registry-doc.sh)", envReferenceDirRelPath, p.Path, err)
			continue
		}
		if string(got) != p.Content {
			t.Errorf("%s/%s is out of date; regenerate with bash scripts/generate-env-registry-doc.sh", envReferenceDirRelPath, p.Path)
		}
	}
	for _, orphan := range orphanedPages(t, refDir, want) {
		t.Errorf("%s/%s is not produced by the generator; regenerate with bash scripts/generate-env-registry-doc.sh to remove it", envReferenceDirRelPath, orphan)
	}
}

// updateReferencePages writes every generated page and removes stale files in
// the generated pages directory, so a renamed or dropped page cannot linger.
func updateReferencePages(t *testing.T, refDir string, pages []Page) {
	t.Helper()
	want := map[string]string{}
	for _, p := range pages {
		want[p.Path] = p.Content
	}
	if err := os.MkdirAll(filepath.Join(refDir, PagesDir), 0o755); err != nil {
		t.Fatalf("create pages dir: %v", err)
	}
	for _, orphan := range orphanedPages(t, refDir, want) {
		if err := os.Remove(filepath.Join(refDir, filepath.FromSlash(orphan))); err != nil {
			t.Fatalf("remove stale page %s: %v", orphan, err)
		}
	}
	for _, p := range pages {
		if err := os.WriteFile(filepath.Join(refDir, filepath.FromSlash(p.Path)), []byte(p.Content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p.Path, err)
		}
		t.Logf("regenerated %s/%s", envReferenceDirRelPath, p.Path)
	}
}

// orphanedPages lists files under the generated pages directory that the
// generator no longer produces, as paths relative to refDir.
func orphanedPages(t *testing.T, refDir string, want map[string]string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(refDir, PagesDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read generated pages dir: %v", err)
	}
	var orphans []string
	for _, e := range entries {
		rel := PagesDir + "/" + e.Name()
		if _, ok := want[rel]; !ok {
			orphans = append(orphans, rel)
		}
	}
	sort.Strings(orphans)
	return orphans
}

// repositoryRoot returns the parent of the Go module root, i.e. the repository
// root that contains the docs/ tree.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(goModuleRoot(t))
}
