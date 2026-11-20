// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// R-B5, R-B10 and R-B29 of eshu-hq/eshu#10735 (binding 2026-11-17 ruling):
// the NVD product path must use the newest NVD copy before matching.
//
// The bug: appendSupplyChainImpactIndexFindings builds products from every
// scope while representative() picks by source rank then lowest FactID, so a
// baseline copy and a later window copy of one CVE let the stale copy supply
// the product set, the representative, and the severity. Retraction already
// goes through the newest-copy rule; severity and the product set do not.
//
// The rule (ARB-4 option (a') as re-scoped by the 2026-11-17 ruling; split
// records cannot exist, so R-B23 is dropped):
//
//   - elect one winner per (source, advisory id) across all scopes with the
//     unchanged newestCopyAfter election (greatest SourceUpdatedAt, then live
//     over retracted, then greater FactID);
//   - admit the winner plus every same-key copy it beats only on FactID, so
//     an equal-lastModified tie keeps every tied copy's products and no
//     StableID hash decides the set;
//   - keep a product only if its own scope and generation holds an admitted
//     copy; orphans drop (NVD is the only affected_product emitter, so an
//     orphan is a broken commit);
//   - kept products fold by (criteria, match_criteria_id) through the finding
//     identity the path already coalesces on;
//   - two guards keep today's behavior (all products stay): the key is in the
//     unreadable-CVE set, or the pass is truncated in a way that can cut cve
//     facts.
//
// Fixtures use unzoned NVD-shaped modified_at values (a zoned fixture would
// pass while #10741 was unfixed; #10741 is closed).

const (
	newestCopyProductBaselineStamp = "2026-10-01T00:00:00.000"
	newestCopyProductWindowStamp   = "2026-11-01T00:00:00.000"

	newestCopyProductCriteriaA = "cpe:2.3:a:example:alpha:1.0:*:*:*:*:*:*:*"
	newestCopyProductCriteriaB = "cpe:2.3:a:example:beta:2.0:*:*:*:*:*:*:*"
	newestCopyProductMatchA    = "match-10735-alpha"
	newestCopyProductMatchB    = "match-10735-beta"
)

// newestCopyProduct builds the affected_product fact one NVD copy's scope
// carries, so a copy is a CVE fact plus its product fact. The product payload
// carries no modified_at; the copy ties to its scope and generation.
func newestCopyProduct(factID, scopeID, cveID, criteria, matchCriteriaID string) facts.Envelope {
	envelope := vulnerabilityAffectedProductFact(factID, cveID, criteria, matchCriteriaID, true)
	envelope.ScopeID = scopeID
	envelope.GenerationID = "generation:" + scopeID
	return envelope
}

// newestCopyNVDProductCVE builds one scope's NVD copy of a product-group CVE
// with a source-reported score, label, and unzoned NVD-shaped stamp.
func newestCopyNVDProductCVE(factID, scopeID, cveID string, score float64, label, modifiedAt string) facts.Envelope {
	envelope := newestCopyCVE(factID, scopeID, cveID, "nvd", cveID, score, modifiedAt, "", "")
	envelope.Payload["severity_label"] = label
	return envelope
}

// newestCopyProductAnchors anchors every named criteria through one shared
// SBOM document, attachment, and image identity, so each classified product
// has the owned anchor the findings gate requires.
func newestCopyProductAnchors(criteria ...string) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, len(criteria)+3)
	for i, criterion := range criteria {
		envelopes = append(envelopes, sbomComponentCPEImpactFact(
			"component-10735-"+string(rune('a'+i)), "doc-10735", criterion))
	}
	return append(envelopes,
		sbomAttachmentImpactFact("attachment-10735", "doc-10735", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-10735", testImpactSubjectDigest, testImpactRepositoryID),
	)
}

// productFindingCriteria returns the sorted product criteria of every
// finding, so a product-set assertion reads the admitted set directly.
func productFindingCriteria(findings []SupplyChainImpactFinding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.ProductCriteria)
	}
	slices.Sort(out)
	return out
}

