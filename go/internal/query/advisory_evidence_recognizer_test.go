// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

// recognizerVectors is the single vector table pinning the structural
// advisory-id recognizer (eshu-hq/eshu#10410, arbiter ruling 3.5). The same
// table exercises the Go predicate and the SQL predicate text: both must
// agree on every entry.
var recognizerVectors = []struct {
	value string
	want  bool
}{
	// Positive: every currently shipped OSV-family and distro/errata shape.
	{"CVE-2021-44228", true},
	{"GHSA-xxxx-yyyy-zzzz", true},
	{"OSV-2020-1", true},
	{"PYSEC-2020-1", true},
	{"GO-2021-0113", true},
	{"RUSTSEC-2021-0001", true},
	{"MAL-2023-1", true},
	{"DEBIAN-CVE-2023-1", true},
	{"DSA-1234-1", true},
	{"DLA-1234-1", true},
	{"ALPINE-CVE-2023-1", true},
	{"UBUNTU-CVE-2023-1", true},
	{"USN-1234-1", true},
	{"ALSA-2023:1234", true},
	{"EUVD-2025-1", true},
	// Positive: lowercase spellings normalize, so they must recognize.
	{"pysec-2020-1", true},
	{"debian-cve-2023-1", true},
	{"dsa-1234-1", true},
	{"alsa-2023:1234", true},
	{"Pysec-2020-1", true},
	// Negative: purls, ARNs, SHAs, versions, free text, paths, empties.
	{"pkg:npm/example", false},
	{"arn:aws:iam::123:role/x", false},
	{"da39a3ee5e6b4b0d3255bfef95601890afd80709", false},
	{"1.2.3", false},
	{"known fixed", false},
	{"a/b-c", false},
	{"", false},
	{"   ", false},
	{"CVE-", false},
	{"-CVE-2023-1", false},
	{"CVE 2023-1", false},
	{"PYSEC@2020-1", false},
}

func TestAdvisoryRouteRecognizerParity(t *testing.T) {
	t.Parallel()

	// The SQL predicate and the Go predicate share one pattern string. The
	// shipped query must carry it verbatim so a drift in either
	// implementation fails here, not in production.
	if !strings.Contains(listAdvisoryEvidenceQuery, advisory.AdvisoryRouteIDPattern) {
		t.Fatalf("listAdvisoryEvidenceQuery does not carry AdvisoryRouteIDPattern %q", advisory.AdvisoryRouteIDPattern)
	}
	for _, forbidden := range []string{
		"UPPER(TRIM(key_value)) LIKE 'CVE-%'",
		"UPPER(TRIM(key_value)) LIKE 'GHSA-%'",
		"UPPER(TRIM(key_value)) LIKE 'OSV-%'",
	} {
		if strings.Contains(listAdvisoryEvidenceQuery, forbidden) {
			t.Fatalf("listAdvisoryEvidenceQuery keeps prefix-allowlist predicate %q", forbidden)
		}
	}

	sqlPattern := regexp.MustCompile(advisory.AdvisoryRouteIDPattern)
	for _, vec := range recognizerVectors {
		vec := vec
		t.Run(vec.value, func(t *testing.T) {
			t.Parallel()
			if got := advisory.IsAdvisoryRouteID(vec.value); got != vec.want {
				t.Fatalf("IsAdvisoryRouteID(%q) = %v, want %v", vec.value, got, vec.want)
			}
			trimmed := strings.TrimSpace(vec.value)
			if got := sqlPattern.MatchString(trimmed); got != vec.want {
				t.Fatalf("SQL pattern on %q = %v, want %v", vec.value, got, vec.want)
			}
		})
	}
}

