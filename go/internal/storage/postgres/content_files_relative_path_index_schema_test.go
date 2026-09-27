// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

func TestContentFilesRelativePathTrigramIndexMigrationIsIsolated(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_files_relative_path_trgm_index")
	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS content_files_relative_path_trgm_idx",
		"ON content_files USING gin (relative_path gin_trgm_ops)",
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("content_files relative-path migration missing %q:\n%s", want, migration)
		}
	}
	if count := strings.Count(migration, "CREATE INDEX CONCURRENTLY"); count != 1 {
		t.Fatalf("migration has %d concurrent index statements, want one isolated statement:\n%s", count, migration)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("migration is not one isolated concurrent-index statement:\n%s", migration)
	}
}

func TestDeferredContentBootstrapDefersRelativePathIndexUntilFinalization(t *testing.T) {
	t.Parallel()

	var deferred Definition
	for _, definition := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		if definition.Name == "content_files_relative_path_trgm_index" {
			deferred = definition
			break
		}
	}
	if deferred.Name == "" {
		t.Fatal("deferred bootstrap omits the content_files relative-path index migration")
	}
	if deferred.Variant != "deferred" || deferred.FullChecksum == "" {
		t.Fatalf("deferred relative-path index definition = %+v, want deferred variant with full checksum", deferred)
	}
	if strings.Contains(deferred.SQL, "content_files_relative_path_trgm_idx") ||
		strings.Contains(deferred.SQL, "CREATE INDEX") {
		t.Fatalf("deferred bootstrap eagerly builds the relative-path index:\n%s", deferred.SQL)
	}
}

func TestContentFilesRelativePathIndexLifecycleRequiresExactIndex(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_files_relative_path_trgm_index_lifecycle")
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION eshu_content_substring_indexes_valid()",
		"content_files_relative_path_trgm_idx",
		"indexed_attribute.attname = 'relative_path'",
		"operator_class.opcname = 'gin_trgm_ops'",
		"state = 'not_built'",
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("relative-path index lifecycle migration missing %q:\n%s", want, migration)
		}
	}
}
