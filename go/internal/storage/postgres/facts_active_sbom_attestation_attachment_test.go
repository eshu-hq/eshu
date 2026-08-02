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
		"legacy_facts AS MATERIALIZED",
		"COALESCE(fact.source_uri, '') AS source_uri",
		"COALESCE(fact.source_record_id, '') AS source_record_id",
		"$1::text[]",
		"$2 = ''",
		"LIMIT $3",
		"fact.fact_kind IN (",
		"'oci_registry.image_referrer'",
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
		"convert_to(fact.fact_id, 'UTF8') > convert_to($2, 'UTF8')",
		"ORDER BY convert_to(fact.fact_id, 'UTF8') ASC",
		"LIMIT $3",
	} {
		if !strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, want) {
			t.Fatalf("listActiveSBOMAttestationAttachmentFactsQuery missing %q:\n%s", want, listActiveSBOMAttestationAttachmentFactsQuery)
		}
	}
	if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, "'reducer_container_image_identity'") {
		t.Fatalf("legacy SBOM query must exclude identity rows:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
	for _, forbidden := range []string{"identity_facts AS MATERIALIZED", "UNION ALL", "container_image_identity_current_support_facts_for("} {
		if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, forbidden) {
			t.Fatalf("legacy SBOM query retains mixed identity operation %q:\n%s", forbidden, listActiveSBOMAttestationAttachmentFactsQuery)
		}
	}
	if !strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, "container_image_identity_current_support_facts_for(") {
		t.Fatalf("separate SBOM identity stream must use the support-grain function:\n%s", listCurrentContainerImageIdentitySupportFactsQuery)
	}
	if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, "container_image_identity_current_facts AS") {
		t.Fatalf("SBOM loader must use the bounded current-facts function:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
	if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, "container_image_identity_current_facts_for(") {
		t.Fatalf("SBOM reducer loader must preserve support-grain correlation:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
}
