package postgres

import (
	"strings"
	"testing"
)

func TestListActiveSBOMAttestationAttachmentFactsQueryIsDigestBoundedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind IN (",
		"'oci_registry.image_referrer'",
		"'reducer_container_image_identity'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.payload->>'subject_digest' = ANY($1::text[])",
		"fact.fact_id > $2",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, want) {
			t.Fatalf("listActiveSBOMAttestationAttachmentFactsQuery missing %q:\n%s", want, listActiveSBOMAttestationAttachmentFactsQuery)
		}
	}
}
