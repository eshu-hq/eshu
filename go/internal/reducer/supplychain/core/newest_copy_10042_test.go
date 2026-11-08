// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// These tests pin the newest-copy rule of #10042 (design 10037, section 4):
// for one payload source and advisory id, the copy with the greatest
// modified_at is the record, ties break on the greater FactID, and an empty or
// unparseable timestamp ranks oldest. Older copies leave the group whole, so a
// newer withdrawn or Rejected copy retracts the advisory even while an older
// active copy of it is still in the active fact set.
//
// RED on origin/main: retractionFromObservations needs EVERY observation
// retracted, so an older active copy blocks the retraction, and
// selectAdvisoryProvenance has no timestamp key, so load order picks which copy
// supplies severity, fixed version, and range. Tests named ...Control pass on
// origin/main and must keep passing once the rule lands.

const (
	newestCopyOlder = "2026-10-01T00:00:00Z"
	newestCopyNewer = "2026-11-01T00:00:00Z"
)

// newestCopyCVE builds one vulnerability.cve copy in its own scope and
// generation, the way a per-window advisory generation holds it.
func newestCopyCVE(
	factID, scopeID, cveID, source, advisoryID string,
	score float64, modifiedAt, status, withdrawnAt string,
) facts.Envelope {
	envelope := vulnerabilityCVEFactWithProvenance(factID, cveID, source, advisoryID, score, "", "", modifiedAt)
	if modifiedAt == "" {
		delete(envelope.Payload, "modified_at")
	}
	if status != "" {
		envelope.Payload["status"] = status
	}
	if withdrawnAt != "" {
		envelope.Payload["withdrawn_at"] = withdrawnAt
	}
	envelope.ScopeID = scopeID
	envelope.GenerationID = "generation:" + scopeID
	return envelope
}

// newestCopyPackage builds the affected_package fact the same copy's scope
// carries, so a copy is a CVE fact plus its package fact.
func newestCopyPackage(factID, scopeID, cveID, source, advisoryID, fixedVersion string) facts.Envelope {
	envelope := retractionPackageFact(factID, cveID, source, advisoryID, fixedVersion)
	// A copy-specific range, so a range taken from the wrong copy is visible.
	envelope.Payload["affected_range"] = ">=1.0.0 <" + fixedVersion
	envelope.ScopeID = scopeID
	envelope.GenerationID = "generation:" + scopeID
	return envelope
}

// newestCopyFinding runs the admission path the handler runs on a merged fact
// set and returns the single finding with its suppression decision applied.
func newestCopyFinding(t *testing.T, envelopes []facts.Envelope) SupplyChainImpactFinding {
	t.Helper()
	return buildRetractionFinding(t, envelopes...)
}

// permutations returns every ordering of items. The slices are small (at most
// six entries), so the factorial is bounded.
func permutations(items []facts.Envelope) [][]facts.Envelope {
	var out [][]facts.Envelope
	var walk func(rest []facts.Envelope, prefix []facts.Envelope)
	walk = func(rest, prefix []facts.Envelope) {
		if len(rest) == 0 {
			out = append(out, append([]facts.Envelope(nil), prefix...))
			return
		}
		for i := range rest {
			next := append(append([]facts.Envelope(nil), rest[:i]...), rest[i+1:]...)
			walk(next, append(prefix, rest[i]))
		}
	}
	walk(items, nil)
	return out
}

