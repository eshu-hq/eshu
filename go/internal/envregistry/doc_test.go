// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"fmt"
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

	if os.Getenv("ESHU_UPDATE_ENV_DOC") != "" {
		if err := regenerateReference(refDir, Default().RenderPages); err != nil {
			t.Fatalf("regenerate reference: %v", err)
		}
		t.Logf("regenerated %s/%s and %s/%s/", envReferenceDirRelPath, IndexPagePath, envReferenceDirRelPath, PagesDir)
		return
	}

	pages, err := Default().RenderPages()
	if err != nil {
		t.Fatalf("render reference pages: %v", err)
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
	orphans, err := orphanedPages(refDir, pages)
	if err != nil {
		t.Fatalf("list generated pages dir: %v", err)
	}
	for _, orphan := range orphans {
		t.Errorf("%s/%s is not produced by the generator; regenerate with bash scripts/generate-env-registry-doc.sh to remove it", envReferenceDirRelPath, orphan)
	}
}

// TestReferenceRegenerationWritesNothingOnFailure pins the generator's
// all-or-nothing contract for every refusal class: when rendering fails, the
// reference directory (existing pages, stale pages, and the directory listing)
// is byte-for-byte what it was, so a failed regeneration never leaves a half
// updated reference behind.
func TestReferenceRegenerationWritesNothingOnFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range renderFailureCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			seed := map[string]string{
				IndexPagePath:                  "old index\n",
				PagesDir + "/api-and-auth.md":  "old page\n",
				PagesDir + "/leftover-page.md": "stale page\n",
			}
			for rel, body := range seed {
				path := filepath.Join(dir, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotDir(t, dir)

			err := regenerateReference(dir, func() ([]Page, error) { return tc.reg.renderPages(tc.layout, tc.lineCap) })
			if err == nil {
				t.Fatal("regenerateReference must fail")
			}
			if after := snapshotDir(t, dir); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("a failed regeneration changed the reference directory:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// TestReferenceRegenerationWritesAllPagesAndRemovesOrphans is the success half:
// a clean render writes the index and every page, and removes a leftover file.
func TestReferenceRegenerationWritesAllPagesAndRemovesOrphans(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, PagesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(dir, PagesDir, "leftover-page.md")
	if err := os.WriteFile(leftover, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := regenerateReference(dir, Default().RenderPages); err != nil {
		t.Fatalf("regenerateReference: %v", err)
	}
	pages := mustRenderPages(t, Default())
	for _, p := range pages {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p.Path)))
		if err != nil || string(got) != p.Content {
			t.Errorf("%s was not written with the rendered content (err %v)", p.Path, err)
		}
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("leftover page still exists (stat err %v)", err)
	}
}

// regenerateReference renders the reference and, only when rendering succeeds,
// writes every page under refDir and removes files in the generated pages
// directory that the render no longer produces. A failed render touches
// nothing.
func regenerateReference(refDir string, render func() ([]Page, error)) error {
	pages, err := render()
	if err != nil {
		return err
	}
	orphans, err := orphanedPages(refDir, pages)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(refDir, PagesDir), 0o755); err != nil {
		return err
	}
	for _, orphan := range orphans {
		if err := os.Remove(filepath.Join(refDir, filepath.FromSlash(orphan))); err != nil {
			return err
		}
	}
	for _, p := range pages {
		if err := os.WriteFile(filepath.Join(refDir, filepath.FromSlash(p.Path)), []byte(p.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// orphanedPages lists files under the generated pages directory that pages no
// longer includes, as paths relative to refDir.
func orphanedPages(refDir string, pages []Page) ([]string, error) {
	want := map[string]struct{}{}
	for _, p := range pages {
		want[p.Path] = struct{}{}
	}
	entries, err := os.ReadDir(filepath.Join(refDir, PagesDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var orphans []string
	for _, e := range entries {
		rel := PagesDir + "/" + e.Name()
		if _, ok := want[rel]; !ok {
			orphans = append(orphans, rel)
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

// snapshotDir returns "relative path=content" for every file under dir, sorted.
func snapshotDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, rel+"="+string(body))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

// repositoryRoot returns the parent of the Go module root, i.e. the repository
// root that contains the docs/ tree.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(goModuleRoot(t))
}
