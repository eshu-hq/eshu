// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// TestSupplyChainImpactFilterAsksForTheUpstreamCVEs: the filter requests exactly
// the keys the grouping pass forms from a record's upstream list, spelled as the
// pass spells them, on their own field. A key the record already holds as its
// cve_id is on the full arm and is not asked for twice.
func TestSupplyChainImpactFilterAsksForTheUpstreamCVEs(t *testing.T) {
	t.Parallel()

	record := debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3",
		"CVE-2023-38039", " cve-2024-1 ", "DEBIAN-CVE-2023-38039", "CVE-")
	filter := supplyChainImpactFilter(convertedOSVRecord(t, record, false))
	if want := []string{"CVE-2023-38039", "CVE-2024-1"}; !reflect.DeepEqual(filter.UpstreamCVEIDs, want) {
		t.Errorf("UpstreamCVEIDs = %v, want %v", filter.UpstreamCVEIDs, want)
	}
	if want := []string{"DSA-9998-1"}; !reflect.DeepEqual(filter.CVEIDs, want) {
		t.Errorf("CVEIDs = %v, want %v: an upstream key must not reach the full arm", filter.CVEIDs, want)
	}

	native := convertedOSVRecord(t, debianRecord("DSA-9998-1", "12", "bookworm", "7.88.1-10+deb12u3", "CVE-2023-38039", "CVE-2024-1"), false)
	for _, envelope := range native {
		envelope.Payload["cve_id"] = "CVE-2023-38039"
	}
	filter = supplyChainImpactFilter(native)
	if want := []string{"CVE-2024-1"}; !reflect.DeepEqual(filter.UpstreamCVEIDs, want) {
		t.Errorf("a record's own cve_id: UpstreamCVEIDs = %v, want %v", filter.UpstreamCVEIDs, want)
	}
}

// TestUpstreamSignalsDoNotWidenTheNextRound: the load repeats until the filter
// stops growing. A KEV, EPSS or suppression fact the upstream arm loaded must not
// put its CVE on the full arm next round, or the pass loads the CVE's NVD record
// and product rows anyway, the full widening the #9914 shim failed. A signal for a
// CVE no record names keeps the full arm: an EPSS or KEV intent is how a language
// finding gets re-read.
func TestUpstreamSignalsDoNotWidenTheNextRound(t *testing.T) {
	t.Parallel()

	const named = "CVE-2023-38039"
	suppression := func(cve string) facts.Envelope {
		return suppressionFactEnvelope("suppression-"+cve, chain.VulnerabilitySuppressionSourcePolicy,
			chain.VulnerabilitySuppressionJustificationIgnored, "eshu:policy/operator@example.com",
			"2026-01-01T00:00:00Z", "2030-01-01T00:00:00Z", map[string]any{"cve_id": cve})
	}
	record := convertedOSVRecord(t, debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", named), false)

	for name, signal := range map[string]facts.Envelope{
		"kev":                        vulnerabilityKEVFact("kev-1", named),
		"epss":                       vulnerabilityEPSSFact("epss-1", named, "0.5", "0.9"),
		"suppression":                suppression(named),
		"suppression, respelled key": suppression(" cve-2023-38039 "),
	} {
		filter := supplyChainImpactFilter(append(slices.Clone(record), signal))
		if slices.ContainsFunc(filter.CVEIDs, func(id string) bool { return strings.EqualFold(strings.TrimSpace(id), named) }) {
			t.Errorf("%s: CVEIDs = %v, must not carry %s", name, filter.CVEIDs, named)
		}
		if !slices.Contains(filter.UpstreamCVEIDs, named) {
			t.Errorf("%s: UpstreamCVEIDs = %v, want %s", name, filter.UpstreamCVEIDs, named)
		}
	}

	const other = "CVE-2024-7"
	for name, signal := range map[string]facts.Envelope{
		"kev":         vulnerabilityKEVFact("kev-2", other),
		"epss":        vulnerabilityEPSSFact("epss-2", other, "0.5", "0.9"),
		"suppression": suppression(other),
	} {
		filter := supplyChainImpactFilter(append(slices.Clone(record), signal))
		if !slices.Contains(filter.CVEIDs, other) {
			t.Errorf("%s for a CVE no record names: CVEIDs = %v, want %s", name, filter.CVEIDs, other)
		}
	}

	// A record that is keyed to the CVE still earns the full arm.
	both := append(slices.Clone(record), vulnerabilityCVEFact("nvd-1", named, 9.8), vulnerabilityKEVFact("kev-1", named))
	filter := supplyChainImpactFilter(both)
	if !slices.Contains(filter.CVEIDs, named) || slices.Contains(filter.UpstreamCVEIDs, named) {
		t.Errorf("keyed record: CVEIDs = %v, UpstreamCVEIDs = %v, want the CVE on the full arm only", filter.CVEIDs, filter.UpstreamCVEIDs)
	}
}

// TestHandlerLoadsTheSignalsOfTheCVEAFindingCarries: a distro record is keyed by
// its own id on disk, so nothing filed under the CVE it names reaches the pass
// unless the filter asks. Every other grouping test puts all facts in one batch
// and cannot see this.
//
// The stub honours the filter the way the catalog does. CVEIDs reads every kind,
// UpstreamCVEIDs reads KEV, EPSS and the suppression scope. Until the widening is
// measured (#9914 shim) the NVD score stays out, and this pins that too.
//
// Measured on the input below at 242415ed46: the only filter asked for
// DEBIAN-CVE-2023-38039, and the finding read kev=false, no EPSS and suppression
// state active. A suppression an operator scoped to the CVE the API shows silently
// did nothing.
func TestHandlerLoadsTheSignalsOfTheCVEAFindingCarries(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	record := convertedOSVRecord(t,
		debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)
	installed := debianCurl("12", "7.88.1-10+deb12u2").envelope()
	var cveFacts, packageFacts []facts.Envelope
	for _, envelope := range record {
		if envelope.FactKind == chain.VulnerabilityCVEFactKind {
			cveFacts = append(cveFacts, envelope)
		} else {
			packageFacts = append(packageFacts, envelope)
		}
	}

	for name, input := range map[string]struct {
		scope, byPackage []facts.Envelope
	}{
		"record in the intent scope": {scope: append(slices.Clone(record), installed)},
		// The package fact is in scope and the cve fact, which holds the upstream
		// list, arrives on a later round, so only the follow-up filter can ask.
		"upstream list arrives on a follow-up": {scope: append(slices.Clone(packageFacts), installed), byPackage: cveFacts},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertUpstreamSignalsReachTheFinding(t, cve, input.scope, input.byPackage)
		})
	}
}

