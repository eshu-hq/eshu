// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeRepositoryTreePathIndex(t *testing.T) {
	t.Parallel()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name != "content_files_repo_path_pattern_idx" {
			continue
		}
		if !strings.Contains(definition.SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_repo_path_pattern_idx") ||
			!strings.Contains(definition.SQL, "ON content_files (repo_id, relative_path text_pattern_ops)") {
			t.Fatalf("repository tree path index must support repository-scoped anchored LIKE: %s", definition.SQL)
		}
		return
	}
	t.Fatal("repository tree path index migration is missing")
}
