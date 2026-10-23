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
// as applying to deployment artifact rows, and that the omission totals are
// lower bounds for the families whose own read is bounded (#8798).
func TestOpenAPIStoryEvidenceDetailNamesArtifactException(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"an artifact row keeps resolved_id, the key get_relationship_evidence takes for the rest",
		"truth.omissions lists each reduced family with its total, the count the story held before its own 50-row cap",
		"for dependents, consumer_repositories, provisioning_source_chains, and uncorrelated_cloud_resources it is a lower bound when the matching *_truncated flag is set",
		"cloud_resources is read at most 50 rows with no flag, so its total never passes 50",
		"investigation.repositories_with_evidence reports the rows it shipped (at most 50), a lower bound when repositories_considered ends with the __truncated__ marker",
	} {
		if !strings.Contains(OpenAPISpec(), fragment) {
			t.Fatalf("OpenAPI spec missing story evidence_detail contract %q", fragment)
		}
	}
}
