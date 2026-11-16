// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// cvssV31Critical is a valid CVSS v3.1 vector whose base score is 9.8.
const cvssV31Critical = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"

const productSeverityModifiedAt = "2026-02-01T00:00:00Z"

// productSeverityRow builds one vulnerability.cve observation for CVE-2026-0110
// with the given source, score, vector, and label.
func productSeverityRow(factID, source string, score float64, vector, label string) facts.Envelope {
	return vulnerabilityCVEFactWithProvenance(factID, "CVE-2026-0110", source, "CVE-2026-0110", score, vector, label, productSeverityModifiedAt)
}

// withStatus marks an observation's source status, as a rejected CNA record
// would carry it.
func withStatus(envelope facts.Envelope, status string) facts.Envelope {
	envelope.Payload["status"] = status
	return envelope
}

// Severity is chosen by tier first and source rank second (eshu-hq/eshu#10717):
// a reported score beats a vector-derived score, which beats a label, which
// beats nothing, whichever source carries it. The cve_list row ranks above NVD,
// so it is the representative in every case below.
func TestProductSeverityTiersAcrossSourceRanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rows        []facts.Envelope
		wantScore   float64
		wantSource  string
		wantLabel   string
		wantVectors string
	}{
		{
			name: "label-only cve_list loses to vector-only nvd, which carries a derived score",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, "", "LOW"),
				productSeverityRow("cve-b-nvd", "nvd", 0, cvssV31Critical, ""),
			},
			wantScore:   9.8,
			wantSource:  "derived_from_vector",
			wantVectors: cvssV31Critical,
		},
		{
			name: "vector-only cve_list loses to reported nvd 5.0",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, cvssV31Critical, ""),
				productSeverityRow("cve-b-nvd", "nvd", 5.0, "", "MEDIUM"),
			},
			wantScore: 5.0,
			wantLabel: "MEDIUM",
		},
		{
			name: "label-only cve_list loses to reported nvd",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, "", "LOW"),
				productSeverityRow("cve-b-nvd", "nvd", 7.5, "", "HIGH"),
			},
			wantScore: 7.5,
			wantLabel: "HIGH",
		},
		{
			name: "unscored cve_list loses to label-only nvd, which sets the bucket but no score",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, "", ""),
				productSeverityRow("cve-b-nvd", "nvd", 0, "", "MEDIUM"),
			},
			wantScore: 0,
			wantLabel: "MEDIUM",
		},
		{
			name: "two label-only rows keep the higher-ranked source's label",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, "", "LOW"),
				productSeverityRow("cve-b-nvd", "nvd", 0, "", "CRITICAL"),
			},
			wantScore: 0,
			wantLabel: "LOW",
		},
		{
			name: "two reported scores keep the higher-ranked source's score",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 9.1, "", "CRITICAL"),
				productSeverityRow("cve-b-nvd", "nvd", 7.5, "", "HIGH"),
			},
			wantScore: 9.1,
			wantLabel: "CRITICAL",
		},
		{
			name: "no observation carries severity",
			rows: []facts.Envelope{
				productSeverityRow("cve-a-list", "cve_list", 0, "", ""),
				productSeverityRow("cve-b-nvd", "nvd", 0, "", ""),
			},
			wantScore: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(tt.rows...))
			if len(findings) != 1 {
				t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
			}
			got := findings[0]
			if got.CVSSScore != tt.wantScore {
				t.Errorf("CVSSScore = %v, want %v", got.CVSSScore, tt.wantScore)
			}
			if string(got.SeverityScoreSource) != tt.wantSource {
				t.Errorf("SeverityScoreSource = %q, want %q", got.SeverityScoreSource, tt.wantSource)
			}
			if got.SeverityLabel != tt.wantLabel {
				t.Errorf("SeverityLabel = %q, want %q", got.SeverityLabel, tt.wantLabel)
			}
			if got.SeverityVector != tt.wantVectors {
				t.Errorf("SeverityVector = %q, want %q", got.SeverityVector, tt.wantVectors)
			}
		})
	}
}

