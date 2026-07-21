// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

func BenchmarkImportDependencyScopeLookup(b *testing.B) {
	const size = 5000
	rows := make([]map[string]any, size)
	scopes := make([]map[string]any, size)
	for index := range size {
		repoID := fmt.Sprintf("repo-%05d", index)
		path := fmt.Sprintf("/proof/%05d/file.go", index)
		rows[index] = map[string]any{"repo_id": repoID, "source_path": path}
		scopes[size-index-1] = map[string]any{"repo_id": repoID, "path": path}
	}

	b.Run("linear-baseline", func(b *testing.B) {
		for range b.N {
			if got := linearImportDependencyScopeMatches(rows, scopes); got != size {
				b.Fatalf("matched rows = %d, want %d", got, size)
			}
		}
	})
	b.Run("indexed-production", func(b *testing.B) {
		for range b.N {
			if got := len(filterImportDependencyScopeRows(rows, "repo_id", "source_path", scopes)); got != size {
				b.Fatalf("matched rows = %d, want %d", got, size)
			}
		}
	})
}

func linearImportDependencyScopeMatches(rows, scopes []map[string]any) int {
	matched := 0
	for _, row := range rows {
		for _, scope := range scopes {
			if StringVal(row, "repo_id") == StringVal(scope, "repo_id") &&
				StringVal(row, "source_path") == StringVal(scope, "path") {
				matched++
				break
			}
		}
	}
	return matched
}
