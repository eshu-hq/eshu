// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeRepositoryOrderedContentSearchIndex(t *testing.T) {
	t.Parallel()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name != "content_entities_repo_path_start_idx" {
			continue
		}
		const want = `CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repo_path_start_idx
    ON content_entities (repo_id, relative_path, start_line, entity_id);`
		if !strings.Contains(definition.SQL, want) {
			t.Fatalf("repository-ordered content index does not match search predicate and order:\n%s", definition.SQL)
		}
		return
	}
	t.Fatal("repository-ordered content search index migration is missing")
}