// A rejected representative still names the finding but lends no severity, so
// a live scored sibling's score is used.
func TestProductRejectedRepresentativeLendsNoSeverityToScoredSibling(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		withStatus(productSeverityRow("cve-a-list", "cve_list", 9.9, "", "CRITICAL"), "Rejected"),
		productSeverityRow("cve-b-nvd", "nvd", 7.5, "", "HIGH"),
	))
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if got := findings[0].CVSSScore; got != 7.5 {
		t.Fatalf("CVSSScore = %v, want the live sibling's 7.5, not the rejected row's 9.9", got)
	}
	if got := findings[0].SeverityLabel; got != "HIGH" {
		t.Fatalf("SeverityLabel = %q, want the live sibling's HIGH", got)
	}
	if findings[0].AdvisoryRetraction != nil {
		t.Fatalf("AdvisoryRetraction = %#v, want nil: a live sibling still asserts the advisory", findings[0].AdvisoryRetraction)
	}
}

// Deliberate behavior change (eshu-hq/eshu#10717), matching the package path:
// a rejected scored ranked row never lends its score, even when the only live
// sibling is unscored. The finding reports score 0 and is not retracted,
// because a live observation still asserts the advisory.
func TestProductRejectedScoredRowYieldsZeroWithLiveUnscoredSibling(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		withStatus(productSeverityRow("cve-a-list", "cve_list", 9.9, "", "CRITICAL"), "Rejected"),
		productSeverityRow("cve-b-nvd", "nvd", 0, "", ""),
	))
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if got := findings[0].CVSSScore; got != 0 {
		t.Fatalf("CVSSScore = %v, want 0: a rejected row's 9.9 must not leak", got)
	}
	if got := findings[0].SeverityLabel; got != "" {
		t.Fatalf("SeverityLabel = %q, want empty: a rejected row's label must not leak", got)
	}
	if findings[0].AdvisoryRetraction != nil {
		t.Fatalf("AdvisoryRetraction = %#v, want nil with a live sibling", findings[0].AdvisoryRetraction)
	}
}

// Two equal-rank, equal-tier NVD rows must resolve the same way whichever
// order the observations arrive in: the lower FactID wins. The unscored
// cve_list representative forces the severity pass to choose between them.
func TestProductSeverityFactIDTieBreakIsOrderIndependent(t *testing.T) {
	t.Parallel()

	listRow := productSeverityRow("cve-a-list", "cve_list", 0, "", "")
	nvdLow := productSeverityRow("cve-b-nvd", "nvd", 5.0, "", "MEDIUM")
	nvdHigh := productSeverityRow("cve-c-nvd", "nvd", 8.0, "", "HIGH")

	forward := BuildSupplyChainImpactFindings(productScoredObservationFacts(listRow, nvdLow, nvdHigh))
	reverse := BuildSupplyChainImpactFindings(productScoredObservationFacts(listRow, nvdHigh, nvdLow))
	if len(forward) != 1 || len(reverse) != 1 {
		t.Fatalf("findings = %d and %d, want 1 each", len(forward), len(reverse))
	}
	if forward[0].CVSSScore != 5.0 || reverse[0].CVSSScore != 5.0 {
		t.Fatalf("CVSSScore = %v forward, %v reverse, want the lower FactID's 5.0 both ways", forward[0].CVSSScore, reverse[0].CVSSScore)
	}
	if !slices.Equal(forward[0].EvidenceFactIDs, reverse[0].EvidenceFactIDs) {
		t.Fatalf("EvidenceFactIDs = %v forward, %v reverse, want the same list", forward[0].EvidenceFactIDs, reverse[0].EvidenceFactIDs)
	}
}

// Within one tier, source rank orders the observations before FactID does: the
// CVE List's 6.0 beats NVD's 8.0 even though NVD's FactID sorts first and NVD
// is read first. The unscored GHSA representative leaves both to the severity
// pass.
func TestProductSeverityRanksSourcesBeforeFactIDWithinATier(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings(productScoredObservationFacts(
		productSeverityRow("f-ghsa", "ghsa", 0, "", ""),
		productSeverityRow("f-a-nvd", "nvd", 8.0, "", "HIGH"),
		productSeverityRow("f-list", "cve_list", 6.0, "", "MEDIUM"),
	))
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	if got := findings[0].CVSSScore; got != 6.0 {
		t.Fatalf("CVSSScore = %v, want the higher-ranked cve_list's 6.0, not NVD's 8.0", got)
	}
}

