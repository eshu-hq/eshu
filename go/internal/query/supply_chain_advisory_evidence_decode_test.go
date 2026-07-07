// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// This file holds the advisory-evidence read-model tests that exercise the
// #4795 W2b typed factschema decode seams: a source fact whose payload fails
// required-field validation, or whose persisted schema_version names an
// unsupported major, must dead-letter (input_invalid) and be dropped from the
// grouped advisory row rather than contribute a zero-valued entry. They live
// beside the store/model tests in supply_chain_advisory_evidence_test.go and
// share its factRow helper; factRowWithSchema is local to these
// schema-version cases.

// TestBuildAdvisoryEvidenceRowsDropsSourceEvidenceMissingRequiredField proves
// the #4795 W2b typed-decode conversion: a vulnerability.cve fact missing its
// required advisory_id classifies input_invalid on decode
// (factschema.DecodeVulnerabilityCVE) and is DROPPED from the response's
// Sources list, rather than contributing a zero-valued AdvisorySourceEvidence
// row the way the pre-conversion raw StringVal read would have. The fact
// still has a valid cve_id so canonicalAdvisoryKey groups it (the decode
// failure is exercised inside addSourceEvidence, not filtered out earlier).
func TestBuildAdvisoryEvidenceRowsDropsSourceEvidenceMissingRequiredField(t *testing.T) {
	t.Parallel()

	rows := []advisoryEvidenceFactRow{
		factRow("cve-missing-advisory-id", "vulnerability.cve", `{
			"source": "osv",
			"cve_id": "CVE-2026-9001",
			"severity_label": "HIGH"
		}`),
	}

	got := buildAdvisoryEvidenceRows(rows)
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (grouped by cve_id)", len(got))
	}
	if len(got[0].Sources) != 0 {
		t.Fatalf("Sources = %#v, want empty: a vulnerability.cve fact missing advisory_id must classify input_invalid and drop, not zero-value", got[0].Sources)
	}
}

// TestBuildAdvisoryEvidenceRowsDropsAffectedPackageMissingRequiredField mirrors
// TestBuildAdvisoryEvidenceRowsDropsSourceEvidenceMissingRequiredField for
// vulnerability.affected_package (factschema.DecodeVulnerabilityAffectedPackage).
func TestBuildAdvisoryEvidenceRowsDropsAffectedPackageMissingRequiredField(t *testing.T) {
	t.Parallel()

	rows := []advisoryEvidenceFactRow{
		factRow("pkg-missing-advisory-id", "vulnerability.affected_package", `{
			"source": "osv",
			"cve_id": "CVE-2026-9002",
			"ecosystem": "npm",
			"package_id": "pkg:npm/example"
		}`),
	}

	got := buildAdvisoryEvidenceRows(rows)
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (grouped by cve_id)", len(got))
	}
	if len(got[0].AffectedPackages) != 0 {
		t.Fatalf("AffectedPackages = %#v, want empty: a vulnerability.affected_package fact missing advisory_id must classify input_invalid and drop, not zero-value", got[0].AffectedPackages)
	}
}

// TestBuildAdvisoryEvidenceRowsDropsUnsupportedSchemaMajor proves the read
// model threads each fact's persisted schema_version into the typed decode
// seam: a vulnerability.cve fact with an otherwise-valid payload but a
// non-1.x (future/unsupported) schema major dead-letters through the seam's
// unsupported-major branch and is dropped from Sources, rather than being
// silently decoded as v1. This fails before schema_version is threaded into
// the decode input (the caller previously ignored it and every fact decoded
// as v1).
func TestBuildAdvisoryEvidenceRowsDropsUnsupportedSchemaMajor(t *testing.T) {
	t.Parallel()

	rows := []advisoryEvidenceFactRow{
		factRowWithSchema("cve-future-major", "vulnerability.cve", "2.0.0", `{
			"source": "osv",
			"advisory_id": "GHSA-future-major",
			"cve_id": "CVE-2026-9100",
			"severity_label": "HIGH"
		}`),
	}

	got := buildAdvisoryEvidenceRows(rows)
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (grouped by cve_id)", len(got))
	}
	if len(got[0].Sources) != 0 {
		t.Fatalf("Sources = %#v, want empty: a schema_version=2.0.0 vulnerability.cve fact must dead-letter through the seam, not decode as v1", got[0].Sources)
	}
}

// TestBuildAdvisoryEvidenceRowsAbsentSchemaVersionDecodesAsV1 proves the
// version-less legacy path still works: a fact with an empty schema_version
// normalizes to queryDefaultSchemaMajorVersion and decodes as v1, so an
// otherwise-valid fact is not dropped just because its row carried no version.
func TestBuildAdvisoryEvidenceRowsAbsentSchemaVersionDecodesAsV1(t *testing.T) {
	t.Parallel()

	rows := []advisoryEvidenceFactRow{
		factRowWithSchema("cve-no-version", "vulnerability.cve", "", `{
			"source": "osv",
			"advisory_id": "GHSA-no-version",
			"cve_id": "CVE-2026-9101",
			"severity_label": "HIGH"
		}`),
	}

	got := buildAdvisoryEvidenceRows(rows)
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(got))
	}
	if len(got[0].Sources) != 1 {
		t.Fatalf("Sources = %#v, want 1: an empty schema_version must normalize to v1 and decode", got[0].Sources)
	}
}

// factRowWithSchema builds an advisoryEvidenceFactRow with an explicit
// persisted schema_version, for the schema-version dead-letter cases above.
func factRowWithSchema(factID string, factKind string, schemaVersion string, payload string) advisoryEvidenceFactRow {
	row := factRow(factID, factKind, payload)
	row.SchemaVersion = schemaVersion
	return row
}
