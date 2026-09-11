// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"sort"
	"testing"
)

func TestCodeCallRepositoryImportPathsForResolutionFallsBackWithoutCache(t *testing.T) {
	t.Parallel()

	repositoryImports := map[string][]string{
		"alpha": {"src/alpha.ts", "src/shared.ts"},
		"beta":  {"src/beta.ts", "src/shared.ts"},
	}
	got := RepositoryImportPathsForResolution(
		EntityIndex{},
		"repo-cache",
		repositoryImports,
	)
	want := RepositoryImportPaths(repositoryImports)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback repository import paths = %#v, want %#v", got, want)
	}
}

func TestCodeCallRepositoryImportPathsForResolutionSkipsEmptyImports(t *testing.T) {
	t.Parallel()

	got := RepositoryImportPathsForResolution(EntityIndex{}, "repo-cache", nil)
	if got != nil {
		t.Fatalf("empty repository import paths = %#v, want nil", got)
	}
}
