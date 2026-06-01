package postgres

import (
	"strings"
	"testing"
)

func TestListOSPackageAdvisoryTargetsQueryUsesActiveBoundedInstalledEvidence(t *testing.T) {
	query := listOSPackageAdvisoryTargetsQuery()
	for _, want := range []string{
		"fact.fact_kind = 'vulnerability.os_package'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"LOWER(COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) = ANY($1::text[])",
		"LIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("listOSPackageAdvisoryTargetsQuery missing %q:\n%s", want, query)
		}
	}
}

func TestListSBOMComponentAdvisoryTargetsQueryUsesAttachedComponentEvidence(t *testing.T) {
	query := listSBOMComponentAdvisoryTargetsQuery()
	for _, want := range []string{
		"component.fact_kind = 'sbom.component'",
		"attachment.fact_kind = 'reducer_sbom_attestation_attachment'",
		"component.payload->>'document_id' = attachment.payload->>'document_id'",
		"attachment.payload->>'attachment_status' IN",
		"WHEN 'golang' THEN 'go'",
		"WHERE ecosystem = ANY($1::text[])",
		"LIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("listSBOMComponentAdvisoryTargetsQuery missing %q:\n%s", want, query)
		}
	}
}