// TestNewestCopyRetractsOlderActiveCopy covers acceptance (a) and (b): an older
// active copy and a newer withdrawn or Rejected copy of one advisory retract it,
// in either load order. RED today: retracted=false because the older active
// copy counts as a live observation.
func TestNewestCopyRetractsOlderActiveCopy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		source     string
		advisoryID string
		cveID      string
		status     string
		withdrawn  string
		wantCode   string
	}{
		{"ghsa withdrawn_at", "ghsa", "GHSA-aaaa-bbbb-cccc", "CVE-2026-9001", "", newestCopyNewer, AdvisoryRetractionWithdrawn},
		{"ghsa status withdrawn", "ghsa", "GHSA-aaaa-bbbb-cccc", "CVE-2026-9002", "withdrawn", "", AdvisoryRetractionWithdrawn},
		{"osv-carried ghsa withdrawn_at", "osv", "GHSA-dddd-eeee-ffff", "CVE-2026-9003", "", newestCopyNewer, AdvisoryRetractionWithdrawn},
		{"nvd Rejected", "nvd", "CVE-2026-9004", "CVE-2026-9004", "Rejected", "", AdvisoryRetractionRejected},
	}
	for _, tc := range cases {
		for _, order := range []string{"older-first", "newer-first"} {
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				older := []facts.Envelope{
					newestCopyCVE("cve-older", "scope:w1", tc.cveID, tc.source, tc.advisoryID, 9.8, newestCopyOlder, "", ""),
					newestCopyPackage("pkg-older", "scope:w1", tc.cveID, tc.source, tc.advisoryID, "1.0.1"),
				}
				newer := []facts.Envelope{
					newestCopyCVE("cve-newer", "scope:w2", tc.cveID, tc.source, tc.advisoryID, 9.8, newestCopyNewer, tc.status, tc.withdrawn),
					newestCopyPackage("pkg-newer", "scope:w2", tc.cveID, tc.source, tc.advisoryID, "1.0.1"),
				}
				in := append(append([]facts.Envelope(nil), older...), newer...)
				if order == "newer-first" {
					in = append(append([]facts.Envelope(nil), newer...), older...)
				}
				in = append(in, retractionConsumption())

				finding := newestCopyFinding(t, in)
				if got := finding.Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
					t.Fatalf("state = %q, want advisory_retracted: the newest copy is withdrawn, the older active copy must leave the group", got)
				}
				if finding.AdvisoryRetraction == nil {
					t.Fatalf("AdvisoryRetraction = nil, want a %q retraction", tc.wantCode)
				}
				if finding.AdvisoryRetraction.ReasonCode != tc.wantCode {
					t.Fatalf("ReasonCode = %q, want %q", finding.AdvisoryRetraction.ReasonCode, tc.wantCode)
				}
				if got := len(finding.AdvisoryRetraction.Sources); got != 1 {
					t.Fatalf("retraction sources = %d, want 1 (the newest copy only): %#v", got, finding.AdvisoryRetraction.Sources)
				}
			})
		}
	}
}

// TestHandlerSecondHopOlderActiveCopyDoesNotBlockRetraction is (a) and (b)
// through Handle, the real entry point: the intent's own scope holds only the
// newer withdrawn or Rejected copy, and the older active copy in another window
// scope arrives through the active-facts second hop (supplyChainImpactFilter ->
// ListActiveSupplyChainImpactFacts). RED today: the finding stays active.
func TestHandlerSecondHopOlderActiveCopyDoesNotBlockRetraction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		source     string
		advisoryID string
		cveID      string
		status     string
		withdrawn  string
	}{
		{"ghsa", "ghsa", "GHSA-aaaa-bbbb-cccc", "CVE-2026-9101", "", newestCopyNewer},
		{"nvd", "nvd", "CVE-2026-9102", "CVE-2026-9102", "Rejected", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scopeFacts := []facts.Envelope{
				newestCopyCVE("cve-newer", "scope:w2", tc.cveID, tc.source, tc.advisoryID, 9.8, newestCopyNewer, tc.status, tc.withdrawn),
				newestCopyPackage("pkg-newer", "scope:w2", tc.cveID, tc.source, tc.advisoryID, "1.0.1"),
				retractionConsumption(),
			}
			olderActive := []facts.Envelope{
				newestCopyCVE("cve-older", "scope:w1", tc.cveID, tc.source, tc.advisoryID, 9.8, newestCopyOlder, "", ""),
				newestCopyPackage("pkg-older", "scope:w1", tc.cveID, tc.source, tc.advisoryID, "1.0.1"),
			}
			loader := &stubSupplyChainImpactFactLoader{
				scopeFacts: scopeFacts,
				activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
					for _, id := range filter.CVEIDs {
						if id == tc.cveID {
							return olderActive
						}
					}
					return nil
				},
			}
			writer := &recordingSupplyChainImpactWriter{}
			handler := SupplyChainImpactHandler{
				FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
				FactLoader:         loader,
				Writer:             writer,
				Now:                func() time.Time { return time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC) },
			}
			if _, err := handler.Handle(context.Background(), reducercontract.Intent{
				IntentID: "intent-10042", ScopeID: "scope:w2", GenerationID: "generation:scope:w2",
				SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact,
				Cause: "vulnerability evidence observed",
			}); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(writer.write.Findings) != 1 {
				t.Fatalf("findings = %d, want 1: %#v", len(writer.write.Findings), writer.write.Findings)
			}
			if got := writer.write.Findings[0].Suppression.State; got != SupplyChainSuppressionStateAdvisoryRetracted {
				t.Fatalf("state = %q, want advisory_retracted (second-hop older active copy must not block it); filters=%#v", got, loader.filters)
			}
		})
	}
}

