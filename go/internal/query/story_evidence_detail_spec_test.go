// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestOpenAPIStoryEvidenceDetailNamesArtifactException pins that the service
// story evidence_detail description says an artifact row keeps resolved_id as
// its handle to the rest, so the row-level truth labels it lists are not read
// as applying to deployment artifact rows, and that the omission totals state
// the shared pre-cap lower-bound rule (#8798).
func TestOpenAPIStoryEvidenceDetailNamesArtifactException(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"an artifact row keeps resolved_id, the key get_relationship_evidence takes for the rest",
		"truth.omissions lists each reduced family with its total. Each total is the story's own pre-cap count: a lower bound wherever the underlying read was itself bounded (a *_truncated flag or deployment_artifacts_* limitation is set, or the read stops at 50 rows with no flag), and not cut by the story's cap otherwise; the Context evidence budget reference lists the rule for each family",
		"investigation.repositories_with_evidence reports the rows it shipped (at most 50), so it is also a lower bound when repositories_considered ends with the __truncated__ marker",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing story evidence_detail contract %q", fragment)
		}
	}
	for _, banned := range []string{"The total is exact only when", "exact otherwise"} {
		if strings.Contains(OpenAPISpec(), banned) {
			t.Fatalf("OpenAPI spec still says %q about an omission total", banned)
		}
	}
}
