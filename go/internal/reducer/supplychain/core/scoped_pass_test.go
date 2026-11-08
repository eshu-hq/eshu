// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scopedSeedLoader is the stub scope-fact loader plus the finding-row seed
// seam, so scoped-pass tests control both the advisory and the row seeds.
type scopedSeedLoader struct {
	*stubSupplyChainImpactFactLoader
	seeds   []ScopedFindingPackage
	seedErr error
	calls   int
}

func (l *scopedSeedLoader) ListScopedSupplyChainImpactFindingPackages(
	_ context.Context,
	_, _ string,
	cveIDs []string,
) ([]ScopedFindingPackage, error) {
	l.calls++
	if l.seedErr != nil {
		return nil, l.seedErr
	}
	var out []ScopedFindingPackage
	for _, seed := range l.seeds {
		for _, id := range cveIDs {
			if seed.CVEID == id {
				out = append(out, seed)
			}
		}
	}
	return out, nil
}

// scopedPassCount reads the pass_total counter for mode=scoped out of a
// test meter reader.
func scopedPassCount(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var out int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_pass_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				mode, _ := dp.Attributes.Value("mode")
				if mode.AsString() == passModeScoped {
					out += dp.Value
				}
			}
		}
	}
	return out
}

// differentialEnvelopeMatchesFilter mirrors the production reader's OR
// semantics over the CVE, package, and advisory arms, so the differential
// below loads what the filter admits and nothing else.
func differentialEnvelopeMatchesFilter(e facts.Envelope, filter SupplyChainImpactFactFilter) bool {
	in := func(values []string, hit string) bool {
		for _, value := range values {
			if value == hit {
				return true
			}
		}
		return false
	}
	payload := e.Payload
	for _, id := range payloadcore.PayloadStrings(payload, "cve_id", "cve_ids") {
		if in(filter.CVEIDs, id) {
			return true
		}
	}
	if id := payloadcore.PayloadStr(payload, "advisory_id"); in(filter.CVEIDs, id) || in(filter.AdvisoryIDs, id) {
		return true
	}
	if id := payloadcore.PayloadStr(payload, "osv_id"); in(filter.CVEIDs, id) {
		return true
	}
	if id := payloadcore.PayloadStr(payload, "package_id"); in(filter.PackageIDs, id) {
		return true
	}
	if id := payloadcore.PayloadStr(payload, "purl"); in(filter.PURLs, id) {
		return true
	}
	return false
}

// differentialFixture builds a two-CVE holder fixture: advisories, package
// evidence, and consumption joins for CVE-2026-9001 (pkg-a) and
// CVE-2026-9002 (pkg-b), each pair independent the way the measured QA
// holder rows are.
func differentialFixture() (scopeFacts, active []facts.Envelope) {
	scopeFacts = []facts.Envelope{
		vulnerabilityCVEFact("cve-a", "CVE-2026-9001", 9.8),
		vulnerabilityAffectedPackageFact("aff-a", "CVE-2026-9001", "pkg:npm/a@1", "npm", "a", "1.0.0", "2.0.0"),
		vulnerabilityCVEFact("cve-b", "CVE-2026-9002", 7.5),
		vulnerabilityAffectedPackageFact("aff-b", "CVE-2026-9002", "pkg:npm/b@1", "npm", "b", "1.0.0", "2.0.0"),
		packageVersionFact("ver-a", "pkg:npm/a@1", "pkg:npm/a@1.0.0", "1.0.0"),
		packageVersionFact("ver-b", "pkg:npm/b@1", "pkg:npm/b@1.0.0", "1.0.0"),
		packageConsumptionFactWithRange("con-a", "pkg:npm/a@1", "repo://example/a", "1.0.0"),
		packageConsumptionFactWithRange("con-b", "pkg:npm/b@1", "repo://example/b", "1.0.0"),
	}
	active = []facts.Envelope{
		vulnerabilityAffectedPackageFact("aff-a-x", "CVE-2026-9001", "pkg:npm/a@1", "npm", "a", "1.0.0", "2.0.0"),
		vulnerabilityAffectedPackageFact("aff-b-x", "CVE-2026-9002", "pkg:npm/b@1", "npm", "b", "1.0.0", "2.0.0"),
		packageVersionFact("ver-a-x", "pkg:npm/a@1", "pkg:npm/a@1.0.0", "1.0.0"),
		packageVersionFact("ver-b-x", "pkg:npm/b@1", "pkg:npm/b@1.0.0", "1.0.0"),
		packageConsumptionFactWithRange("con-a-x", "pkg:npm/a@1", "repo://example/a", "1.0.0"),
		packageConsumptionFactWithRange("con-b-x", "pkg:npm/b@1", "repo://example/b", "1.0.0"),
	}
	return scopeFacts, active
}

