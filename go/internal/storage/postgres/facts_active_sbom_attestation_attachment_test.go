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
		"container_image_identity_current_support_facts_for(",
		"$1::text[]",
		"'{}'::text[]",
		"$2::text",
		"$3::integer",
		"UNION ALL",
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
	legacyStart := strings.Index(listActiveSBOMAttestationAttachmentFactsQuery, "legacy_facts AS MATERIALIZED")
	identityStart := strings.Index(listActiveSBOMAttestationAttachmentFactsQuery, "identity_facts AS MATERIALIZED")
	if legacyStart < 0 || identityStart < 0 || identityStart <= legacyStart {
		t.Fatalf("SBOM loader must keep legacy and canonical identity sources distinct:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
	legacyBranch := listActiveSBOMAttestationAttachmentFactsQuery[legacyStart:identityStart]
	if strings.Contains(legacyBranch, "'reducer_container_image_identity'") {
		t.Fatalf("legacy SBOM branch must exclude v2 identity rows:\n%s", legacyBranch)
	}
	if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, "container_image_identity_current_facts AS") {
		t.Fatalf("SBOM loader must use the bounded current-facts function:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
	if strings.Contains(listActiveSBOMAttestationAttachmentFactsQuery, "container_image_identity_current_facts_for(") {
		t.Fatalf("SBOM reducer loader must preserve support-grain correlation:\n%s", listActiveSBOMAttestationAttachmentFactsQuery)
	}
}
