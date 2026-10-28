// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"encoding/json"
	"testing"
)

func cov7FactRow(factID, factKind, payload string) EvidenceFactRow {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		panic(err)
	}
	return EvidenceFactRow{
		FactID:           factID,
		FactKind:         factKind,
		SourceConfidence: "reported",
		ObservedAt:       "2026-05-24T12:00:00Z",
		Payload:          decoded,
	}
}

// TestCov7BuildEvidenceRowsGroupsAndSkips proves grouping by canonical key,
// the empty-key skip, key sorting, and the unknown-kind no-op.
func TestCov7BuildEvidenceRowsGroupsAndSkips(t *testing.T) {
	t.Parallel()

	rows := BuildEvidenceRows([]EvidenceFactRow{
		cov7FactRow("b", "vulnerability.cve", `{"source":"osv","advisory_id":"OSV-9","cve_id":"CVE-2026-0002","aliases":[]}`),
		cov7FactRow("keyless", "vulnerability.cve", `{"source":"osv"}`),
		cov7FactRow("mystery", "vulnerability.something_new", `{"source":"osv","advisory_id":"OSV-9","cve_id":"CVE-2026-0002"}`),
		cov7FactRow("a", "vulnerability.cve", `{"source":"ghsa","advisory_id":"GHSA-aaaa-bbbb-cccc","cve_id":"CVE-2026-0001","aliases":[]}`),
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 grouped rows (keyless skipped)", len(rows))
	}
	if rows[0].Key != "CVE-2026-0001" || rows[1].Key != "CVE-2026-0002" {
		t.Fatalf("keys = [%q %q], want sorted CVE order", rows[0].Key, rows[1].Key)
	}
	if len(rows[1].Sources) != 1 {
		t.Fatalf("CVE-2026-0002 sources = %#v, want one (unknown kind contributes nothing)", rows[1].Sources)
	}
	if rows[1].LatestObservedAt != "2026-05-24T12:00:00Z" {
		t.Fatalf("LatestObservedAt = %q, want max observed", rows[1].LatestObservedAt)
	}
	if rows[1].SourceConfidence != "reported" {
		t.Fatalf("SourceConfidence = %q, want single reported label", rows[1].SourceConfidence)
	}
	if rows[1].SourceDisagreements != nil {
		t.Fatalf("disagreements = %#v, want nil when sources agree", rows[1].SourceDisagreements)
	}
	if len(rows) != 2 || BuildEvidenceRows(nil) == nil {
		t.Fatalf("empty build = %#v, want empty non-nil", BuildEvidenceRows(nil))
	}
}

// TestCov7BuildEvidenceRowsRawPaths proves the raw-payload contribution
// paths: affected products, EPSS/KEV observations, and references.
func TestCov7BuildEvidenceRowsRawPaths(t *testing.T) {
	t.Parallel()

	rows := BuildEvidenceRows([]EvidenceFactRow{
		cov7FactRow("product", "vulnerability.affected_product", `{
			"source":"nvd","cve_id":"CVE-2026-0003","criteria":"cpe:2.3:a:x",
			"match_criteria_id":"m1","vulnerable":true,
			"version_start_including":"1.0","version_end_excluding":"2.0",
			"source_configuration_operator":"OR","source_configuration_negate":true,
			"source_node_operator":"AND","source_node_negate":false}`),
		cov7FactRow("epss", "vulnerability.epss_score", `{
			"source":"first","cve_id":"CVE-2026-0003",
			"probability":"0.4","percentile":"0.9","score_date":"2026-05-01"}`),
		cov7FactRow("kev", "vulnerability.known_exploited", `{
			"source":"cisa","cve_id":"CVE-2026-0003","date_added":"2026-04-01",
			"required_action":"patch","due_date":"2026-06-01",
			"known_ransomware_campaign_use":"yes","cwes":["CWE-1","CWE-1"]}`),
		cov7FactRow("ref", "vulnerability.reference", `{
			"source":"nvd","advisory_id":"CVE-2026-0003","cve_id":"CVE-2026-0003",
			"reference_type":"advisory","url":"https://example.com/x"}`),
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if len(row.AffectedProducts) != 1 || !row.AffectedProducts[0].Vulnerable ||
		!row.AffectedProducts[0].SourceConfigurationNegate || row.AffectedProducts[0].VersionStartIncluding != "1.0" {
		t.Fatalf("products = %#v, want decoded CPE row", row.AffectedProducts)
	}
	if len(row.EPSS) != 1 || row.EPSS[0].Probability != "0.4" || row.EPSS[0].ScoreDate != "2026-05-01" {
		t.Fatalf("epss = %#v, want FIRST observation", row.EPSS)
	}
	if len(row.KEV) != 1 || row.KEV[0].DueDate != "2026-06-01" || len(row.KEV[0].CWEs) != 1 {
		t.Fatalf("kev = %#v, want deduped CISA observation", row.KEV)
	}
	if len(row.References) != 1 || row.References[0].URL != "https://example.com/x" {
		t.Fatalf("references = %#v, want raw reference row", row.References)
	}
}

// TestCov7BuildEvidenceRowsDropsUndecodable proves EPSS/KEV facts that fail
// required-field decode contribute nothing rather than zero-valued rows.
func TestCov7BuildEvidenceRowsDropsUndecodable(t *testing.T) {
	t.Parallel()

	rows := BuildEvidenceRows([]EvidenceFactRow{
		cov7FactRow("epss-bad", "vulnerability.epss_score", `{"source":"first","advisory_id":"GHSA-aaaa-bbbb-cccc","probability":"0.4"}`),
		cov7FactRow("kev-bad", "vulnerability.known_exploited", `{"source":"cisa","advisory_id":"GHSA-aaaa-bbbb-cccc"}`),
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 keyed group", len(rows))
	}
	if len(rows[0].EPSS) != 0 || len(rows[0].KEV) != 0 {
		t.Fatalf("row = %#v, want undecodable EPSS/KEV dropped", rows[0])
	}
}

// TestCov7BuildEvidenceRowsDisagreements proves multi-source severity,
// withdrawal, fixed-version, and range disagreements surface sorted, and
// mixed confidences label the row mixed.
func TestCov7BuildEvidenceRowsDisagreements(t *testing.T) {
	t.Parallel()

	ghsa := cov7FactRow("ghsa", "vulnerability.cve", `{
		"source":"ghsa","advisory_id":"GHSA-aaaa-bbbb-cccc","cve_id":"CVE-2026-0004",
		"aliases":[],"cvss_v4":"CVSS:4.0/AV:N"}`)
	osv := cov7FactRow("osv", "vulnerability.cve", `{
		"source":"osv","advisory_id":"OSV-1","cve_id":"CVE-2026-0004",
		"aliases":[],"severity":[{"type":"CVSS_V3","score":"low"}],
		"withdrawn_at":"2026-05-24T10:00:00Z"}`)
	osv.SourceConfidence = "unreviewed"
	pkg := cov7FactRow("pkg", "vulnerability.affected_package", `{
		"source":"osv","advisory_id":"OSV-1","cve_id":"CVE-2026-0004",
		"ecosystem":"npm","package_id":"pkg:npm/example",
		"affected_ranges":[{"type":"SEMVER"}],"fixed_versions":["1.2.4"]}`)
	glad := cov7FactRow("glad", "vulnerability.affected_package", `{
		"source":"glad","advisory_id":"GHSA-aaaa-bbbb-cccc","cve_id":"CVE-2026-0004",
		"ghsa_id":"GHSA-aaaa-bbbb-cccc","ecosystem":"npm","package_id":"pkg:npm/example",
		"affected_range":"<1.2.5","fixed_versions":["1.2.5"]}`)
	rows := BuildEvidenceRows([]EvidenceFactRow{ghsa, osv, pkg, glad})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	fields := map[string]bool{}
	for _, disagreement := range row.SourceDisagreements {
		fields[disagreement.Field] = true
	}
	for _, want := range []string{"severity", "withdrawn_status", "fixed_versions", "affected_ranges"} {
		if !fields[want] {
			t.Fatalf("disagreements = %#v, want %q", row.SourceDisagreements, want)
		}
	}
	if row.SourceConfidence != "mixed" {
		t.Fatalf("SourceConfidence = %q, want mixed", row.SourceConfidence)
	}
	if len(row.CVEIDs) != 1 || row.CVEIDs[0] != "CVE-2026-0004" {
		t.Fatalf("CVEIDs = %#v, want normalized CVE", row.CVEIDs)
	}
	if len(row.SourceIDs) != 3 {
		t.Fatalf("SourceIDs = %#v, want ghsa+osv+glad identities", row.SourceIDs)
	}
}

// TestCov7CanonicalKeyTable proves key priority (CVE, then GHSA, then
// advisory) and normalization across aliases and anchors.
func TestCov7CanonicalKeyTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"cve wins", map[string]any{"cve_id": "cve-2026-1", "ghsa_id": "GHSA-a-b-c"}, "CVE-2026-1"},
		{"ghsa fallback", map[string]any{"ghsa_id": "ghsa-a-b-c"}, "GHSA-a-b-c"},
		{"alias cve", map[string]any{"aliases": []any{"cve-2026-2"}}, "CVE-2026-2"},
		{"anchor ghsa", map[string]any{"correlation_anchors": []any{"ghsa-d-e-f"}}, "GHSA-d-e-f"},
		{"advisory id", map[string]any{"advisory_id": "OSV-1"}, "OSV-1"},
		{"ghsa_id key", map[string]any{"ghsa_id": "GHSA-x-y-z"}, "GHSA-x-y-z"},
		{"empty", map[string]any{"source": "osv"}, ""},
		{"blank", map[string]any{"cve_id": "  "}, ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := CanonicalKey(tc.payload); got != tc.want {
				t.Fatalf("CanonicalKey = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCov7AdvisoryIDNormalization proves CVE uppercasing, GHSA/OSV prefix
// preservation, whitespace trimming, and the non-ID drop.
func TestCov7AdvisoryIDNormalization(t *testing.T) {
	t.Parallel()

	acc := newAdvisoryEvidenceAccumulator("k")
	acc.addID("  cve-2026-1  ")
	acc.addID("ghsa-a-b-c")
	acc.addID("osv-2026-9")
	acc.addID("  ")
	acc.addID("not-an-id")
	finished := acc.finish()
	if len(finished.CVEIDs) != 1 || finished.CVEIDs[0] != "CVE-2026-1" {
		t.Fatalf("CVEIDs = %#v, want uppercased CVE", finished.CVEIDs)
	}
	if len(finished.GHSAIDs) != 1 || finished.GHSAIDs[0] != "GHSA-a-b-c" {
		t.Fatalf("GHSAIDs = %#v, want GHSA", finished.GHSAIDs)
	}
	if len(finished.OSVIDs) != 1 || finished.OSVIDs[0] != "OSV-2026-9" {
		t.Fatalf("OSVIDs = %#v, want OSV", finished.OSVIDs)
	}
	if got := normalizeAdvisoryDisplayID("cve-2026-7"); got != "CVE-2026-7" {
		t.Fatalf("display CVE = %q, want uppercased", got)
	}
	if got := normalizeAdvisoryDisplayID(" plain "); got != "plain" {
		t.Fatalf("display plain = %q, want trimmed", got)
	}
}

// TestCov7SeverityAndRangeSignatures proves every signature arm, including
// the severity-map JSON and the unmarshalable canonicalJSON fallback.
func TestCov7SeverityAndRangeSignatures(t *testing.T) {
	t.Parallel()

	labeled := severitySignature(SourceEvidence{SeverityLabel: "HIGH", CVSSScore: 7.5, CVSSVector: "vec"})
	if labeled != "HIGH 7.5 vec" {
		t.Fatalf("labeled signature = %q, want label/score/vector", labeled)
	}
	if got := severitySignature(SourceEvidence{CVSSVectorV4: "v4"}); got != "v4" {
		t.Fatalf("v4 signature = %q", got)
	}
	if got := severitySignature(SourceEvidence{CVSSVectorV3: "v3"}); got != "v3" {
		t.Fatalf("v3 signature = %q", got)
	}
	if got := severitySignature(SourceEvidence{CVSSVector: "v"}); got != "v" {
		t.Fatalf("vector signature = %q", got)
	}
	mapped := severitySignature(SourceEvidence{Severity: []map[string]string{{"type": "x"}}})
	if mapped == "" || mapped[0] != '[' {
		t.Fatalf("mapped signature = %q, want JSON", mapped)
	}
	if got := severitySignature(SourceEvidence{}); got != "" {
		t.Fatalf("empty signature = %q, want empty", got)
	}
	if got := affectedRangeSignature(AffectedPackage{AffectedRange: "<2.0"}); got != "<2.0" {
		t.Fatalf("range signature = %q", got)
	}
	ranged := affectedRangeSignature(AffectedPackage{AffectedRanges: []map[string]any{{"t": "s"}}})
	if ranged == "" || ranged[0] != '[' {
		t.Fatalf("ranges signature = %q, want JSON", ranged)
	}
	if got := affectedRangeSignature(AffectedPackage{}); got != "" {
		t.Fatalf("empty range signature = %q, want empty", got)
	}
	if got := canonicalJSON(func() {}); got == "" {
		t.Fatalf("unmarshalable canonicalJSON = empty, want Go-syntax fallback")
	}
}

// TestCov7DisagreementShaping proves the two-source gate, blank filtering,
// and the all-agree nil compaction.
func TestCov7DisagreementShaping(t *testing.T) {
	t.Parallel()

	solo := disagreement("severity", map[string]string{"a": "high"})
	if solo.Field != "" || len(solo.Values) != 0 {
		t.Fatalf("solo disagreement = %#v, want empty", solo)
	}
	blanked := disagreement("severity", map[string]string{"a": "high", "b": "  "})
	if blanked.Field != "" {
		t.Fatalf("blanked disagreement = %#v, want empty", blanked)
	}
	split := disagreement("fixed_versions", map[string]string{"b": "2.0", "a": "1.0"})
	if split.Field != "fixed_versions" || len(split.Values) != 2 || split.Values[0].Source != "a" {
		t.Fatalf("split disagreement = %#v, want source-sorted pair", split)
	}
	if compacted := compactDisagreements([]SourceDisagreement{{}, {}}); compacted != nil {
		t.Fatalf("compacted empties = %#v, want nil", compacted)
	}
	kept := compactDisagreements([]SourceDisagreement{{}, split})
	if len(kept) != 1 || kept[0].Field != "fixed_versions" {
		t.Fatalf("compacted = %#v, want only populated", kept)
	}
}
