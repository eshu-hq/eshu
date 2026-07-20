// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

const documentationSearchExpressionSQLForTest = `LOWER(
	COALESCE(payload->>'display_name', '') || ' ' ||
	COALESCE(payload->>'title', '') || ' ' ||
	COALESCE(payload->>'heading_text', '') || ' ' ||
	COALESCE(payload->>'content', '') || ' ' ||
	COALESCE(payload->>'target_uri', '')
)`

const documentationCollectedFactKindsSQLForTest = `fact_kind IN (
	'documentation_source',
	'documentation_document',
	'documentation_section',
	'documentation_link',
	'documentation_entity_mention',
	'documentation_claim_candidate',
	'semantic.documentation_observation'
)`

func TestDocumentationReadIndexFinalSchemaMatchesCurrentQueries(t *testing.T) {
	t.Parallel()

	findingsDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_visible_idx",
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

	searchDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_facts_search_trgm_idx",
	)
	for _, want := range []string{
		"USING GIN",
		"gin_trgm_ops",
		documentationSearchExpressionSQLForTest,
		documentationCollectedFactKindsSQLForTest,
		"is_tombstone = FALSE",
	} {
		if !containsNormalizedSQLForTest(searchDDL, want) {
			t.Errorf("documentation facts search index missing %q:\n%s", want, searchDDL)
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
			name: "drop stale findings index",
			path: "go/internal/storage/postgres/migrations/064_drop_documentation_findings_visible_idx.sql",
			want: []string{
				"DROP INDEX CONCURRENTLY IF EXISTS fact_records_documentation_findings_visible_idx",
			},
			statements: 1,
		},
		{
			name: "create corrected findings index",
			path: "go/internal/storage/postgres/migrations/065_create_documentation_findings_visible_idx.sql",
			want: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_findings_visible_idx",
				"fact_kind = 'documentation_finding'",
				"is_tombstone = FALSE",
			},
			forbidden:  documentationFindingACLIndexPredicatesForTest(),
			statements: 1,
		},
		{
			name: "create documentation search index",
			path: "go/internal/storage/postgres/migrations/066_create_documentation_facts_search_trgm_idx.sql",
			want: []string{
				"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_facts_search_trgm_idx",
				"USING GIN",
				"gin_trgm_ops",
				documentationSearchExpressionSQLForTest,
				documentationCollectedFactKindsSQLForTest,
				"is_tombstone = FALSE",
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
