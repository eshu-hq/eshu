// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		// distro_version, arch, and generation_id are additive projections
		// (issue #5463/#5705): the reducer's cross-scope supply-chain-impact
		// evidence loader (supply_chain_impact_os_package_advisory_load.go)
		// reconstructs a full vulnerability.os_package envelope from this
		// target and needs all three to satisfy that fact kind's required-field
		// decode contract and the scanner-analysis-scope ScopeID+GenerationID
		// join.
		"fact.payload->>'distro_version'",
		"fact.payload->>'arch'",
		"fact.generation_id",
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
		"attachment.scope_id = component.scope_id",
		"component.payload->>'document_id' = attachment.payload->>'document_id'",
		"attachment_scope.active_generation_id = attachment.generation_id",
		"attachment_generation.status = 'active'",
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

func TestSBOMComponentAdvisoryTargetFromRowRejectsConflictingPURLVersion(t *testing.T) {
	target, ok := sbomComponentAdvisoryTargetFromRow(sbomComponentAdvisoryTargetRow{
		PURL:          "pkg:npm/left-pad@1.3.0",
		PackageName:   "left-pad",
		Version:       "9.9.9",
		DocumentID:    "sbom-doc-conflict",
		SubjectDigest: "sha256:7777",
		FactID:        "sbom-component-left-pad-conflict",
	})
	if ok {
		t.Fatalf("sbomComponentAdvisoryTargetFromRow() ok with target %#v, want conflicting PURL/version rejected", target)
	}
}

func TestSBOMComponentAdvisoryTargetFromRowUsesPURLVersionAsInstalledTruth(t *testing.T) {
	target, ok := sbomComponentAdvisoryTargetFromRow(sbomComponentAdvisoryTargetRow{
		PURL:          "pkg:npm/left-pad@1.3.0",
		PackageName:   "left-pad",
		Version:       "",
		DocumentID:    "sbom-doc",
		SubjectDigest: "sha256:7777",
		FactID:        "sbom-component-left-pad",
	})
	if !ok {
		t.Fatal("sbomComponentAdvisoryTargetFromRow() ok = false, want target from PURL version")
	}
	if got, want := target.Version, "1.3.0"; got != want {
		t.Fatalf("target.Version = %q, want PURL version %q", got, want)
	}
}