// runDifferentialPass runs one findings pass over the fixture with the given
// payload, returning the findings it derived and how many cross-scope
// evidence envelopes its active loads admitted.
func runDifferentialPass(t *testing.T, scopeFacts, active []facts.Envelope, seeds []ScopedFindingPackage, payload map[string]any) ([]SupplyChainImpactFinding, int) {
	t.Helper()
	pool := append([]facts.Envelope(nil), active...)
	admitted := 0
	loader := &scopedSeedLoader{
		stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{
			scopeFacts: scopeFacts,
			activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
				var out []facts.Envelope
				for _, e := range pool {
					if differentialEnvelopeMatchesFilter(e, filter) {
						out = append(out, e)
					}
				}
				admitted += len(out)
				return out
			},
		},
		seeds: seeds,
	}
	handler, _, writer := scopedPassHandler(loader, nil)
	intent := fanOutIntent(1, payload)
	intent.Payload = payload
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return writer.write.Findings, admitted
}

// findingsForCVE selects the findings naming one CVE, in stable order.
func findingsForCVE(findings []SupplyChainImpactFinding, cve string) []SupplyChainImpactFinding {
	var out []SupplyChainImpactFinding
	for _, finding := range findings {
		if finding.CVEID == cve {
			out = append(out, finding)
		}
	}
	return out
}

// TestScopedFindingsMatchFullFindingsForStampedCVEs is the #9779 evidence
// differential: over a two-CVE holder fixture, a scoped re-derivation for
// CVE-2026-9001 derives the same findings for that CVE as the full pass. The
// finding construction still sees the whole scope generation; only the
// cross-scope loads narrow, and the writer publishes the stamp only. If a
// finding for the stamped CVE depended on cross-scope evidence the scope
// leaves out, this test goes red and scoping is the wrong fix.
func TestScopedFindingsMatchFullFindingsForStampedCVEs(t *testing.T) {
	t.Parallel()

	scopeFacts, active := differentialFixture()
	seeds := []ScopedFindingPackage{{
		CVEID: "CVE-2026-9001", PackageID: "pkg:npm/a@1", PURL: "pkg:npm/a@1.0.0",
		PackageName: "a", Ecosystem: "npm",
	}}
	full, fullAdmitted := runDifferentialPass(t, scopeFacts, active, seeds, map[string]any{})
	scoped, scopedAdmitted := runDifferentialPass(t, scopeFacts, active, seeds, scopedStampPayload("CVE-2026-9001"))

	fullA := findingsForCVE(full, "CVE-2026-9001")
	if len(fullA) == 0 {
		t.Fatal("full pass derived no finding for CVE-2026-9001: the fixture proves nothing")
	}
	if len(findingsForCVE(full, "CVE-2026-9002")) == 0 {
		t.Fatal("full pass derived no finding for CVE-2026-9002: the fixture proves nothing")
	}
	scopedA := findingsForCVE(scoped, "CVE-2026-9001")
	if !reflect.DeepEqual(scopedA, fullA) {
		t.Fatalf("scoped findings for the stamp differ from the full pass:\nscoped=%+v\nfull=%+v", scopedA, fullA)
	}
	if scopedAdmitted >= fullAdmitted {
		t.Fatalf("scoped loads admitted %d envelopes, full %d: the scope narrowed nothing", scopedAdmitted, fullAdmitted)
	}
	t.Logf("active evidence admitted: full=%d scoped=%d", fullAdmitted, scopedAdmitted)
}