// TestNewestCopySelectionIsIndependentOfLoadOrder is (c): two live GHSA copies
// of one advisory, newer one carrying a different severity, fixed version, and
// range. Every ordering of the facts must select what the newest copy alone
// selects. RED today: the stable sort has no timestamp key, so load order picks
// the copy.
func TestNewestCopySelectionIsIndependentOfLoadOrder(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9201"
		advisoryID = "GHSA-gggg-hhhh-iiii"
	)
	oldCVE := newestCopyCVE("fact-z-older", "scope:w1", cveID, "ghsa", advisoryID, 9.8, newestCopyOlder, "", "")
	oldPkg := newestCopyPackage("pkg-z-older", "scope:w1", cveID, "ghsa", advisoryID, "1.0.1")
	newCVE := newestCopyCVE("fact-a-newer", "scope:w2", cveID, "ghsa", advisoryID, 5.0, newestCopyNewer, "", "")
	newPkg := newestCopyPackage("pkg-a-newer", "scope:w2", cveID, "ghsa", advisoryID, "1.0.2")

	want := newestCopyFinding(t, []facts.Envelope{newCVE, newPkg, retractionConsumption()})
	if want.CVSSScore != 5.0 || want.FixedVersion != "1.0.2" || want.VulnerableRange != ">=1.0.0 <1.0.2" {
		t.Fatalf("reference = score %v fixed %q range %q, want the newer copy's 5.0, 1.0.2, >=1.0.0 <1.0.2",
			want.CVSSScore, want.FixedVersion, want.VulnerableRange)
	}

	for i, order := range permutations([]facts.Envelope{oldCVE, oldPkg, newCVE, newPkg, retractionConsumption()}) {
		got := newestCopyFinding(t, order)
		if got.CVSSScore != want.CVSSScore || got.FixedVersion != want.FixedVersion ||
			got.VulnerableRange != want.VulnerableRange || got.SeverityVector != want.SeverityVector {
			t.Fatalf("permutation %d %v: score=%v fixed=%q range=%q, want newest copy's score=%v fixed=%q range=%q",
				i, factIDs(order), got.CVSSScore, got.FixedVersion, got.VulnerableRange,
				want.CVSSScore, want.FixedVersion, want.VulnerableRange)
		}
	}
}

// TestNewestCopyTiebreakAndUnparseableTimestamps is (c) for the tiebreak: equal
// modified_at breaks on the greater FactID, and an empty or unparseable
// timestamp ranks oldest whatever its FactID. Each case must select what the
// expected winner alone selects, in every load order. RED today.
func TestNewestCopyTiebreakAndUnparseableTimestamps(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9301"
		advisoryID = "GHSA-jjjj-kkkk-llll"
	)
	cases := []struct {
		name       string
		firstTime  string // copy with FactID "fact-a"
		secondTime string // copy with FactID "fact-b"
		winner     string // "fact-a" or "fact-b"
	}{
		{"equal timestamps, greater FactID wins", newestCopyNewer, newestCopyNewer, "fact-b"},
		{"empty timestamp ranks oldest", newestCopyNewer, "", "fact-a"},
		{"unparseable timestamp ranks oldest", newestCopyOlder, "not-a-time", "fact-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			copyA := []facts.Envelope{
				newestCopyCVE("fact-a", "scope:a", cveID, "ghsa", advisoryID, 9.8, tc.firstTime, "", ""),
				newestCopyPackage("pkg-a", "scope:a", cveID, "ghsa", advisoryID, "1.0.1"),
			}
			copyB := []facts.Envelope{
				newestCopyCVE("fact-b", "scope:b", cveID, "ghsa", advisoryID, 5.0, tc.secondTime, "", ""),
				newestCopyPackage("pkg-b", "scope:b", cveID, "ghsa", advisoryID, "1.0.2"),
			}
			winner := copyA
			if tc.winner == "fact-b" {
				winner = copyB
			}
			want := newestCopyFinding(t, append(append([]facts.Envelope(nil), winner...), retractionConsumption()))
			all := append(append(append([]facts.Envelope(nil), copyA...), copyB...), retractionConsumption())
			for _, order := range permutations(all) {
				got := newestCopyFinding(t, order)
				if got.CVSSScore != want.CVSSScore || got.FixedVersion != want.FixedVersion || got.VulnerableRange != want.VulnerableRange {
					t.Fatalf("order %v: score=%v fixed=%q range=%q, want winner %s's score=%v fixed=%q range=%q",
						factIDs(order), got.CVSSScore, got.FixedVersion, got.VulnerableRange, tc.winner,
						want.CVSSScore, want.FixedVersion, want.VulnerableRange)
				}
			}
		})
	}
}

