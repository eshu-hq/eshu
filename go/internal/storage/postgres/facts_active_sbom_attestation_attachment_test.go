// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"'sbom.document'",
		"'sbom.component'",
		"'sbom.dependency_relationship'",
		"'sbom.external_reference'",
		"'attestation.statement'",
		"'attestation.slsa_provenance'",
		"fact.payload->>'statement_id' = ANY($1::text[])",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.payload->>'subject_digest' = ANY($1::text[])",
		"fact.payload->>'referrer_digest' = ANY($1::text[])",
		"fact.payload->>'document_digest' = ANY($1::text[])",
		"fact.payload->>'document_id' = ANY($1::text[])",
		"fact.fact_id > $2",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, want) {
			t.Fatalf("listActiveSBOMAttestationAttachmentFactsQuery missing %q:\n%s", want, listActiveSBOMAttestationAttachmentFactsQuery)
		}
	}
}