func TestAdvisoryRouteRecognizerMutationCheck(t *testing.T) {
	t.Parallel()

	// A widened pattern (slash admitted) must catch at least one negative
	// vector, and a narrowed CVE-only census must drop at least one
	// positive vector. Both prove the table above is sensitive to a
	// mutation on either side of the parity.
	widened := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9:/._-]*$`)
	widenedHit := false
	for _, vec := range recognizerVectors {
		if !vec.want && widened.MatchString(strings.TrimSpace(vec.value)) {
			widenedHit = true
		}
	}
	if !widenedHit {
		t.Fatal("widened pattern admits no negative vector; the table cannot catch over-expansion")
	}

	cveOnly := regexp.MustCompile(`^CVE-[A-Za-z0-9][A-Za-z0-9:._-]*$`)
	narrowedHit := false
	for _, vec := range recognizerVectors {
		if vec.want && !cveOnly.MatchString(strings.TrimSpace(vec.value)) {
			narrowedHit = true
		}
	}
	if !narrowedHit {
		t.Fatal("CVE-only pattern keeps every positive vector; the table cannot catch a prefix census")
	}
}

func TestAdvisoryEvidenceSeedKeysStaySingleHop(t *testing.T) {
	t.Parallel()

	// #10410 expands one hop over aliases and correlation anchors only.
	// upstream and related must never seed the matched legs.
	for _, forbidden := range []string{
		"payload->'upstream'",
		`payload->'related'`,
	} {
		if strings.Contains(listAdvisoryEvidenceQuery, forbidden) {
			t.Fatalf("listAdvisoryEvidenceQuery expands %q in seed_keys", forbidden)
		}
	}
	for _, want := range []string{
		"payload->'aliases'",
		"payload->'correlation_anchors'",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing single-hop source %q", want)
		}
	}
}

func TestNormalizeAdvisoryEvidenceFilterUppercasesWidenedPrefixes(t *testing.T) {
	t.Parallel()

	got := advisory.NormalizeEvidenceFilter(advisory.EvidenceFilter{
		ID:       "pysec-2020-1",
		CVEID:    "cve-2021-44228",
		AfterKey: "debian-cve-2023-1",
		Limit:    10,
	})
	if got.ID != "PYSEC-2020-1" {
		t.Fatalf("ID = %q, want PYSEC-2020-1", got.ID)
	}
	if got.CVEID != "CVE-2021-44228" {
		t.Fatalf("CVEID = %q, want CVE-2021-44228", got.CVEID)
	}
	if got.AfterKey != "DEBIAN-CVE-2023-1" {
		t.Fatalf("AfterKey = %q, want DEBIAN-CVE-2023-1", got.AfterKey)
	}

	got = advisory.NormalizeEvidenceFilter(advisory.EvidenceFilter{
		ID:    "dsa-1234-1",
		Limit: 10,
	})
	if got.ID != "DSA-1234-1" {
		t.Fatalf("ID = %q, want DSA-1234-1", got.ID)
	}

	// GHSA suffix case behavior is unchanged: prefix uppercased, suffix
	// preserved byte-identical.
	got = advisory.NormalizeEvidenceFilter(advisory.EvidenceFilter{
		ID:    "ghsa-aaaa-BBBB-cccc",
		Limit: 10,
	})
	if got.ID != "GHSA-aaaa-BBBB-cccc" {
		t.Fatalf("ID = %q, want GHSA-aaaa-BBBB-cccc", got.ID)
	}
}

func TestBuildAdvisoryEvidenceRowsWidensOsvBucket(t *testing.T) {
	t.Parallel()

	payload := func(fields map[string]any) map[string]any {
		base := map[string]any{
			"source":      "osv",
			"cve_id":      "CVE-2026-5101",
			"advisory_id": "PYSEC-2026-5101",
			"aliases":     []any{"CVE-2026-5101", "PYSEC-2026-5101", "DEBIAN-CVE-2026-5101"},
		}
		for key, value := range fields {
			base[key] = value
		}
		return base
	}
	facts := []advisory.EvidenceFactRow{
		{FactID: "widen-cve", FactKind: "vulnerability.cve", Payload: payload(nil)},
		{FactID: "widen-debian", FactKind: "vulnerability.cve", Payload: payload(map[string]any{
			"source":      "debian",
			"advisory_id": "DEBIAN-CVE-2026-5101",
		})},
	}
	rows := advisory.BuildEvidenceRows(facts)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 grouped row", len(rows))
	}
	row := rows[0]
	if row.CanonicalID != "CVE-2026-5101" {
		t.Fatalf("CanonicalID = %q, want CVE precedence over advisory_id", row.CanonicalID)
	}
	for _, want := range []string{"PYSEC-2026-5101", "DEBIAN-CVE-2026-5101"} {
		found := false
		for _, value := range row.OSVIDs {
			if value == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("osv_ids = %v, want entry %q", row.OSVIDs, want)
		}
	}
	for _, value := range row.OSVIDs {
		if strings.HasPrefix(value, "CVE-") || strings.HasPrefix(value, "GHSA-") {
			t.Fatalf("osv_ids = %v, must not carry CVE/GHSA ids", row.OSVIDs)
		}
	}
	if len(row.CVEIDs) != 1 || row.CVEIDs[0] != "CVE-2026-5101" {
		t.Fatalf("cve_ids = %v, want [CVE-2026-5101]", row.CVEIDs)
	}
	if len(row.SourceIDs) != 2 {
		t.Fatalf("source_ids = %v, want one per source record", row.SourceIDs)
	}
}
