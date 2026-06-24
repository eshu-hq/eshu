// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestListActiveCICDRunCorrelationFactsQueryIsArtifactBoundedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'digest' = ANY($1::text[])",
		"fact.payload->>'image_ref' = ANY($2::text[])",
		"($3 = '' OR fact.fact_id > $3)",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $4",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
	} {
		if !strings.Contains(listActiveCICDRunCorrelationFactsQuery, want) {
			t.Fatalf("listActiveCICDRunCorrelationFactsQuery missing %q", want)
		}
	}
}
