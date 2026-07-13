package postgres

import (
	"strings"
	"testing"
)

// TestRepositoryFreshnessWebhookQueryFoldsRepositoryNameCase is the hermetic
// in-CI guard for the #5148 case-fold fix. GitHub webhook payloads carry
// repository_full_name with the owner's real casing while resolver-derived
// lookups arrive lowercased, so a case-sensitive equality silently
// under-reports genuine unobserved pushes. The live-DB proof for this fix is
// DSN-gated and skips in CI; this query-text assertion is what keeps a
// revert to bare equality from going green there.
func TestRepositoryFreshnessWebhookQueryFoldsRepositoryNameCase(t *testing.T) {
	t.Parallel()

	query := repositoryFreshnessWebhookQuery
	if !strings.Contains(query, "LOWER(repository_full_name) = LOWER(") {
		t.Fatalf("repositoryFreshnessWebhookQuery must case-fold both sides of the repository_full_name match, got:\n%s", query)
	}
	if strings.Contains(query, "repository_full_name = $") {
		t.Fatalf("repositoryFreshnessWebhookQuery must not compare repository_full_name case-sensitively, got:\n%s", query)
	}
}
