// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

func TestContentReaderInvestigateHardcodedSecretsReturnsClassifiedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		secretLinesReadinessResult(true),
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
				{"repo-1", "cmd/api/config_test.go", "go", int64(7), `password := "example-password"`, "password_literal"},
			},
		},
	})
	reader := NewContentReader(db)

	results, err := reader.InvestigateHardcodedSecrets(context.Background(), codequery.HardcodedSecretInvestigationRequest{
		RepoID:            "repo-1",
		Limit:             3,
		IncludeSuppressed: true,
	})
	if err != nil {
		t.Fatalf("InvestigateHardcodedSecrets() error = %v, want nil", err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if got, want := results[0].Severity, "high"; got != want {
		t.Fatalf("severity = %q, want %q", got, want)
	}
	if !results[1].Suppressed {
		t.Fatal("test fixture finding was not marked suppressed")
	}
	if got, want := results[1].Suppressions[0], "test_or_fixture_path"; got != want {
		t.Fatalf("suppression = %q, want %q", got, want)
	}
}

func TestContentReaderInvestigateHardcodedSecretsDoesNotDropFetchedSuppressedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		secretLinesReadinessResult(true),
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config_test.go", "go", int64(7), `password := "example-password"`, "password_literal"},
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
			},
		},
	})
	reader := NewContentReader(db)

	results, err := reader.InvestigateHardcodedSecrets(context.Background(), codequery.HardcodedSecretInvestigationRequest{
		RepoID:            "repo-1",
		Limit:             2,
		IncludeSuppressed: false,
	})
	if err != nil {
		t.Fatalf("InvestigateHardcodedSecrets() error = %v, want nil", err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("len(results) = %d, want %d; suppression filtering must happen before SQL LIMIT/OFFSET", got, want)
	}
	if !results[0].Suppressed {
		t.Fatal("first fetched row should still carry suppression metadata")
	}
}

func TestContentReaderInvestigateHardcodedSecretsPagesAfterSQLSuppressionFilter(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		secretLinesReadinessResult(true),
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
			},
			queryContainsInOrder: []string{
				"FROM content_file_secret_lines s",
				"AND s.repo_id = $1",
				"AND NOT s.suppressed",
				"LIMIT $2 OFFSET $3",
			},
		},
	})
	reader := NewContentReader(db)

	_, err := reader.InvestigateHardcodedSecrets(context.Background(), codequery.HardcodedSecretInvestigationRequest{
		RepoID: "repo-1",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("InvestigateHardcodedSecrets() error = %v, want nil", err)
	}
}

// secretLinesReadinessResult is the fake driver's answer to the readiness
// statement that precedes every hardcoded-secret read (#7125).
func secretLinesReadinessResult(ready bool) contentReaderQueryResult {
	return contentReaderQueryResult{
		columns:              []string{"exists"},
		rows:                 [][]driver.Value{{ready}},
		queryContainsInOrder: []string{"FROM content_file_secret_lines_state", "state = 'ready'"},
	}
}

// TestContentReaderInvestigateHardcodedSecretsServesLegacyScanUntilReady proves
// the gate: when the readiness statement says the side table is not ready the
// read runs the corpus scan over content_files, never the side table, and
// reports legacy_scan.
func TestContentReaderInvestigateHardcodedSecretsServesLegacyScanUntilReady(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		secretLinesReadinessResult(false),
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
			},
			queryContainsInOrder: []string{"WITH candidate_files AS", "FROM content_files", "regexp_split_to_table"},
		},
	})
	reader := NewContentReader(db)

	results, source, err := reader.InvestigateHardcodedSecretsWithSource(context.Background(), codequery.HardcodedSecretInvestigationRequest{
		RepoID: "repo-1",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("InvestigateHardcodedSecretsWithSource() error = %v, want nil", err)
	}
	if source != codequery.HardcodedSecretReadLegacyScan {
		t.Fatalf("source = %q, want %q", source, codequery.HardcodedSecretReadLegacyScan)
	}
	if len(results) != 1 || results[0].FindingKind != "api_token" {
		t.Fatalf("results = %+v, want the one classified row", results)
	}
}

func TestHardcodedSecretSuppressionRulesCoverSQLAndNotes(t *testing.T) {
	t.Parallel()

	sqlPredicate := hardcodedSecretSQLSuppressionPredicate()
	for _, rule := range hardcodedSecretSuppressionRules {
		for _, fragment := range rule.pathFragments {
			if !strings.Contains(sqlPredicate, hardcodedSecretSQLContains("f.relative_path", fragment)) {
				t.Fatalf("SQL predicate missing path fragment %q for reason %q", fragment, rule.reason)
			}
			if suppressions := hardcodedSecretSuppressions("src/"+fragment+"config.go", "token := \"real-token\""); !slices.Contains(suppressions, rule.reason) {
				t.Fatalf("Go suppressions for path fragment %q = %#v, want reason %q", fragment, suppressions, rule.reason)
			}
		}
		for _, fragment := range rule.lineFragments {
			if !strings.Contains(sqlPredicate, hardcodedSecretSQLContains("lines.line_text", fragment)) {
				t.Fatalf("SQL predicate missing line fragment %q for reason %q", fragment, rule.reason)
			}
			if suppressions := hardcodedSecretSuppressions("src/config.go", "token := \""+fragment+"-token\""); !slices.Contains(suppressions, rule.reason) {
				t.Fatalf("Go suppressions for line fragment %q = %#v, want reason %q", fragment, suppressions, rule.reason)
			}
		}
	}
}
