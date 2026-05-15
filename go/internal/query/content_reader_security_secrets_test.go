package query

import (
	"context"
	"database/sql/driver"
	"slices"
	"strings"
	"testing"
)

func TestContentReaderInvestigateHardcodedSecretsReturnsClassifiedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
				{"repo-1", "cmd/api/config_test.go", "go", int64(7), `password := "example-password"`, "password_literal"},
			},
		},
	})
	reader := NewContentReader(db)

	results, err := reader.investigateHardcodedSecrets(context.Background(), hardcodedSecretInvestigationRequest{
		RepoID:            "repo-1",
		Limit:             3,
		IncludeSuppressed: true,
	})
	if err != nil {
		t.Fatalf("investigateHardcodedSecrets() error = %v, want nil", err)
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
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config_test.go", "go", int64(7), `password := "example-password"`, "password_literal"},
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
			},
		},
	})
	reader := NewContentReader(db)

	results, err := reader.investigateHardcodedSecrets(context.Background(), hardcodedSecretInvestigationRequest{
		RepoID:            "repo-1",
		Limit:             2,
		IncludeSuppressed: false,
	})
	if err != nil {
		t.Fatalf("investigateHardcodedSecrets() error = %v, want nil", err)
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
		{
			columns: []string{"repo_id", "relative_path", "language", "line_number", "line_text", "finding_kind"},
			rows: [][]driver.Value{
				{"repo-1", "cmd/api/config.go", "go", int64(42), `token := "sk_live_1234567890abcdef"`, "api_token"},
			},
			queryContainsInOrder: []string{
				"AS suppressed",
				"AND ($4 OR NOT suppressed)",
				"LIMIT $5 OFFSET $6",
			},
		},
	})
	reader := NewContentReader(db)

	_, err := reader.investigateHardcodedSecrets(context.Background(), hardcodedSecretInvestigationRequest{
		RepoID: "repo-1",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("investigateHardcodedSecrets() error = %v, want nil", err)
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
