// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestContentEntityNameIndexMigrationAndDeferredBootstrapStayInLockstep(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_entity_name_trgm_index")
	for _, fragment := range []string{
		"IF eshu_content_substring_indexes_valid() THEN",
		"CREATE INDEX IF NOT EXISTS content_entities_name_trgm_idx",
		"content_entities_name_trgm_idx",
		"indexed_attribute.attname = 'entity_name'",
		"operator_class.opcname = 'gin_trgm_ops'",
		"state = 'not_built'",
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
	for _, definition := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		if definition.Name == "content_store" && strings.Contains(definition.SQL, "content_entities_name_trgm_idx") {
			t.Fatal("deferred bootstrap eagerly creates content entity name GIN index")
		}
	}
}

func TestContentEntityNameIndexMigrationChecksLegacyReadinessBeforeReplacingValidator(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_entity_name_trgm_index")
	legacyCheck := strings.Index(migration, "IF eshu_content_substring_indexes_valid() THEN")
	createIndex := strings.Index(migration, "CREATE INDEX IF NOT EXISTS content_entities_name_trgm_idx")
	replaceValidator := strings.Index(migration, "CREATE OR REPLACE FUNCTION eshu_content_substring_indexes_valid()")
	if legacyCheck < 0 || createIndex < legacyCheck || replaceValidator < createIndex {
		t.Fatalf("migration order does not gate the name index on pre-062 two-index readiness")
	}
}