// TestNewestCopyLeavesOlderCopyOutOfTheGroup pins "older copies leave the group
// whole" for two live copies: the older copy's severity is not an alternate
// severity and its fixed version is not a branch. RED today: both copies are
// group members, so the older one surfaces as an alternate severity and a
// fixed-version branch.
func TestNewestCopyLeavesOlderCopyOutOfTheGroup(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9401"
		advisoryID = "GHSA-mmmm-nnnn-oooo"
	)
	finding := newestCopyFinding(t, []facts.Envelope{
		newestCopyCVE("cve-older", "scope:w1", cveID, "ghsa", advisoryID, 9.8, newestCopyOlder, "", ""),
		newestCopyPackage("pkg-older", "scope:w1", cveID, "ghsa", advisoryID, "1.0.1"),
		newestCopyCVE("cve-newer", "scope:w2", cveID, "ghsa", advisoryID, 5.0, newestCopyNewer, "", ""),
		newestCopyPackage("pkg-newer", "scope:w2", cveID, "ghsa", advisoryID, "1.0.2"),
		retractionConsumption(),
	})
	if findingHasAlternateSeverity(finding, "ghsa", 9.8) {
		t.Fatalf("AlternateSeverities = %#v, want none from the older copy", finding.AlternateSeverities)
	}
	if findingHasFixedVersionBranch(finding, "1.0.1", "ghsa") {
		t.Fatalf("FixedVersionBranches = %#v, want only the newest copy's 1.0.2", finding.FixedVersionBranches)
	}
	for _, id := range finding.EvidenceFactIDs {
		if strings.HasSuffix(id, "-older") {
			t.Fatalf("EvidenceFactIDs = %v, want no older-copy fact id", finding.EvidenceFactIDs)
		}
	}
}

// TestNewestCopyOlderWithdrawnNewerActiveStaysActiveControl is (f): an older
// withdrawn copy and a newer active copy do NOT retract. The design (section 4,
// "Un-withdrawal") makes the later null-withdrawn_at copy the record, so the
// finding returns, and the withdrawn older copy supplies nothing. Control: GREEN
// on origin/main today (a live observation blocks retraction) and must stay
// GREEN once the rule lands.
func TestNewestCopyOlderWithdrawnNewerActiveStaysActiveControl(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9501"
		advisoryID = "GHSA-pppp-qqqq-rrrr"
	)
	for _, order := range permutations([]facts.Envelope{
		newestCopyCVE("cve-older", "scope:w1", cveID, "ghsa", advisoryID, 9.8, newestCopyOlder, "", newestCopyOlder),
		newestCopyPackage("pkg-older", "scope:w1", cveID, "ghsa", advisoryID, "1.0.1"),
		newestCopyCVE("cve-newer", "scope:w2", cveID, "ghsa", advisoryID, 5.0, newestCopyNewer, "", ""),
		newestCopyPackage("pkg-newer", "scope:w2", cveID, "ghsa", advisoryID, "1.0.2"),
		retractionConsumption(),
	}) {
		finding := newestCopyFinding(t, order)
		if finding.Suppression.State != SupplyChainSuppressionStateActive || finding.AdvisoryRetraction != nil {
			t.Fatalf("order %v: state=%q retraction=%v, want active (un-withdrawal: the newest copy is live)",
				factIDs(order), finding.Suppression.State, finding.AdvisoryRetraction)
		}
		if finding.CVSSScore != 5.0 {
			t.Fatalf("order %v: CVSSScore = %v, want the live newest copy's 5.0", factIDs(order), finding.CVSSScore)
		}
	}
}