// TestSupplyChainImpactBaselineThenWindowReadsWindowCopy (R-B5) pins I1 to I3
// for the product path: with a baseline copy and a newer window copy of one
// NVD CVE, the window copy alone supplies the product set, the
// representative, and the severity. It runs in both commit orders, and with
// the baseline FactID sorting below the window FactID and above it, so the
// result depends on neither fact order nor StableID hash order (I3).
//
// RED before the fix: both scopes' products classify (two findings), and the
// representative is the lowest FactID, so the baseline stamp and score win
// whenever the baseline FactID sorts first.
func TestSupplyChainImpactBaselineThenWindowReadsWindowCopy(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9101"
	factOrders := []struct {
		name         string
		baselineFact string
		windowFact   string
	}{
		// The baseline FactID sorts below the window FactID: the old
		// lowest-FactID representative reads the stale copy.
		{name: "baseline fact id first", baselineFact: "cve-10735-a-base", windowFact: "cve-10735-b-win"},
		// The baseline FactID sorts above the window FactID: the old
		// representative already reads the window copy, but the stale
		// product still classifies.
		{name: "baseline fact id last", baselineFact: "cve-10735-b-base", windowFact: "cve-10735-a-win"},
	}
	commitOrders := []struct {
		name         string
		baselineLead bool
	}{
		{name: "baseline committed first", baselineLead: true},
		{name: "window committed first", baselineLead: false},
	}
	wantIDs := make(map[string]string)
	for _, fo := range factOrders {
		for _, order := range commitOrders {
			name := fo.name + "/" + order.name
			// Sequential on purpose: every subtest records its finding id in
			// the shared wantIDs map the order-independence check reads below.
			t.Run(name, func(t *testing.T) {
				baseline := []facts.Envelope{
					newestCopyNVDProductCVE(fo.baselineFact, "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
					newestCopyProduct("product-10735-a", "scope:nvd-baseline", cveID, newestCopyProductCriteriaA, newestCopyProductMatchA),
				}
				window := []facts.Envelope{
					newestCopyNVDProductCVE(fo.windowFact, "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
					newestCopyProduct("product-10735-b", "scope:nvd-window", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
				}
				envelopes := append(append([]facts.Envelope{}, baseline...), window...)
				if !order.baselineLead {
					envelopes = append(append([]facts.Envelope{}, window...), baseline...)
				}
				envelopes = append(envelopes, newestCopyProductAnchors(newestCopyProductCriteriaA, newestCopyProductCriteriaB)...)

				findings := BuildSupplyChainImpactFindings(envelopes)
				if got := len(findings); got != 1 {
					t.Fatalf("len(findings) = %d, want 1 (the window copy's product only): %v", got, productFindingCriteria(findings))
				}
				got := findings[0]
				if got.ProductCriteria != newestCopyProductCriteriaB {
					t.Fatalf("ProductCriteria = %q, want the window copy's %q", got.ProductCriteria, newestCopyProductCriteriaB)
				}
				if got.CVSSScore != 9.8 {
					t.Fatalf("CVSSScore = %v, want 9.8 from the window copy", got.CVSSScore)
				}
				if got.SeverityLabel != "CRITICAL" {
					t.Fatalf("SeverityLabel = %q, want CRITICAL from the window copy", got.SeverityLabel)
				}
				if got.AdvisoryUpdatedAt != newestCopyProductWindowStamp {
					t.Fatalf("AdvisoryUpdatedAt = %q, want the window stamp %q", got.AdvisoryUpdatedAt, newestCopyProductWindowStamp)
				}
				if !slices.Contains(got.EvidenceFactIDs, fo.windowFact) {
					t.Fatalf("EvidenceFactIDs = %v, want the window copy %q", got.EvidenceFactIDs, fo.windowFact)
				}
				if slices.Contains(got.EvidenceFactIDs, fo.baselineFact) {
					t.Fatalf("EvidenceFactIDs = %v, must not name the superseded baseline copy %q", got.EvidenceFactIDs, fo.baselineFact)
				}
				// The finding id does not move with commit or fact order.
				wantIDs[name] = supplyChainImpactFindingID(got)
			})
		}
	}
	for name, id := range wantIDs {
		if id != wantIDs["baseline fact id first/baseline committed first"] {
			t.Fatalf("finding id for %q = %q, want the same id in every order", name, id)
		}
	}
}

// TestProductPathReadsNewestNVDCopyOnly (R-B10) pins I1: the elected copy
// supplies the representative and the severity, and a product present only in
// a losing copy forms no finding (I2). The retracted-newer subtest pins the
// live-over-retracted election rung on the product set: the newer Rejected
// copy admits its own scope's products while the older live copy's products
// drop, and the finding carries the retraction.
func TestProductPathReadsNewestNVDCopyOnly(t *testing.T) {
	t.Parallel()

	t.Run("live newer wins", func(t *testing.T) {
		t.Parallel()

		const cveID = "CVE-2026-9102"
		envelopes := []facts.Envelope{
			newestCopyNVDProductCVE("cve-10735-a-base", "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
			newestCopyProduct("product-10735-a", "scope:nvd-baseline", cveID, newestCopyProductCriteriaA, newestCopyProductMatchA),
			newestCopyNVDProductCVE("cve-10735-b-win", "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
			newestCopyProduct("product-10735-b", "scope:nvd-window", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
		}
		envelopes = append(envelopes, newestCopyProductAnchors(newestCopyProductCriteriaA, newestCopyProductCriteriaB)...)
		findings := BuildSupplyChainImpactFindings(envelopes)
		if got := len(findings); got != 1 {
			t.Fatalf("len(findings) = %d, want 1 (the newest copy's product only): %v", got, productFindingCriteria(findings))
		}
		got := findings[0]
		if got.ProductCriteria != newestCopyProductCriteriaB || got.MatchCriteriaID != newestCopyProductMatchB {
			t.Fatalf("finding reads (%q, %q), want the window copy's (%q, %q)",
				got.ProductCriteria, got.MatchCriteriaID, newestCopyProductCriteriaB, newestCopyProductMatchB)
		}
		if got.CVSSScore != 9.8 || got.SeverityLabel != "CRITICAL" || got.AdvisoryUpdatedAt != newestCopyProductWindowStamp {
			t.Fatalf("finding reads score=%v label=%q updated=%q, want the window copy's 9.8/CRITICAL/%q",
				got.CVSSScore, got.SeverityLabel, got.AdvisoryUpdatedAt, newestCopyProductWindowStamp)
		}
	})

	t.Run("retracted newer admits its own scope only", func(t *testing.T) {
		t.Parallel()

		const cveID = "CVE-2026-9103"
		window := newestCopyNVDProductCVE("cve-10735-b-win", "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp)
		window.Payload["status"] = "Rejected"
		retracted := []facts.Envelope{
			newestCopyNVDProductCVE("cve-10735-a-base", "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
			newestCopyProduct("product-10735-a", "scope:nvd-baseline", cveID, newestCopyProductCriteriaA, newestCopyProductMatchA),
			window,
			newestCopyProduct("product-10735-b", "scope:nvd-window", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
		}
		retracted = append(retracted, newestCopyProductAnchors(newestCopyProductCriteriaA, newestCopyProductCriteriaB)...)
		finding := buildRetractionFinding(t, retracted...)
		if finding.ProductCriteria != newestCopyProductCriteriaB {
			t.Fatalf("ProductCriteria = %q, want the retracted winner's %q (the live loser's product drops)",
				finding.ProductCriteria, newestCopyProductCriteriaB)
		}
		if finding.AdvisoryRetraction == nil || finding.AdvisoryRetraction.ReasonCode != AdvisoryRetractionRejected {
			t.Fatalf("AdvisoryRetraction = %#v, want the newer copy's rejection", finding.AdvisoryRetraction)
		}
		if got := finding.Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
			t.Fatalf("Suppression.State = %q, want advisory_retracted", got)
		}
	})
}

// TestProductPathAdmitsTiedCopiesAndDropsOrphans (R-B29) pins the tie rule and
// the orphan rule: two copies tied on the winner instant (equal
// SourceUpdatedAt, same lifecycle) both admit their scopes' products, so no
// FactID hash decides the set; a product from a losing copy and a product
// whose scope holds no same-key cve fact form no finding (I2). It runs in
// both scope-commit orders (I3).
//
// The seeded violation it guards against is an admission that falls back to
// the winner's scope and drops the tied parts' products: the union of the
// tied scopes' products must survive.
func TestProductPathAdmitsTiedCopiesAndDropsOrphans(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9104"
	const (
		criteriaT1 = "cpe:2.3:a:example:tied-one:3.0:*:*:*:*:*:*:*"
		criteriaT2 = "cpe:2.3:a:example:tied-two:3.1:*:*:*:*:*:*:*"
		criteriaT3 = "cpe:2.3:a:example:orphan:3.2:*:*:*:*:*:*:*"
		criteriaT4 = "cpe:2.3:a:example:loser:3.3:*:*:*:*:*:*:*"
	)
	for _, order := range []struct {
		name      string
		tiedFirst bool
	}{
		{name: "tied scopes committed first", tiedFirst: true},
		{name: "losing scope committed first", tiedFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()

			tied := []facts.Envelope{
				newestCopyNVDProductCVE("cve-10735-tie-a", "scope:nvd-tie-a", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
				newestCopyProduct("product-10735-t1", "scope:nvd-tie-a", cveID, criteriaT1, "match-10735-t1"),
				newestCopyNVDProductCVE("cve-10735-tie-b", "scope:nvd-tie-b", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
				newestCopyProduct("product-10735-t2", "scope:nvd-tie-b", cveID, criteriaT2, "match-10735-t2"),
			}
			rest := []facts.Envelope{
				newestCopyProduct("product-10735-t3", "scope:nvd-orphan", cveID, criteriaT3, "match-10735-t3"),
				newestCopyNVDProductCVE("cve-10735-older", "scope:nvd-older", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
				newestCopyProduct("product-10735-t4", "scope:nvd-older", cveID, criteriaT4, "match-10735-t4"),
			}
			envelopes := append(append([]facts.Envelope{}, tied...), rest...)
			if !order.tiedFirst {
				envelopes = append(append([]facts.Envelope{}, rest...), tied...)
			}
			envelopes = append(envelopes, newestCopyProductAnchors(criteriaT1, criteriaT2, criteriaT3, criteriaT4)...)

			findings := BuildSupplyChainImpactFindings(envelopes)
			if got, want := productFindingCriteria(findings), []string{criteriaT1, criteriaT2}; !slices.Equal(got, want) {
				t.Fatalf("product criteria = %v, want the tied copies' union %v", got, want)
			}
		})
	}
}

// TestProductPathReportsDroppedProducts pins the per-pass dropped-product
// telemetry for the newest-copy admission: the count rides the
// products_dropped_by_newest_copy sub-signal (0 on a clean pass, so an
// operator can tell a clean pass from an unreported one), and both guards
// report 0 because every product stays — the unreadable-CVE key guard and
// the truncated-load guard.
func TestProductPathReportsDroppedProducts(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9105"
	dropping := []facts.Envelope{
		newestCopyNVDProductCVE("cve-10735-a-base", "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
		newestCopyProduct("product-10735-a", "scope:nvd-baseline", cveID, newestCopyProductCriteriaA, newestCopyProductMatchA),
		newestCopyNVDProductCVE("cve-10735-b-win", "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
		newestCopyProduct("product-10735-b", "scope:nvd-window", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
	}
	dropping = append(dropping, newestCopyProductAnchors(newestCopyProductCriteriaA, newestCopyProductCriteriaB)...)

	t.Run("admission drops the loser", func(t *testing.T) {
		t.Parallel()

		_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(dropping)
		if err != nil {
			t.Fatalf("build error = %v", err)
		}
		if stats.productsDroppedByNewestCopy != 1 {
			t.Fatalf("productsDroppedByNewestCopy = %d, want 1 (the baseline copy's product)", stats.productsDroppedByNewestCopy)
		}
		subSignals := map[string]float64{}
		SupplyChainImpactHandler{}.recordBuildStats(context.Background(), stats, subSignals)
		// The literal is the operator-facing contract; do not route it through the constant.
		if got, ok := subSignals["products_dropped_by_newest_copy"]; !ok || got != 1 {
			t.Fatalf("SubSignals[products_dropped_by_newest_copy] = %v (present %v), want 1", got, ok)
		}
	})

	t.Run("clean pass reports zero", func(t *testing.T) {
		t.Parallel()

		clean := []facts.Envelope{
			newestCopyNVDProductCVE("cve-10735-only", "scope:nvd-only", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
			newestCopyProduct("product-10735-only", "scope:nvd-only", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
		}
		clean = append(clean, newestCopyProductAnchors(newestCopyProductCriteriaB)...)
		_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(clean)
		if err != nil {
			t.Fatalf("build error = %v", err)
		}
		empty := map[string]float64{}
		SupplyChainImpactHandler{}.recordBuildStats(context.Background(), stats, empty)
		if got, ok := empty["products_dropped_by_newest_copy"]; !ok || got != 0 {
			t.Fatalf("SubSignals[products_dropped_by_newest_copy] = %v (present %v), want 0 on a clean pass", got, ok)
		}
	})

	t.Run("unreadable key keeps every product", func(t *testing.T) {
		t.Parallel()

		withQuarantine := append(slices.Clone(dropping), quarantinedCVEFact("cve-10735-quar", cveID, "nvd", cveID))
		findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(withQuarantine)
		if err != nil {
			t.Fatalf("build error = %v", err)
		}
		if stats.productsDroppedByNewestCopy != 0 {
			t.Fatalf("productsDroppedByNewestCopy = %d, want 0 while the key is unreadable", stats.productsDroppedByNewestCopy)
		}
		if got := len(findings); got != 2 {
			t.Fatalf("len(findings) = %d, want 2 (every product stays while the key is unreadable)", got)
		}
	})

	t.Run("truncated load keeps every product", func(t *testing.T) {
		t.Parallel()

		findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(dropping, supplyChainAliasGroupConfig{disabled: true})
		if err != nil {
			t.Fatalf("build error = %v", err)
		}
		if stats.productsDroppedByNewestCopy != 0 {
			t.Fatalf("productsDroppedByNewestCopy = %d, want 0 on a truncated load", stats.productsDroppedByNewestCopy)
		}
		if got := len(findings); got != 2 {
			t.Fatalf("len(findings) = %d, want 2 (every product stays on a truncated load)", got)
		}
	})
}

// benchNewestCopyProductGroups builds groups of two NVD copies (an older
// baseline copy and a newer window copy) with one anchored product each, the
// multi-copy shape the #10735 admission elects over. It uses only helpers and
// payload keys that predate the change, so the same benchmark compiles and
// runs against the code before it for a before/after comparison.
func benchNewestCopyProductGroups(groups int) []facts.Envelope {
	productIn := func(factID, scopeID, cveID, criteria, matchID string) facts.Envelope {
		envelope := vulnerabilityAffectedProductFact(factID, cveID, criteria, matchID, true)
		envelope.ScopeID = scopeID
		envelope.GenerationID = "generation:" + scopeID
		return envelope
	}
	cveIn := func(factID, scopeID, cveID string, score float64, label, modifiedAt string) facts.Envelope {
		envelope := newestCopyCVE(factID, scopeID, cveID, "nvd", cveID, score, modifiedAt, "", "")
		envelope.Payload["severity_label"] = label
		return envelope
	}
	envelopes := make([]facts.Envelope, 0, groups*4+2)
	criteria := make([]string, 0, groups*2)
	for i := 0; i < groups; i++ {
		cveID := fmt.Sprintf("CVE-2026-%05d", 91000+i)
		oldCriterion := fmt.Sprintf("cpe:2.3:a:example:bench-old-%d:1.0:*:*:*:*:*:*:*", i)
		newCriterion := fmt.Sprintf("cpe:2.3:a:example:bench-new-%d:2.0:*:*:*:*:*:*:*", i)
		envelopes = append(envelopes,
			cveIn(fmt.Sprintf("bench-cve-old-%d", i), "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
			productIn(fmt.Sprintf("bench-product-old-%d", i), "scope:nvd-baseline", cveID, oldCriterion, fmt.Sprintf("match-bench-old-%d", i)),
			cveIn(fmt.Sprintf("bench-cve-new-%d", i), "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
			productIn(fmt.Sprintf("bench-product-new-%d", i), "scope:nvd-window", cveID, newCriterion, fmt.Sprintf("match-bench-new-%d", i)),
		)
		criteria = append(criteria, oldCriterion, newCriterion)
	}
	anchors := make([]facts.Envelope, 0, len(criteria)+2)
	for i, criterion := range criteria {
		anchors = append(anchors, sbomComponentCPEImpactFact(fmt.Sprintf("bench-component-%d", i), "doc-bench", criterion))
	}
	return append(envelopes, append(anchors,
		sbomAttachmentImpactFact("bench-attachment", "doc-bench", testImpactSubjectDigest),
		containerImageIdentityImpactFact("bench-image", testImpactSubjectDigest, testImpactRepositoryID),
	)...)
}

// BenchmarkProductPathNewestCopyAdmission measures the full findings build
// over 200 two-copy NVD groups, so the per-group election, the per-product
// scope admission, and the elected representative and severity are all on
// the measured path (#10735).
func BenchmarkProductPathNewestCopyAdmission(b *testing.B) {
	envelopes := benchNewestCopyProductGroups(200)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BuildSupplyChainImpactFindings(envelopes)
	}
}
