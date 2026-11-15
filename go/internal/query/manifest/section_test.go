// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"testing"
)

// TestLoadContextSectionBoundsTheList pins the bounded section the repository
// context response carries: at most maxContextSectionDependencies rows, a
// truncation marker when more exist, and a full_list pointer back to the
// paged read. The dev-inclusion opt-in rides the pointer when set.
func TestLoadContextSectionBoundsTheList(t *testing.T) {
	full := make([]ManifestDependencyRow, 0, maxContextSectionDependencies+1)
	for i := 0; i < maxContextSectionDependencies+1; i++ {
		full = append(full, ManifestDependencyRow{PackageName: "pkg", Scope: "runtime", FactID: "f"})
	}
	truncatedStore := &fakeManifestDependencyStore{page: ManifestDependencyPage{
		Rows: full, Freshness: manifestFreshPage().Freshness,
	}}
	section, refused, err := LoadContextSection(context.Background(), truncatedStore, "repo-1", false)
	if err != nil || refused {
		t.Fatalf("LoadContextSection: refused=%v err=%v, want clean", refused, err)
	}
	if !section.Truncated {
		t.Fatal("Truncated = false, want true past the bound")
	}
	if section.Count != maxContextSectionDependencies || len(section.Dependencies) != maxContextSectionDependencies {
		t.Fatalf("Count = %d rows = %d, want %d", section.Count, len(section.Dependencies), maxContextSectionDependencies)
	}
	if section.FullList != "/api/v0/repository/manifest-dependencies?repository_id=repo-1" {
		t.Fatalf("FullList = %q, want the paged read pointer", section.FullList)
	}

	small := &fakeManifestDependencyStore{page: manifestFreshPage()}
	section, refused, err = LoadContextSection(context.Background(), small, "repo/dev", true)
	if err != nil || refused {
		t.Fatalf("LoadContextSection: refused=%v err=%v, want clean", refused, err)
	}
	if section.Truncated {
		t.Fatal("Truncated = true, want false under the bound")
	}
	if section.Count != 2 {
		t.Fatalf("Count = %d, want 2", section.Count)
	}
	if want := "/api/v0/repository/manifest-dependencies?repository_id=repo%2Fdev&include_dev_dependencies=true"; section.FullList != want {
		t.Fatalf("FullList = %q, want %q", section.FullList, want)
	}
}
