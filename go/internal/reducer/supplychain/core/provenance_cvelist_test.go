// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
)

// The CVE List v5 ranks as CNA enrichment (eshu-hq/eshu#9880): below the
// curated advisory sources, above NVD. A CVE that NVD has not yet scored
// shows severity from the CNA, and when both score, the primary-source CNA
// assessment wins the tie. Provenance keeps the deciding source.
func TestCVEListRanksBetweenCuratedSourcesAndNVD(t *testing.T) {
	t.Parallel()

	for _, ecosystem := range []string{"npm", "os", "rpm"} {
		if got := advisorySourcePriority(ecosystem, "cve_list"); got <= advisorySourcePriority(ecosystem, "osv") {
			t.Errorf("advisorySourcePriority(%q, cve_list) = %d, want below osv", ecosystem, got)
		}
		if got := advisorySourcePriority(ecosystem, "cve_list"); got >= advisorySourcePriority(ecosystem, "nvd") {
			t.Errorf("advisorySourcePriority(%q, cve_list) = %d, want above nvd", ecosystem, got)
		}
	}
}

func TestCVEListSeverityWinsWhenNVDDefers(t *testing.T) {
	t.Parallel()

	observations := []AdvisoryProvenanceObservation{
		{Source: "nvd", AdvisoryID: "CVE-2026-9102"},
		{Source: "cve_list", AdvisoryID: "CVE-2026-9102", SeverityScore: 7.5, SeverityLabel: "HIGH"},
	}
	selection := selectAdvisoryProvenance("npm", observations)
	if selection.SeveritySource != "cve_list" {
		t.Fatalf("SeveritySource = %q, want cve_list for the NVD-deferred CVE", selection.SeveritySource)
	}
	if selection.SeverityScore != 7.5 {
		t.Fatalf("SeverityScore = %v, want the CNA 7.5", selection.SeverityScore)
	}
}

func TestCVEListSeverityWinsScoredTieWithNVD(t *testing.T) {
	t.Parallel()

	observations := []AdvisoryProvenanceObservation{
		{Source: "nvd", AdvisoryID: "CVE-2026-9101", SeverityScore: 9.8, SeverityLabel: "CRITICAL"},
		{Source: "cve_list", AdvisoryID: "CVE-2026-9101", SeverityScore: 9.8, SeverityLabel: "CRITICAL"},
	}
	selection := selectAdvisoryProvenance("npm", observations)
	if selection.SeveritySource != "cve_list" {
		t.Fatalf("SeveritySource = %q, want the primary-source cve_list assessment", selection.SeveritySource)
	}
	foundNVD := false
	for _, alternate := range selection.AlternateSeverities {
		if alternate.Source == "nvd" {
			foundNVD = true
		}
	}
	if !foundNVD {
		t.Fatal("the NVD assessment must survive as an alternate severity in provenance")
	}
}