// runHandlerOverCatalogStub runs one pass over a stub that honours the filter the
// way the catalog does: CVEIDs reads every kind, and UpstreamCVEIDs reads KEV, EPSS
// and the suppression scope. byPackage is what it returns to a filter that names a
// package.
func runHandlerOverCatalogStub(
	t *testing.T, scope, corpus, byPackage []facts.Envelope,
) (*stubSupplyChainImpactFactLoader, *recordingSupplyChainImpactWriter) {
	t.Helper()
	loader := &stubSupplyChainImpactFactLoader{scopeFacts: scope, activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		var out []facts.Envelope
		if len(filter.PackageIDs) > 0 {
			out = append(out, byPackage...)
		}
		for _, envelope := range corpus {
			key := payloadcore.PayloadStr(envelope.Payload, "cve_id")
			matches := slices.Contains(filter.CVEIDs, key)
			switch envelope.FactKind {
			case chain.VulnerabilitySuppressionFactKind:
				key = payloadcore.PayloadStr(payloadcore.PayloadMap(envelope.Payload, "scope"), "cve_id")
				matches = slices.Contains(filter.CVEIDs, key) || slices.Contains(filter.UpstreamCVEIDs, key)
			case chain.VulnerabilityKnownExploitedFactKind, chain.VulnerabilityEPSSScoreFactKind:
				matches = matches || slices.Contains(filter.UpstreamCVEIDs, key)
			}
			if matches {
				out = append(out, envelope)
			}
		}
		return out
	}}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer,
		Now: func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "i", ScopeID: "vuln-intel://osv/fixture", GenerationID: "g",
		SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact, Cause: "x",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return loader, writer
}

