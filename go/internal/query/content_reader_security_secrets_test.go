package query

import (
	"context"
	"database/sql/driver"
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
