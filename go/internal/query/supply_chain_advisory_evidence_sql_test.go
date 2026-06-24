// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestAdvisoryEvidenceQuerySeedsFromFactRecordsNotBroadActiveSet(t *testing.T) {
	t.Parallel()

	if strings.Contains(listAdvisoryEvidenceQuery, "FROM active") {
		t.Fatalf("advisory evidence query must not seed or return from a broad active CTE:\n%s", listAdvisoryEvidenceQuery)
	}
	seedStart := strings.Index(listAdvisoryEvidenceQuery, "seed AS (")
	matchesStart := strings.Index(listAdvisoryEvidenceQuery, "matched_facts AS (")
	if seedStart < 0 || matchesStart < 0 {
		t.Fatalf("query must keep selector seed and matched facts as explicit CTEs:\n%s", listAdvisoryEvidenceQuery)
	}
	if seedStart > matchesStart {
		t.Fatalf("query must derive seed keys before matching facts:\n%s", listAdvisoryEvidenceQuery)
	}
	for _, want := range []string{
		"JOIN fact_records AS fact",
		"fact.fact_kind = ANY($1::text[])",
		"fact.fact_kind IN (",
		"'vulnerability.reference'",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"LIMIT $5",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
}

func TestAdvisoryEvidenceQueryUsesIndexableJSONBPredicates(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"FROM jsonb_array_elements_text",
		"UPPER(payload->>'cve_id')",
		"UPPER(payload->>'advisory_id')",
		"UPPER(payload->>'ghsa_id')",
		"ANY(keys.values)",
		"fact.payload->'aliases' ? lookup.value",
		"fact.payload->'correlation_anchors' ? lookup.value",
	} {
		if strings.Contains(listAdvisoryEvidenceQuery, forbidden) {
			t.Fatalf("listAdvisoryEvidenceQuery contains unbounded predicate %q:\n%s", forbidden, listAdvisoryEvidenceQuery)
		}
	}
	for _, want := range []string{
		"JOIN LATERAL unnest(keys.values) AS lookup(value) ON TRUE",
		"fact.payload->>'cve_id' = lookup.value",
		"fact.payload->>'advisory_id' = lookup.value",
		"fact.payload->>'ghsa_id' = lookup.value",
		"key_source = 'identity'",
		"key_source = 'alias'",
		"UPPER(TRIM(key_value)) LIKE 'CVE-%'",
		"fact.payload->>'package_id' = pkg.value",
		"fact.payload->>'purl' = pkg.value",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
}

func TestAdvisoryEvidenceLookupIDsStaySeparateFromPackageScope(t *testing.T) {
	t.Parallel()

	got := advisoryEvidenceLookupIDs(AdvisoryEvidenceFilter{
		CVEID:      " cve-2026-0002 ",
		AdvisoryID: " GHSA-aaaa-bbbb-cccc ",
		PackageID:  "pkg:npm/example",
	})
	if joined := strings.Join(got, ","); joined != "CVE-2026-0002,GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("advisoryEvidenceLookupIDs() = %#v, want only normalized advisory ids", got)
	}
}