// assertUpstreamSignalsReachTheFinding runs the handler over a corpus filed under
// cve. byPackage is what the stub returns to a filter that names a package.
func assertUpstreamSignalsReachTheFinding(t *testing.T, cve string, scope, byPackage []facts.Envelope) {
	t.Helper()
	corpus := []facts.Envelope{
		suppressionFactEnvelope("suppression-cve", chain.VulnerabilitySuppressionSourcePolicy,
			chain.VulnerabilitySuppressionJustificationIgnored, "eshu:policy/operator@example.com",
			"2026-01-01T00:00:00Z", "2030-01-01T00:00:00Z", map[string]any{"cve_id": cve}),
		vulnerabilityKEVFact("kev-1", cve),
		vulnerabilityEPSSFact("epss-1", cve, "0.5", "0.9"),
		vulnerabilityCVEFact("nvd-1", cve, 9.8),
		vulnerabilityAffectedProductFact("product-1", cve, testImpactProductCriteria, testImpactMatchCriteriaID, true),
	}
	loader, writer := runHandlerOverCatalogStub(t, scope, corpus, byPackage)
	asked := false
	for _, filter := range loader.filters {
		asked = asked || slices.Contains(filter.UpstreamCVEIDs, cve)
		if slices.Contains(filter.CVEIDs, cve) {
			t.Errorf("filter %#v puts %s on the full arm: that is the unmeasured widening", filter, cve)
		}
	}
	if !asked {
		t.Fatalf("filters = %#v, want one that asks for %s", loader.filters, cve)
	}
	if len(writer.write.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(writer.write.Findings), writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.CVEID != cve || got.AdvisoryID != "DEBIAN-CVE-2023-38039" {
		t.Errorf("cve/advisory = %q/%q, want %q named by the deciding record", got.CVEID, got.AdvisoryID, cve)
	}
	if !got.KnownExploited {
		t.Error("KnownExploited = false, want the KEV fact keyed by the CVE to reach the finding")
	}
	if got.EPSSProbability == "" {
		t.Error("EPSSProbability is empty, want the EPSS fact keyed by the CVE to reach the finding")
	}
	if got.Suppression.State != SupplyChainSuppressionStateIgnored {
		t.Errorf("Suppression.State = %q, want %q: a suppression scoped to the CVE must take effect", got.Suppression.State, SupplyChainSuppressionStateIgnored)
	}
	if got.CVSSScore != 0 {
		t.Errorf("CVSSScore = %v, want 0: the NVD record stays out until the widening is measured", got.CVSSScore)
	}
}

// TestUpstreamKeysAcrossRecordsAreSearchedSorted: keys from several records arrive
// in fact order, and the signal check binary-searches them.
func TestUpstreamKeysAcrossRecordsAreSearchedSorted(t *testing.T) {
	t.Parallel()

	envelopes := convertedOSVRecord(t, debianRecord("DSA-2-1", "12", "bookworm", "7.88.1-10+deb12u3", "CVE-2024-9"), false)
	envelopes = append(envelopes, convertedOSVRecord(t, debianRecord("DSA-1-1", "12", "bookworm", "7.88.1-10+deb12u3", "CVE-2023-1"), false)...)
	envelopes = append(envelopes, vulnerabilityKEVFact("kev-1", "CVE-2023-1"), vulnerabilityKEVFact("kev-2", "CVE-2024-9"))
	filter := supplyChainImpactFilter(envelopes)
	if want := []string{"CVE-2023-1", "CVE-2024-9"}; !reflect.DeepEqual(filter.UpstreamCVEIDs, want) {
		t.Errorf("UpstreamCVEIDs = %v, want %v", filter.UpstreamCVEIDs, want)
	}
	for _, id := range filter.CVEIDs {
		if strings.HasPrefix(id, "CVE-") {
			t.Errorf("CVEIDs = %v: a signal for a named CVE must stay off the full arm", filter.CVEIDs)
		}
	}
}

// TestUpstreamListAloneIsWorkToDo: a follow-up round whose only new input is the
// upstream list still loads, and it asks only for what is new.
func TestUpstreamListAloneIsWorkToDo(t *testing.T) {
	t.Parallel()

	requested := SupplyChainImpactFactFilter{UpstreamCVEIDs: []string{"CVE-2024-1"}}
	current := SupplyChainImpactFactFilter{UpstreamCVEIDs: []string{"CVE-2024-1", "CVE-2024-2"}}
	next := supplyChainImpactFollowUpFilter(requested, current)
	if next.empty() {
		t.Fatal("empty() = true for a filter holding only upstream ids")
	}
	if want := []string{"CVE-2024-2"}; !reflect.DeepEqual(next.UpstreamCVEIDs, want) {
		t.Errorf("UpstreamCVEIDs = %v, want %v", next.UpstreamCVEIDs, want)
	}
	if !supplyChainImpactFollowUpFilter(current, current).empty() {
		t.Error("empty() = false with nothing new")
	}
}

// TestUpstreamArmYieldsToANativeRecordArrivingLater: the arm a CVE takes depends on
// the loaded set, not on the round a fact arrived in. The pass first asks for the
// CVE on the upstream arm. A native record for it then arrives through the package
// arm, so the CVE moves to the full arm once, and the finding reads both the native
// score and the KEV the upstream arm already brought.
func TestUpstreamArmYieldsToANativeRecordArrivingLater(t *testing.T) {
	t.Parallel()

	const cve = "CVE-2023-38039"
	record := convertedOSVRecord(t,
		debianRecord("DEBIAN-CVE-2023-38039", "12", "bookworm", "7.88.1-10+deb12u3", cve), false)
	scope := append(slices.Clone(record), debianCurl("12", "7.88.1-10+deb12u2").envelope())
	kev := vulnerabilityKEVFact("kev-1", cve)
	native := vulnerabilityCVEFact("nvd-1", cve, 9.8)

	loader, writer := runHandlerOverCatalogStub(t, scope, []facts.Envelope{kev, native}, []facts.Envelope{native})
	if len(loader.filters) < 2 {
		t.Fatalf("filters = %#v, want at least two rounds", loader.filters)
	}
	first, second := loader.filters[0], loader.filters[1]
	if want := []string{cve}; !reflect.DeepEqual(first.UpstreamCVEIDs, want) || slices.Contains(first.CVEIDs, cve) {
		t.Errorf("round 0: CVEIDs = %v, UpstreamCVEIDs = %v, want %s on the upstream arm only", first.CVEIDs, first.UpstreamCVEIDs, cve)
	}
	if want := []string{cve}; !reflect.DeepEqual(second.CVEIDs, want) || len(second.UpstreamCVEIDs) != 0 {
		t.Errorf("round 1: CVEIDs = %v, UpstreamCVEIDs = %v, want %s on the full arm only", second.CVEIDs, second.UpstreamCVEIDs, cve)
	}
	if len(writer.write.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(writer.write.Findings), writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.CVSSScore != 9.8 || !got.KnownExploited {
		t.Errorf("cvss = %v, kev = %v, want the native score and the KEV the upstream arm brought", got.CVSSScore, got.KnownExploited)
	}
	if got.AdvisoryID != "DEBIAN-CVE-2023-38039" {
		t.Errorf("AdvisoryID = %q, want the record that decided: a scored source supplies the score without renaming", got.AdvisoryID)
	}

	// The follow-up subtracts what was requested, so the rounds above cannot show a
	// CVE asked for on both arms. The loaded set can: with a native fact loaded the
	// CVE is on the full arm only, whatever order the facts are read in, and with
	// only signals it stays on the upstream arm.
	for name, input := range map[string]struct {
		loaded       []facts.Envelope
		fullArm      bool
		upstreamList []string
	}{
		"native fact loaded": {loaded: append(slices.Clone(scope), kev, native), fullArm: true},
		"signal only":        {loaded: append(slices.Clone(scope), kev), upstreamList: []string{cve}},
	} {
		reversed := slices.Clone(input.loaded)
		slices.Reverse(reversed)
		for _, order := range [][]facts.Envelope{input.loaded, reversed} {
			filter := supplyChainImpactFilter(order)
			if slices.Contains(filter.CVEIDs, cve) != input.fullArm || !slices.Equal(filter.UpstreamCVEIDs, input.upstreamList) {
				t.Errorf("%s: CVEIDs = %v, UpstreamCVEIDs = %v, want full arm %v and upstream list %v",
					name, filter.CVEIDs, filter.UpstreamCVEIDs, input.fullArm, input.upstreamList)
			}
		}
	}

	// A native fact in the intent's own scope puts the CVE on one arm from round 0.
	loader, _ = runHandlerOverCatalogStub(t, append(slices.Clone(scope), native), []facts.Envelope{kev}, nil)
	if len(loader.filters) == 0 {
		t.Fatal("no active load ran")
	}
	if first := loader.filters[0]; !slices.Contains(first.CVEIDs, cve) || slices.Contains(first.UpstreamCVEIDs, cve) {
		t.Errorf("native fact in scope, round 0: CVEIDs = %v, UpstreamCVEIDs = %v, want %s on the full arm only", first.CVEIDs, first.UpstreamCVEIDs, cve)
	}
}
