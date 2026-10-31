// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"path"
	"strings"
	"testing"
)

// TestPublicOnlyMigrationPathsExist pins every publicOnlyMigrationPaths key
// to an embedded migration file. A map lookup by path never matches a file
// that was renamed without a map update, so the migration silently stops
// being public-only and isolated-schema bootstraps apply it against missing
// authority tables (#9544: the 24x renumber left 247-267 keys pointing at
// gone filenames while 245a-265 applied unskipped and failed closed).
func TestPublicOnlyMigrationPathsExist(t *testing.T) {
	t.Parallel()

	entries, err := embedded.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations dir: %v", err)
	}
	files := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files[path.Join("go", "internal", "storage", "postgres", "migrations", entry.Name())] = true
	}
	for key := range publicOnlyMigrationPaths {
		if !files[key] {
			t.Errorf("publicOnlyMigrationPaths key %q names no embedded migration file", key)
		}
	}
}
