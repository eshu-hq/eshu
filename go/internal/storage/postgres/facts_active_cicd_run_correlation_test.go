package postgres

import (
	"strings"
	"testing"
)

func TestListActiveCICDRunCorrelationFactsQueryIsDigestBoundedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'digest' = ANY($1::text[])",
		"($2 = '' OR fact.fact_id > $2)",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $3",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
	} {
		if !strings.Contains(listActiveCICDRunCorrelationFactsQuery, want) {
			t.Fatalf("listActiveCICDRunCorrelationFactsQuery missing %q", want)
		}
	}
}
