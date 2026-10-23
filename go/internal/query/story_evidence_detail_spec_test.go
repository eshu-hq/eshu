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
// as applying to deployment artifact rows (#8798).
func TestOpenAPIStoryEvidenceDetailNamesArtifactException(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"an artifact row keeps resolved_id, the key get_relationship_evidence takes for the rest",
		"the investigation repositories report the 50 rows shipped, a lower bound, and the __truncated__ marker row says more exist",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing story evidence_detail contract %q", fragment)
		}
	}
}