// TestGHSAAndOSVSameIDKeepOneSourceRowAndBothEvidenceIDsControl is (d): a direct
// GHSA copy (payload source "ghsa") and an OSV-carried copy (payload source
// "osv", GHSA- id) of one advisory stay two observations of logical source
// "ghsa": one AdvisorySources row, both copies' fact ids as evidence. A newest-
// copy rule keyed on the CLASSIFIED source would collapse them and drop the OSV
// copy's evidence, so this fences the key choice. Control: GREEN on origin/main.
func TestGHSAAndOSVSameIDKeepOneSourceRowAndBothEvidenceIDsControl(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9601"
		advisoryID = "GHSA-ssss-tttt-uuuu"
	)
	finding := newestCopyFinding(t, []facts.Envelope{
		newestCopyCVE("cve-direct", "scope:ghsa", cveID, "ghsa", advisoryID, 9.8, newestCopyOlder, "", ""),
		newestCopyPackage("pkg-direct", "scope:ghsa", cveID, "ghsa", advisoryID, "1.0.1"),
		newestCopyCVE("cve-osv", "scope:osv", cveID, "osv", advisoryID, 9.8, newestCopyNewer, "", ""),
		newestCopyPackage("pkg-osv", "scope:osv", cveID, "osv", advisoryID, "1.0.1"),
		retractionConsumption(),
	})
	rows := 0
	for _, source := range finding.AdvisorySources {
		if source.Source == "ghsa" && source.AdvisoryID == advisoryID {
			rows++
		}
	}
	if rows != 1 || len(finding.AdvisorySources) != 1 {
		t.Fatalf("AdvisorySources = %#v, want exactly one ghsa row for %s", finding.AdvisorySources, advisoryID)
	}
	have := map[string]bool{}
	for _, id := range finding.EvidenceFactIDs {
		have[id] = true
	}
	for _, id := range []string{"cve-direct", "cve-osv", "pkg-direct", "pkg-osv"} {
		if !have[id] {
			t.Fatalf("EvidenceFactIDs = %v, want %q present (both copies are evidence)", finding.EvidenceFactIDs, id)
		}
	}
}

// TestGHSADirectWithdrawalWithLiveOSVCopyStaysActiveControl is (d) for
// retraction: a direct GHSA withdrawal with a live OSV-carried copy keeps the
// finding until OSV catches up ("the safe direction", design section 4), in
// either direction of recency. Control: GREEN on origin/main.
func TestGHSADirectWithdrawalWithLiveOSVCopyStaysActiveControl(t *testing.T) {
	t.Parallel()

	const (
		cveID      = "CVE-2026-9602"
		advisoryID = "GHSA-vvvv-wwww-xxxx"
	)
	cases := []struct {
		name                      string
		directUpdated, osvUpdated string
	}{
		{"direct withdrawal is newer", newestCopyNewer, newestCopyOlder},
		{"osv copy is newer", newestCopyOlder, newestCopyNewer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			finding := newestCopyFinding(t, []facts.Envelope{
				newestCopyCVE("cve-direct", "scope:ghsa", cveID, "ghsa", advisoryID, 9.8, tc.directUpdated, "", newestCopyNewer),
				newestCopyPackage("pkg-direct", "scope:ghsa", cveID, "ghsa", advisoryID, "1.0.1"),
				newestCopyCVE("cve-osv", "scope:osv", cveID, "osv", advisoryID, 9.8, tc.osvUpdated, "", ""),
				newestCopyPackage("pkg-osv", "scope:osv", cveID, "osv", advisoryID, "1.0.1"),
				retractionConsumption(),
			})
			if finding.Suppression.State != SupplyChainSuppressionStateActive || finding.AdvisoryRetraction != nil {
				t.Fatalf("state=%q retraction=%v, want active: the live OSV copy is its own observation", finding.Suppression.State, finding.AdvisoryRetraction)
			}
		})
	}
}

func factIDs(envelopes []facts.Envelope) string {
	ids := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		ids = append(ids, envelope.FactID)
	}
	return fmt.Sprintf("%v", ids)
}
