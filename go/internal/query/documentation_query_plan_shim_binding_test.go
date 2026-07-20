// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDocumentationQueryPlanShimMatchesProductionSearch(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		ScopeID: "scope:largest-search-proof",
		Query:   "needle",
		Limit:   50,
		Offset:  0,
	})
	shim := documentationQueryPlanShimForTest(t)
	if err := validateDocumentationSearchShimForTest(query, args, shim); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentationQueryPlanShimRejectsProductionSearchDrift(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		ScopeID: "scope:largest-search-proof",
		Query:   "needle",
		Limit:   50,
		Offset:  0,
	})
	shim := documentationQueryPlanShimForTest(t)
	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "projection",
			query: strings.Replace(query, "'source_uri', fact_records.source_uri", "'source_uri', NULL", 1),
			args:  args,
		},
		{
			name:  "from shape",
			query: strings.Replace(query, "FROM fact_records", "FROM fact_records AS changed", 1),
			args:  args,
		},
		{
			name:  "search expression",
			query: strings.Replace(query, "fact_records.payload->>'content'", "fact_records.payload->>'body'", 1),
			args:  args,
		},
		{
			name:  "seven-kind allowlist",
			query: strings.Replace(query, "'documentation_link'", "'documentation_link_changed'", 1),
			args:  args,
		},
		{
			name:  "scope predicate",
			query: query,
			args:  replaceDocumentationProofArgForTest(args, 0, "scope:changed"),
		},
		{
			name:  "ordering",
			query: strings.Replace(query, "fact_records.observed_at DESC", "fact_records.observed_at ASC", 1),
			args:  args,
		},
		{
			name:  "limit",
			query: query,
			args:  replaceDocumentationProofArgForTest(args, 2, 52),
		},
		{
			name:  "offset",
			query: query,
			args:  replaceDocumentationProofArgForTest(args, 3, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateDocumentationSearchShimForTest(tt.query, tt.args, shim); err == nil {
				t.Fatal("production drift did not break the checked-in search proof binding")
			}
		})
	}
}

func validateDocumentationSearchShimForTest(query string, args []any, shim string) error {
	productionStatement, err := renderDocumentationProofArgsForTest(query, args)
	if err != nil {
		return fmt.Errorf("render production documentation query: %w", err)
	}

	shimStatement, err := documentationSQLBetweenForTest(
		shim,
		"PREPARE scoped_search AS",
		`\echo SEARCH_BASELINE_PLAN_AND_RESULT`,
	)
	if err != nil {
		return fmt.Errorf("checked-in documentation shim: %w", err)
	}
	got := normalizeDocumentationProofSQLForTest(shimStatement)
	want := normalizeDocumentationProofSQLForTest(productionStatement)
	if got != want {
		return fmt.Errorf("checked-in search proof drifted from production\nshim:       %s\nproduction: %s", got, want)
	}
	return nil
}

func documentationQueryPlanShimForTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(
		filepath.Dir(thisFile), "..", "..", "..",
		"docs", "internal", "evidence", "5275-documentation-query-plan-shim.sql",
	)
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a fixed repository test fixture.
	if err != nil {
		t.Fatalf("read documentation query-plan shim: %v", err)
	}
	return string(raw)
}

func documentationSQLBetweenForTest(sqlText string, start string, end string) (string, error) {
	startAt := strings.Index(sqlText, start)
	if startAt < 0 {
		return "", fmt.Errorf("missing start marker %q", start)
	}
	remaining := sqlText[startAt+len(start):]
	endAt := strings.Index(remaining, end)
	if endAt < 0 {
		return "", fmt.Errorf("missing end marker %q", end)
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(remaining[:endAt]), ";")), nil
}

func renderDocumentationProofArgsForTest(sqlText string, args []any) (string, error) {
	rendered := sqlText
	for index := len(args) - 1; index >= 0; index-- {
		var literal string
		switch value := args[index].(type) {
		case string:
			literal = "'" + strings.ReplaceAll(value, "'", "''") + "'"
		case int:
			literal = strconv.Itoa(value)
		default:
			return "", fmt.Errorf("unsupported argument %d type %T", index+1, value)
		}
		rendered = strings.ReplaceAll(rendered, "$"+strconv.Itoa(index+1), literal)
	}
	return rendered, nil
}

func normalizeDocumentationProofSQLForTest(sqlText string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(sqlText), " "))
	return strings.NewReplacer(
		"( ", "(",
		" )", ")",
	).Replace(normalized)
}

func replaceDocumentationProofArgForTest(args []any, index int, replacement any) []any {
	mutated := append([]any(nil), args...)
	mutated[index] = replacement
	return mutated
}