// TestScopedPassCVEIDsRunsFullOtherwise pins the full-pass legs: the
// sentinel, a stamp without a set, a later attempt, and an owed retry each
// run full, so only a bounded one-hop re-derivation ever scopes.
func TestScopedPassCVEIDsRunsFullOtherwise(t *testing.T) {
	t.Parallel()

	bounded := scopedStampPayload("CVE-2026-9001")
	if got := scopedPassCVEIDs(fanOutIntent(1, bounded)); !reflect.DeepEqual(got, []string{"CVE-2026-9001"}) {
		t.Fatalf("bounded one-hop scope = %v, want the stamp", got)
	}
	sentinel := scopedStampPayload(FanOutStampSentinel)
	if got := scopedPassCVEIDs(fanOutIntent(1, sentinel)); got != nil {
		t.Fatalf("sentinel scope = %v, want full", got)
	}
	if got := scopedPassCVEIDs(fanOutIntent(1, map[string]any{})); got != nil {
		t.Fatalf("unstamped scope = %v, want full", got)
	}
	stampOnly := map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope_id": "s"}}
	if got := scopedPassCVEIDs(fanOutIntent(1, stampOnly)); got != nil {
		t.Fatalf("set-less stamp scope = %v, want full", got)
	}
	if got := scopedPassCVEIDs(fanOutIntent(2, bounded)); got != nil {
		t.Fatalf("retry scope = %v, want full: a later attempt is genuine again", got)
	}
	owed := scopedStampPayload("CVE-2026-9001")
	owed[PayloadKeyFanOutOwed] = true
	if got := scopedPassCVEIDs(fanOutIntent(1, owed)); got != nil {
		t.Fatalf("owed scope = %v, want full: the retry may be establishing rows", got)
	}
}

// scopedStampPayload builds a one-hop claim payload stamping cves.
func scopedStampPayload(cves ...string) map[string]any {
	ids := make([]any, 0, len(cves))
	for _, id := range cves {
		ids = append(ids, id)
	}
	return map[string]any{PayloadKeyFanOutOrigin: map[string]any{
		"scope_id": "scope:src", "generation_id": "gen-src", "intent_id": "intent-src",
		"cves": ids,
	}}
}

// scopedPassHandler wires a handler with the seed loader and a recording
// writer for scoped-pass tests.
func scopedPassHandler(loader *scopedSeedLoader, inst *telemetry.Instruments) (*SupplyChainImpactHandler, *stubFanOut, *recordingSupplyChainImpactWriter) {
	port := &stubFanOut{payload: map[string]any{}}
	writer := &recordingSupplyChainImpactWriter{}
	handler := &SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             writer,
		FanOut:             port,
		Instruments:        inst,
	}
	return handler, port, writer
}

// TestScopedPassSeedsFromFindingRows pins the #9779 scoped pass: a one-hop
// pass whose stamp names a bounded set writes with that scope, restricts its
// active-evidence CVE arm to the stamp, and counts mode=scoped.
func TestScopedPassSeedsFromFindingRows(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &scopedSeedLoader{
		stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{
			scopeFacts: []facts.Envelope{
				vulnerabilityCVEFact("cve-1", "CVE-2026-9001", 9.8),
				vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-9001", "pkg:npm/example@1", "npm", "example", "1.2.3", "1.3.0"),
			},
		},
		seeds: []ScopedFindingPackage{{
			CVEID: "CVE-2026-9001", PackageID: "pkg:npm/example@1", PURL: "pkg:npm/example@1.2.3",
			PackageName: "example", Ecosystem: "npm",
		}},
	}
	handler, _, writer := scopedPassHandler(loader, inst)
	payload := scopedStampPayload("CVE-2026-9001")
	intent := fanOutIntent(1, payload)
	intent.Payload = payload
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(writer.write.ScopedCVEIDs, []string{"CVE-2026-9001"}) {
		t.Fatalf("write scope = %v, want the stamped set", writer.write.ScopedCVEIDs)
	}
	if loader.calls != 1 {
		t.Fatalf("seed reads = %d, want 1", loader.calls)
	}
	for _, filter := range loader.filters {
		for _, id := range filter.CVEIDs {
			if id != "CVE-2026-9001" {
				t.Fatalf("active filter CVE ids = %v, want the stamp only", filter.CVEIDs)
			}
		}
	}
	if got := result.SubSignals["scoped_cve_count"]; got != 1 {
		t.Fatalf("scoped_cve_count = %v, want 1", got)
	}
	if got := scopedPassCount(t, reader); got != 1 {
		t.Fatalf("pass_total{mode=scoped} = %d, want 1", got)
	}
}

