// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
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
	if err := validateDocumentationFindingReadIndexForTest(findingsDDL); err != nil {
		t.Errorf("final documentation findings index: %v", err)
	}
	for _, stale := range documentationFindingACLIndexPredicatesForTest() {
		if strings.Contains(findingsDDL, stale) {
			t.Errorf("documentation findings index keeps stale ACL predicate %q:\n%s", stale, findingsDDL)
		}
	}

	definitions := BootstrapDefinitions()
	create, ok := definitionByPathForTest(
		definitions,
		"go/internal/storage/postgres/migrations/064_create_documentation_findings_read_idx.sql",
	)
	if !ok {
		t.Fatal("documentation findings replacement migration is missing")
	}
	if err := validateDocumentationFindingReadIndexForTest(create.SQL); err != nil {
		t.Errorf("replacement migration documentation findings index: %v", err)
	}
	if got, want := documentationFindingReadIndexShapeForTest(create.SQL),
		documentationFindingReadIndexShapeForTest(findingsDDL); got != want {
		t.Errorf("replacement migration and final schema differ\nmigration: %s\nfinal:     %s", got, want)
	}

	for _, forbidden := range []string{"fact_records_documentation_facts_search_trgm_idx"} {
		if strings.Contains(documentationFactRecordReadIndexesSQL, forbidden) {
			t.Errorf("final documentation schema keeps rejected index %q", forbidden)
		}
	}
}

func TestDocumentationAggregateVisibleIndexIsRetainedAcrossBootstrapPaths(t *testing.T) {
	t.Parallel()

	finalDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_visible_idx",
	)
	if err := validateDocumentationFindingAggregateVisibleIndexForTest(finalDDL); err != nil {
		t.Errorf("final aggregate-visible documentation findings index: %v", err)
	}

	definitions := BootstrapDefinitions()
	facts, ok := definitionByPathForTest(
		definitions,
		"go/internal/storage/postgres/migrations/003_fact_records.sql",
	)
	if !ok {
		t.Fatal("fact records migration is missing")
	}
	migrationDDL := indexStatementForTest(
		t,
		facts.SQL,
		"fact_records_documentation_findings_visible_idx",
	)
	if err := validateDocumentationFindingAggregateVisibleIndexForTest(migrationDDL); err != nil {
		t.Errorf("fact records migration aggregate-visible documentation findings index: %v", err)
	}
	if got, want := documentationFindingReadIndexShapeForTest(migrationDDL),
		documentationFindingReadIndexShapeForTest(finalDDL); got != want {
		t.Errorf("fact records migration and final aggregate-visible schema differ\nmigration: %s\nfinal:     %s", got, want)
	}
}

func TestDocumentationReadIndexContractRejectsEveryMissingKey(t *testing.T) {
	t.Parallel()

	findingsDDL := indexStatementForTest(
		t,
		documentationFactRecordReadIndexesSQL,
		"fact_records_documentation_findings_read_idx",
	)
	for _, term := range documentationFindingReadIndexOrderedTermsForTest() {
		term := term
		t.Run(term, func(t *testing.T) {
			t.Parallel()
			normalized := strings.ToLower(findingsDDL)
			mutated := strings.Replace(normalized, term, "'removed_index_term'", 1)
			if mutated == normalized {
				t.Fatalf("test fixture does not contain ordered term %q", term)
			}
			if err := validateDocumentationFindingReadIndexForTest(mutated); err == nil {
				t.Fatalf("index contract accepted definition without %q", term)
			}
		})
	}
}

func TestDocumentationReadIndexConcurrentMigrationIsIsolated(t *testing.T) {
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

func TestDocumentationAggregateVisibleIndexAndRejectedIndexesAreReplayed(t *testing.T) {
	t.Parallel()

	definitions := BootstrapDefinitions()
	facts, ok := definitionByPathForTest(
		definitions,
		"go/internal/storage/postgres/migrations/003_fact_records.sql",
	)
	if !ok {
		t.Fatal("fact records migration is missing")
	}
	visibleDDL := indexStatementForTest(
		t,
		facts.SQL,
		"fact_records_documentation_findings_visible_idx",
	)
	if err := validateDocumentationFindingAggregateVisibleIndexForTest(visibleDDL); err != nil {
		t.Fatalf("fact records migration aggregate-visible index: %v", err)
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

func documentationFindingReadIndexOrderedTermsForTest() []string {
	return []string{
		"'finding_type'",
		"'source_id'",
		"'document_id'",
		"'status'",
		"'truth_level'",
		"'freshness_state'",
		"observed_at desc",
		"fact_id desc",
	}
}

func validateDocumentationFindingAggregateVisibleIndexForTest(definition string) error {
	if err := validateDocumentationFindingReadIndexForTest(definition); err != nil {
		return err
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	for _, predicate := range []string{
		"viewer_can_read_source",
		"source_acl_evaluated",
		"permission_decision",
	} {
		if !strings.Contains(normalized, predicate) {
			return fmt.Errorf("missing aggregate ACL predicate %q", predicate)
		}
	}
	return nil
}

func validateDocumentationFindingReadIndexForTest(definition string) error {
	normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	previous := -1
	for _, term := range documentationFindingReadIndexOrderedTermsForTest() {
		position := strings.Index(normalized, term)
		if position < 0 {
			return fmt.Errorf("missing ordered term %q", term)
		}
		if position <= previous {
			return fmt.Errorf("ordered term %q appears out of order", term)
		}
		previous = position
	}
	for _, predicate := range []string{
		"fact_kind = 'documentation_finding'",
		"is_tombstone = false",
	} {
		if !strings.Contains(normalized, predicate) {
			return fmt.Errorf("missing partial-index predicate %q", predicate)
		}
	}
	return nil
}

func documentationFindingReadIndexShapeForTest(definition string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
	start := strings.Index(normalized, " on fact_records ")
	if start < 0 {
		return normalized
	}
	shape := strings.TrimSuffix(normalized[start:], ";")
	return strings.NewReplacer(
		"( ", "(",
		" )", ")",
		", ", ",",
	).Replace(shape)
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
