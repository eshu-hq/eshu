// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestDocumentationReadIndexFinalSchemaMatchesCurrentQueries(t *testing.T) {
	t.Parallel()

	findingsDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_read_idx",
	)
	for _, want := range []string{
		"fact_kind = 'documentation_finding'",
		"is_tombstone = FALSE",
		"(payload->>'finding_type')",
		"observed_at DESC",
		"fact_id DESC",
	} {
		if !strings.Contains(findingsDDL, want) {
			t.Errorf("documentation findings index missing %q:\n%s", want, findingsDDL)
		}
	}
	for _, stale := range documentationFindingACLIndexPredicatesForTest() {
		if strings.Contains(findingsDDL, stale) {
			t.Errorf("documentation findings index keeps stale ACL predicate %q:\n%s", stale, findingsDDL)
		}
	}

	for _, forbidden := range []string{
		"fact_records_documentation_findings_visible_idx",
		"fact_records_documentation_facts_search_trgm_idx",
	} {
		if strings.Contains(documentationFactRecordReadIndexesSQL, forbidden) {
			t.Errorf("final documentation schema keeps rejected or legacy index %q", forbidden)
		}
	}
}

func TestDocumentationReadIndexConcurrentMigrationsAreIsolated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		want       []string
		forbidden  []string
		statements int
	}{
		{
			name: "create corrected findings index first",
			path: "go/internal/storage/postgres/migrations/064_create_documentation_findings_read_idx.sql",
			want: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_read_idx",
				"fact_kind = 'documentation_finding'",
				"is_tombstone = FALSE",
			},
			forbidden:  documentationFindingACLIndexPredicatesForTest(),
			statements: 1,
		},
		{
			name: "drop stale findings index second",
			path: "go/internal/storage/postgres/migrations/065_drop_documentation_findings_visible_idx.sql",
			want: []string{
				"DROP INDEX CONCURRENTLY IF EXISTS fact_records_documentation_findings_visible_idx",
			},
			statements: 1,
		},
	}

	definitions := BootstrapDefinitions()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, ok := definitionByPathForTest(definitions, tt.path)
			if !ok {
				t.Fatalf("migration %q is missing", tt.path)
			}
			for _, want := range tt.want {
				if !containsNormalizedSQLForTest(definition.SQL, want) {
					t.Errorf("migration %q missing %q:\n%s", tt.path, want, definition.SQL)
				}
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(definition.SQL, forbidden) {
					t.Errorf("migration %q contains stale predicate %q:\n%s", tt.path, forbidden, definition.SQL)
				}
			}
			if got := strings.Count(definition.SQL, ";"); got != tt.statements {
				t.Errorf("migration %q has %d SQL terminators, want %d; concurrent DDL must remain isolated", tt.path, got, tt.statements)
			}
		})
	}
}

func TestDocumentationRejectedAndLegacyIndexesAreNotReplayed(t *testing.T) {
	t.Parallel()

	definitions := BootstrapDefinitions()
	facts, ok := definitionByPathForTest(
		definitions,
		"go/internal/storage/postgres/migrations/003_fact_records.sql",
	)
	if !ok {
		t.Fatal("fact records migration is missing")
	}
	if strings.Contains(facts.SQL, "fact_records_documentation_findings_visible_idx") {
		t.Fatal("fact records migration still recreates the legacy findings index")
	}
	for _, definition := range definitions {
		if strings.Contains(definition.SQL, "fact_records_documentation_facts_search_trgm_idx") {
			t.Fatalf("bootstrap definition %q keeps rejected documentation search GIN", definition.Path)
		}
	}
}

func containsNormalizedSQLForTest(sqlText string, fragment string) bool {
	return strings.Contains(
		strings.Join(strings.Fields(sqlText), " "),
		strings.Join(strings.Fields(fragment), " "),
	)
}

func documentationFindingACLIndexPredicatesForTest() []string {
	return []string{
		"viewer_can_read_source",
		"source_acl_evaluated",
		"permission_decision",
	}
}

func indexStatementForTest(t *testing.T, ddl string, indexName string) string {
	t.Helper()
	start := strings.Index(ddl, indexName)
	if start < 0 {
		t.Fatalf("index %q is missing from final schema", indexName)
	}
	end := strings.Index(ddl[start:], ";")
	if end < 0 {
		t.Fatalf("index %q statement is not terminated", indexName)
	}
	return ddl[start : start+end]
}

func definitionByPathForTest(definitions []Definition, path string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Path == path {
			return definition, true
		}
	}
	return Definition{}, false
}