// TestScopedPassBridgesDistroSpellingViaUpstream pins the #9779 suppression
// regression: a holder-side distro record spells its own id with the vendor
// prefix while the stamp carries the bare CVE. The seed must still admit the
// record through its upstream list (seeding the upstream and advisory sets),
// admit the finding row's deciding advisory, keep both in the derivation
// subset, and restrict the filter so the upstream arm survives while the
// distro id stays off the full CVE arm. An advisory for another CVE stays
// out on every leg.
func TestScopedPassBridgesDistroSpellingViaUpstream(t *testing.T) {
	t.Parallel()

	const distro, cve = "DEBIAN-CVE-2023-38039", "CVE-2023-38039"
	record := vulnerabilityCVEFact("rec-1", distro, 0)
	record.Payload["upstream"] = []string{cve}
	affected := vulnerabilityAffectedPackageFact("aff-1", distro, "os://debian/curl", "os", "curl", "0", "7.88.1-10+deb12u3")
	affected.Payload["purl"] = "pkg:deb/debian/curl?arch=source&distro=bookworm"
	other := vulnerabilityCVEFact("rec-2", "CVE-2024-0001", 9.8)
	seeds := []ScopedFindingPackage{{
		CVEID: cve, AdvisoryID: distro, PackageID: "os://debian/curl",
		PURL: "pkg:deb/debian/curl?arch=source&distro=bookworm", PackageName: "curl", Ecosystem: "os",
	}}
	stamp := []string{cve}

	scope, ok := seedScopedSupplyChainImpactScope([]facts.Envelope{record, affected, other}, seeds, stamp)
	if !ok {
		t.Fatal("seedScopedSupplyChainImpactScope() = false, want a scope: the bridged record and finding row must seed")
	}
	if _, ok := scope.upstream[cve]; !ok {
		t.Errorf("scope.upstream = %v, want %s from the record's upstream list", setKeys(scope.upstream), cve)
	}
	if _, ok := scope.advisoryIDs[distro]; !ok {
		t.Errorf("scope.advisoryIDs = %v, want %s from the record and finding row", setKeys(scope.advisoryIDs), distro)
	}

	stampSet := map[string]struct{}{cve: {}}
	for _, envelope := range []facts.Envelope{record, affected} {
		if !scopedEnvelopeIncluded(envelope, scope, stampSet) {
			t.Errorf("scopedEnvelopeIncluded(%s) = false, want true: the bridged advisory joins the subset", envelope.FactID)
		}
	}
	if scopedEnvelopeIncluded(other, scope, stampSet) {
		t.Error("scopedEnvelopeIncluded(rec-2) = true, want false: another CVE's advisory stays out")
	}

	subset := scopeSupplyChainImpactEnvelopes([]facts.Envelope{record, affected, other}, scope)
	filter := restrictSupplyChainImpactFilter(supplyChainImpactFilter(subset), scope)
	if !slices.Contains(filter.UpstreamCVEIDs, cve) {
		t.Errorf("restricted UpstreamCVEIDs = %v, want %s: the suppression arm must survive restriction", filter.UpstreamCVEIDs, cve)
	}
	if !slices.Contains(filter.AdvisoryIDs, distro) {
		t.Errorf("restricted AdvisoryIDs = %v, want %s: the record must stay loadable by advisory", filter.AdvisoryIDs, distro)
	}
	for _, id := range filter.CVEIDs {
		if id == distro {
			t.Errorf("restricted CVEIDs = %v: the distro id must stay off the full arm", filter.CVEIDs)
		}
	}
}

// TestScopedPassSeedsStampKeysOnUpstreamArm pins the mass-holder leg of the
// #9779 regression: a holder whose deciding record lives in another scope
// has no own-scope advisory spelling the stamp, so the seed must still open
// the upstream arm from the stamp's own canonical keys, with the advisory
// arm from the finding row's deciding advisory.
func TestScopedPassSeedsStampKeysOnUpstreamArm(t *testing.T) {
	t.Parallel()

	const distro, cve = "DEBIAN-CVE-2023-38039", "CVE-2023-38039"
	affected := vulnerabilityAffectedPackageFact("aff-1", distro, "os://debian/curl", "os", "curl", "0", "7.88.1-10+deb12u3")
	seeds := []ScopedFindingPackage{{
		CVEID: cve, AdvisoryID: distro, PackageID: "os://debian/curl", Ecosystem: "os",
	}}
	scope, ok := seedScopedSupplyChainImpactScope([]facts.Envelope{affected}, seeds, []string{cve})
	if !ok {
		t.Fatal("seedScopedSupplyChainImpactScope() = false, want a scope: the finding row must seed without any own-scope record")
	}
	if _, ok := scope.upstream[cve]; !ok {
		t.Errorf("scope.upstream = %v, want %s from the stamp itself", setKeys(scope.upstream), cve)
	}
	if _, ok := scope.advisoryIDs[distro]; !ok {
		t.Errorf("scope.advisoryIDs = %v, want %s from the finding row", setKeys(scope.advisoryIDs), distro)
	}
}
