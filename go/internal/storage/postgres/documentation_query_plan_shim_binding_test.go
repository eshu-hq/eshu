// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDocumentationQueryPlanShimMatchesProductionFactUpsert(t *testing.T) {
	t.Parallel()

	shim := documentationQueryPlanShimForPostgresTest(t)
	if err := validateDocumentationWriteShimForTest(
		upsertFactBatchPrefix,
		upsertFactBatchSuffix,
		shim,
	); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentationQueryPlanShimRejectsProductionFactUpsertDrift(t *testing.T) {
	t.Parallel()

	shim := documentationQueryPlanShimForPostgresTest(t)
	tests := []struct {
		name   string
		prefix string
		suffix string
	}{
		{
			name:   "seventeen-column insert",
			prefix: strings.Replace(upsertFactBatchPrefix, "source_uri", "source_url", 1),
			suffix: upsertFactBatchSuffix,
		},
		{
			name:   "conflict update",
			prefix: upsertFactBatchPrefix,
			suffix: strings.Replace(upsertFactBatchSuffix, "payload = EXCLUDED.payload", "payload = '{}'::jsonb", 1),
		},
		{
			name:   "fencing predicate",
			prefix: upsertFactBatchPrefix,
			suffix: strings.Replace(upsertFactBatchSuffix, "<= EXCLUDED.fencing_token", "< EXCLUDED.fencing_token", 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateDocumentationWriteShimForTest(tt.prefix, tt.suffix, shim); err == nil {
				t.Fatal("production drift did not break the checked-in write proof binding")
			}
		})
	}
}

func validateDocumentationWriteShimForTest(prefix string, suffix string, shim string) error {
	shimStatement, err := documentationSQLSectionForPostgresTest(
		shim,
		"PREPARE write_probe(TEXT) AS",
		`\echo SEARCH_BASELINE_PLAN_AND_RESULT`,
	)
	if err != nil {
		return fmt.Errorf("checked-in documentation write shim: %w", err)
	}

	productionColumns, err := documentationSQLSectionForPostgresTest(
		prefix,
		"INSERT INTO fact_records (",
		") VALUES",
	)
	if err != nil {
		return fmt.Errorf("production fact upsert columns: %w", err)
	}
	shimColumns, err := documentationSQLSectionForPostgresTest(
		shimStatement,
		"INSERT INTO fact_records (",
		")\nSELECT",
	)
	if err != nil {
		return fmt.Errorf("checked-in fact upsert columns: %w", err)
	}
	productionColumns = normalizeDocumentationWriteSQLForTest(productionColumns)
	shimColumns = normalizeDocumentationWriteSQLForTest(shimColumns)
	if got := len(strings.Split(productionColumns, ",")); got != 17 {
		return fmt.Errorf("production fact upsert has %d columns, want 17", got)
	}
	if shimColumns != productionColumns {
		return fmt.Errorf("checked-in insert columns drifted from production\nshim:       %s\nproduction: %s", shimColumns, productionColumns)
	}

	shimSuffixAt := strings.Index(shimStatement, "ON CONFLICT")
	if shimSuffixAt < 0 {
		return fmt.Errorf("checked-in fact upsert is missing ON CONFLICT")
	}
	shimSuffix := strings.TrimSuffix(strings.TrimSpace(shimStatement[shimSuffixAt:]), ";")
	got := normalizeDocumentationWriteSQLForTest(shimSuffix)
	want := normalizeDocumentationWriteSQLForTest(suffix)
	if got != want {
		return fmt.Errorf("checked-in conflict clause drifted from production\nshim:       %s\nproduction: %s", got, want)
	}
	return nil
}

func documentationQueryPlanShimForPostgresTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(
		filepath.Dir(thisFile), "..", "..", "..", "..",
		"docs", "internal", "evidence", "5275-documentation-query-plan-shim.sql",
	)
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a fixed repository test fixture.
	if err != nil {
		t.Fatalf("read documentation query-plan shim: %v", err)
	}
	return string(raw)
}

func documentationSQLSectionForPostgresTest(sqlText string, start string, end string) (string, error) {
	startAt := strings.Index(sqlText, start)
	if startAt < 0 {
		return "", fmt.Errorf("missing start marker %q", start)
	}
	remaining := sqlText[startAt+len(start):]
	endAt := strings.Index(remaining, end)
	if endAt < 0 {
		return "", fmt.Errorf("missing end marker %q", end)
	}
	return strings.TrimSpace(remaining[:endAt]), nil
}

func normalizeDocumentationWriteSQLForTest(sqlText string) string {
	return strings.ToLower(strings.Join(strings.Fields(sqlText), " "))
}