// BenchmarkProductSeverityObservation measures the severity selection over a
// group of n observations: the per-CVE cost the product path adds
// (eshu-hq/eshu#10717). Two shapes: "score-last", where only the last row
// carries a score so every row is read, and "vector-only", where every other
// row carries a CVSS vector and no reported score, so each read also parses a
// vector (cvss.DeriveSeverity parses one only when the reported score is zero).
func BenchmarkProductSeverityObservation(b *testing.B) {
	const vector = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"
	for _, shape := range []string{"score-last", "vector-only"} {
		for _, n := range []int{1, 8, 64} {
			b.Run(fmt.Sprintf("%s/observations=%d", shape, n), func(b *testing.B) {
				group := supplyChainCVEGroup{}
				for i := 0; i < n; i++ {
					observation := supplychainmodel.ImpactCVE{
						FactID:     fmt.Sprintf("cve-%03d", i),
						AdvisoryID: "CVE-2026-0110",
						Source:     "cve_list",
					}
					if shape == "vector-only" {
						observation.CVSSVector = vector
					}
					if i == n-1 {
						observation.Source = "nvd"
						observation.CVSSScore = 7.5
					}
					group.observations = append(group.observations, observation)
				}
				representative := group.observations[0]
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = group.severityObservation(representative)
				}
			})
		}
	}
}

// TestProductAllRejectedGroupKeepsIdentityButLendsNoSeverity pins the
// all-retracted case (eshu-hq/eshu#10764): with no live observation to lend
// severity, the representative still names the finding (its FactID stays) but
// its score, vector and label are blanked, the same outcome a live unscored
// sibling gives (see the rejected-representative tests above).
func TestProductAllRejectedGroupKeepsIdentityButLendsNoSeverity(t *testing.T) {
	t.Parallel()

	rejected := func(factID, source string, score float64) supplychainmodel.ImpactCVE {
		return supplychainmodel.ImpactCVE{
			FactID: factID, AdvisoryID: "CVE-2026-0111", Source: source,
			Status: "Rejected", CVSSScore: score,
		}
	}
	group := supplyChainCVEGroup{observations: []supplychainmodel.ImpactCVE{
		rejected("cve-a", "cve_list", 9.9),
		rejected("cve-b", "nvd", 7.5),
	}}
	representative := group.observations[0]
	got := group.severityObservation(representative)
	if got.FactID != representative.FactID {
		t.Fatalf("all-rejected group selected %s, want the representative %s to keep naming the finding",
			got.FactID, representative.FactID)
	}
	if got.CVSSScore != 0 || got.CVSSVector != "" || got.SeverityLabel != "" {
		t.Fatalf("all-rejected group lent severity score=%.1f vector=%q label=%q, want none",
			got.CVSSScore, got.CVSSVector, got.SeverityLabel)
	}
}

// TestProductRetractedSiblingNeverOutranksRetractedRepresentative pins the
// retracted-row skip in severityObservation. A retracted row and a retracted
// representative share the lowest tier, so without the skip a lower-ranked
// withdrawn row would take over the representative as the selected row. The
// representative stays the selected row and, being retracted, lends no
// severity of its own either.
func TestProductRetractedSiblingNeverOutranksRetractedRepresentative(t *testing.T) {
	t.Parallel()

	group := supplyChainCVEGroup{observations: []supplychainmodel.ImpactCVE{
		{FactID: "cve-a", AdvisoryID: "CVE-2026-0112", Source: "cve_list", Status: "Rejected", CVSSScore: 9.9},
		{FactID: "ghsa-b", AdvisoryID: "CVE-2026-0112", Source: "ghsa", WithdrawnAt: "2026-11-01T00:00:00Z", CVSSScore: 2.0},
	}}
	representative := group.observations[0]
	got := group.severityObservation(representative)
	if got.FactID != representative.FactID || got.CVSSScore != 0 {
		t.Fatalf("selected %s score %.1f, want the representative %s with no severity",
			got.FactID, got.CVSSScore, representative.FactID)
	}
}
