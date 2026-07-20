// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDocumentationFactSearchQueryMatchesIndexExpression(t *testing.T) {
	t.Parallel()

	querySQL, _ := buildDocumentationFactsSQL(documentationFactFilter{Query: "deployment"})
	queryExpression := extractSQLFunctionCallForTest(t, querySQL, "LOWER")

	repoRoot := documentationQueryPlanRepoRoot(t)
	finalDDL := readDocumentationQueryPlanFile(t, filepath.Join(
		repoRoot,
		"go/internal/storage/postgres/schema_fact_records_documentation.go",
	))
	migrationDDL := readDocumentationQueryPlanFile(t, filepath.Join(
		repoRoot,
		"go/internal/storage/postgres/migrations/066_create_documentation_facts_search_trgm_idx.sql",
	))

	for name, ddl := range map[string]string{
		"final schema":  finalDDL,
		"migration 066": migrationDDL,
	} {
		indexExpression := extractSQLFunctionCallForTest(t, ddl, "LOWER")
		if normalizeSQLExpressionForTest(indexExpression) != normalizeSQLExpressionForTest(queryExpression) {
			t.Errorf("%s search expression drifted from buildDocumentationFactsSQL\nquery: %s\nindex: %s", name, queryExpression, indexExpression)
		}
	}

	fields := []string{"display_name", "title", "heading_text", "content", "target_uri"}
	fieldTokens := make([]string, 0, len(fields))
	for _, field := range fields {
		fieldTokens = append(fieldTokens, "payload->>'"+field+"'")
	}
	assertSQLTokensInOrderForTest(t, "query search expression", queryExpression, fieldTokens)

	factKinds := []string{
		"'documentation_source'",
		"'documentation_document'",
		"'documentation_section'",
		"'documentation_link'",
		"'documentation_entity_mention'",
		"'documentation_claim_candidate'",
		"'semantic.documentation_observation'",
	}
	assertSQLTokensInOrderForTest(t, "query fact kinds", querySQL, factKinds)
	for name, ddl := range map[string]string{
		"final schema":  finalDDL,
		"migration 066": migrationDDL,
	} {
		searchIndexStart := strings.Index(ddl, "fact_records_documentation_facts_search_trgm_idx")
		if searchIndexStart < 0 {
			t.Errorf("%s is missing the documentation facts search index", name)
			continue
		}
		assertSQLTokensInOrderForTest(t, name+" fact kinds", ddl[searchIndexStart:], factKinds)
	}
}

func assertSQLTokensInOrderForTest(t *testing.T, name string, sqlText string, tokens []string) {
	t.Helper()
	last := -1
	for _, token := range tokens {
		index := strings.Index(sqlText, token)
		if index < 0 {
			t.Errorf("%s is missing %q", name, token)
			continue
		}
		if index <= last {
			t.Errorf("%s token %q is out of order", name, token)
		}
		last = index
	}
}

func extractSQLFunctionCallForTest(t *testing.T, sqlText string, functionName string) string {
	t.Helper()
	start := strings.Index(sqlText, functionName+"(")
	if start < 0 {
		t.Fatalf("SQL does not contain %s(...):\n%s", functionName, sqlText)
	}
	depth := 0
	for index := start + len(functionName); index < len(sqlText); index++ {
		switch sqlText[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return sqlText[start : index+1]
			}
		}
	}
	t.Fatalf("SQL contains an unterminated %s(...) expression:\n%s", functionName, sqlText)
	return ""
}

func normalizeSQLExpressionForTest(expression string) string {
	expression = strings.ReplaceAll(expression, "fact_records.", "")
	return regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(expression), " ")
}

func documentationQueryPlanRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func readDocumentationQueryPlanFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(raw)
}
