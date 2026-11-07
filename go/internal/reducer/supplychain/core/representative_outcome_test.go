// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// repJoinComponentEvidence is a verified image subject for the package: an SBOM
// component, its attachment and the image identity it resolves through.
func repJoinComponentEvidence() []facts.Envelope {
	const digest = "sha256:" + "9990999099909990999099909990999099909990999099909990999099909990"
	return []facts.Envelope{
		sbomComponentImpactFactWithPackageIDVersion("component-9990", "doc-9990", repJoinPackageID+"@"+repJoinVersion, repJoinPackageID, repJoinVersion),
		sbomAttachmentImpactFact("attachment-9990", "doc-9990", digest),
		containerImageIdentityImpactFact("image-9990", digest, "repository:r_9990"),
	}
}

// TestOSRepresentativeOutcomeNamesWhatEachGroupJoined pins the operator signal
// at the builder (#9990): every OS-shaped group lands in exactly one outcome, the
// group that yields no finding is counted, and a language group is not counted.
func TestOSRepresentativeOutcomeNamesWhatEachGroupJoined(t *testing.T) {
	t.Parallel()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	host := ubuntuCurl("22.04", repJoinVersion).envelope()
	nvd := repJoinOtherFact("affected-nvd", "nvd", "deb")
	debian := repJoinOtherFact("affected-debian", "debian", "deb")
	consumption := packageConsumptionFactWithRange("consume-9990", repJoinPackageID, "repository:r_9990", repJoinVersion)
	for _, tc := range []struct {
		name      string
		envelopes []facts.Envelope
		findings  int
		want      map[osRepresentativeOutcomeKey]int
	}{
		{
			"source order joins",
			[]facts.Envelope{cve, repJoinOwnFact("ubuntu"), repJoinOtherFact("affected-nvd", "nvd", "ubuntu"), host},
			1,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeJoined}: 1},
		},
		{
			"a lone joining fact",
			[]facts.Envelope{cve, repJoinOwnFact("deb"), host},
			1,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeJoined}: 1},
		},
		{
			"another fact joins",
			[]facts.Envelope{cve, repJoinOwnFact("deb"), nvd, host},
			1,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeReselected}: 1},
		},
		{
			// The R5 shape (arbiter ruling 9914-r5, decision 1): the ubuntu
			// record is withdrawn but joins, the live nvd fact cannot anchor,
			// so the liveness filter keeps the retracted record's fact and the
			// group counts joined.
			"a live fact that cannot join beside a withdrawn fact that can",
			[]facts.Envelope{
				unjoinableRetractedCVE("withdrawn"), unjoinableLiveCVE("nvd"),
				unjoinableRetractedPackage("deb"), unjoinableLivePackage("nvd", "deb", ""), host,
			},
			1,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeJoined}: 1},
		},
		{
			"a consumer anchors the group",
			[]facts.Envelope{cve, repJoinOwnFact("deb"), debian, host, consumption},
			1,
			map[osRepresentativeOutcomeKey]int{{"debian", osRepresentativeOtherAnchor}: 1},
		},
		{
			"an SBOM subject anchors the group", append([]facts.Envelope{cve, repJoinOwnFact("deb"), debian, host}, repJoinComponentEvidence()...), 1,
			map[osRepresentativeOutcomeKey]int{{"debian", osRepresentativeOtherAnchor}: 1},
		},
		{
			"rows exist and no fact can join",
			[]facts.Envelope{cve, nvd, debian, host},
			0,
			map[osRepresentativeOutcomeKey]int{{"debian", osRepresentativeNoJoinable}: 1},
		},
		{
			"a lone fact that cannot join",
			[]facts.Envelope{cve, nvd, host},
			0,
			map[osRepresentativeOutcomeKey]int{{"other", osRepresentativeNoJoinable}: 1},
		},
		{
			"a vendor fact whose ecosystem does not meet the row",
			[]facts.Envelope{cve, repJoinOwnFact("rpm"), host},
			0,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeNoJoinable}: 1},
		},
		{
			"no row under the group's keys",
			[]facts.Envelope{cve, repJoinOwnFact("deb"), nvd, debianCurl("12", repJoinVersion).envelope()},
			0,
			map[osRepresentativeOutcomeKey]int{{"other", osRepresentativeNoInstalledRow}: 1},
		},
		{
			"nothing installed at all",
			[]facts.Envelope{cve, repJoinOwnFact("deb"), nvd},
			0,
			map[osRepresentativeOutcomeKey]int{{"other", osRepresentativeNoInstalledRow}: 1},
		},
		{
			// #10034 item 3: an os:// identity with no purl can meet no
			// purl-keyed row even when the package is installed, so it counts
			// its own outcome instead of no_installed_row.
			"an identity that can meet no installed row",
			[]facts.Envelope{cve, repJoinNoPURLFact("os://ubuntu/curl"), debianCurl("12", repJoinVersion).envelope()},
			0,
			map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeNoJoinableIdentity}: 1},
		},
		{"a language group", []facts.Envelope{
			cve,
			vulnerabilityAffectedPackageFactWithSource("affected-npm", vendorLineCVE, "ghsa", "GHSA-9990", "pkg:npm/example", "npm", "example", "1.2.3", "1.3.0"),
			host,
		}, 0, map[osRepresentativeOutcomeKey]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range repJoinPermutations(tc.envelopes[:min(len(tc.envelopes), 4)]) {
				order = append(order, tc.envelopes[min(len(tc.envelopes), 4):]...)
				findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(order)
				if err != nil {
					t.Fatalf("build findings: %v", err)
				}
				if len(findings) != tc.findings {
					t.Fatalf("findings = %d, want %d: %#v", len(findings), tc.findings, findings)
				}
				if !maps.Equal(stats.osRepresentativeOutcomes, tc.want) {
					t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, tc.want)
				}
			}
		})
	}
}

// TestRepresentativeWithAnAnchorIsNotMoved pins the parity rule (#9990): a group
// that has an anchor produces a finding today, so its source-order representative
// stays even though another fact could join the installed row. The row stays off
// the finding's evidence, exactly as it is without the reselection.
func TestRepresentativeWithAnAnchorIsNotMoved(t *testing.T) {
	t.Parallel()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	host := ubuntuCurl("22.04", repJoinVersion).envelope()
	own := repJoinOwnFact("deb")
	nvd := repJoinOtherFact("affected-nvd", "nvd", "deb")
	consumption := packageConsumptionFactWithRange("consume-9990", repJoinPackageID, "repository:r_9990", repJoinVersion)
	for name, envelopes := range map[string][]facts.Envelope{
		"consumer":     {cve, own, nvd, host, consumption},
		"SBOM subject": append([]facts.Envelope{cve, own, nvd, host}, repJoinComponentEvidence()...),
	} {
		findings := BuildSupplyChainImpactFindings(envelopes)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1", name, len(findings))
		}
		if slices.Contains(findings[0].EvidencePath, chain.VulnerabilityOSPackageFactKind) {
			t.Fatalf("%s: evidence path %v, want the source-order representative's path with no installed row", name, findings[0].EvidencePath)
		}
	}
}

// TestReselectedFactMeetsEveryImageSubject pins the re-read of the SBOM paths after
// reselection. The source-order fact has no purl, so it meets no component that
// names the package by purl alone. The reselected fact does, and each image
// subject then classifies on its own (#9990).
func TestReselectedFactMeetsEveryImageSubject(t *testing.T) {
	t.Parallel()
	const (
		digestOne = "sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
		digestTwo = "sha256:" + "2222222222222222222222222222222222222222222222222222222222222222"
	)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		repJoinOwnFact("deb"),
		repJoinOtherFact("affected-nvd", "nvd", "deb"),
		ubuntuCurl("22.04", repJoinVersion).envelope(),
		sbomComponentImpactFact("component-one", "doc-one", repJoinPURL),
		sbomAttachmentImpactFact("attachment-one", "doc-one", digestOne),
		containerImageIdentityImpactFact("image-one", digestOne, "repository:r_one"),
		sbomComponentImpactFact("component-two", "doc-two", repJoinPURL),
		sbomAttachmentImpactFact("attachment-two", "doc-two", digestTwo),
		containerImageIdentityImpactFact("image-two", digestTwo, "repository:r_two"),
	})
	digests := map[string]bool{}
	for _, finding := range findings {
		digests[finding.SubjectDigest] = true
	}
	if !digests[digestOne] || !digests[digestTwo] {
		t.Fatalf("subjects %v, want a finding for each image: %#v", digests, findings)
	}
}

// TestAffectedFactHasOSShapeReadsEveryOSSpelling pins the group filter on the
// outcome signal, one arm per spelling, with language shapes as the controls.
func TestAffectedFactHasOSShapeReadsEveryOSSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pkg  supplychainmodel.AffectedPackage
		want bool
	}{
		{"OS ecosystem", supplychainmodel.AffectedPackage{Ecosystem: "deb", PackageID: "curl"}, true},
		{"package manager ecosystem", supplychainmodel.AffectedPackage{Ecosystem: "dpkg", PackageID: "curl"}, true},
		{"OS purl", supplychainmodel.AffectedPackage{PURL: "pkg:apk/alpine/curl", PackageID: "curl"}, true},
		{"OS package id", supplychainmodel.AffectedPackage{PackageID: "pkg:rpm/alma/curl"}, true},
		{"os:// package id", supplychainmodel.AffectedPackage{PackageID: "OS://debian/curl"}, true},
		{"an OSV fact counts", supplychainmodel.AffectedPackage{Source: "osv", Ecosystem: "os", PackageID: "curl"}, true},
		{"npm", supplychainmodel.AffectedPackage{Ecosystem: "npm", PackageID: "pkg:npm/curl", PURL: "pkg:npm/curl"}, false},
		{"nothing", supplychainmodel.AffectedPackage{PackageID: "curl"}, false},
	} {
		if got := affectedFactHasOSShape(tc.pkg); got != tc.want {
			t.Errorf("%s: affectedFactHasOSShape = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// osPackageRowForTest is an index holding the fixture Ubuntu row.
func osPackageRowForTest(t *testing.T) supplyChainImpactIndex {
	t.Helper()
	index, _, err := buildSupplyChainImpactIndexWithQuarantine([]facts.Envelope{ubuntuCurl("22.04", repJoinVersion).envelope()})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return index
}

// TestReselectedRepresentativeKeepsRetractionRules pins that reselection reads no
// retraction state of its own (#9990): a withdrawn record reads on the finding
// exactly as on the shape whose own fact wins source order, the ubuntu ecosystem.
func TestReselectedRepresentativeKeepsRetractionRules(t *testing.T) {
	t.Parallel()
	const withdrawnAt = "2026-11-01T00:00:00Z"
	ownCVE := func(withdrawn bool) facts.Envelope {
		if withdrawn {
			return vulnerabilityCVEFactWithdrawn("cve-own", vendorLineCVE, "ubuntu", "USN-9990-1", 7.5, "HIGH", "2026-10-01T00:00:00Z", withdrawnAt)
		}
		return vulnerabilityCVEFactWithProvenance("cve-own", vendorLineCVE, "ubuntu", "USN-9990-1", 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	}
	otherCVE := func(withdrawn bool) facts.Envelope {
		if withdrawn {
			return vulnerabilityCVEFactWithdrawn("cve-other", vendorLineCVE, "nvd", "EXT-affected-other", 7.5, "HIGH", "2026-10-01T00:00:00Z", withdrawnAt)
		}
		return vulnerabilityCVEFactWithProvenance("cve-other", vendorLineCVE, "nvd", "EXT-affected-other", 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	}
	for _, tc := range []struct {
		name          string
		own, other    bool
		wantRetracted bool
	}{
		{"every source withdrew", true, true, true},
		// A group whose only joining record is withdrawn keeps its finding with
		// the retraction shown (arbiter ruling 9914-r5, decision 1): the live
		// record's fact has no anchor, so the finding is the retracted
		// record's, exactly as if the live record listed no package.
		{"only the joining source withdrew", true, false, true},
		{"only the other source withdrew", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			build := func(ecosystem string, withOther bool) SupplyChainImpactFinding {
				envs := []facts.Envelope{ownCVE(tc.own), otherCVE(tc.other), repJoinOwnFact(ecosystem)}
				if withOther {
					envs = append(envs, repJoinOtherFact("affected-other", "nvd", ecosystem))
				}
				envs = append(envs, ubuntuCurl("22.04", repJoinVersion).envelope())
				findings := BuildSupplyChainImpactFindings(envs)
				if len(findings) != 1 {
					t.Fatalf("ecosystem %s withOther %v: findings = %d, want 1", ecosystem, withOther, len(findings))
				}
				return findings[0]
			}
			reselected := build("deb", true)
			// The reselected group is narrowed to the facts that join, so only
			// the ubuntu record lists the package there: the control is the
			// group without the nvd fact, and the retraction is judged over the
			// records listing the package (9914-r5 decision 1,
			// recordsListingThePackage; 9990-f6 5.4). The other rows keep the
			// whole-group ubuntu control.
			control := build("ubuntu", true)
			if tc.name == "every source withdrew" {
				control = build("deb", false)
			}
			// The reselected row is named by the record that decided it (f156
			// decision 1): deb with nvd reads USN-9990-1 on the merged tree.
			if reselected.AdvisoryID != "USN-9990-1" {
				t.Fatalf("advisory = %q, want USN-9990-1", reselected.AdvisoryID)
			}
			if got := reselected.AdvisoryRetraction != nil; got != tc.wantRetracted {
				t.Fatalf("retracted = %v, want %v", got, tc.wantRetracted)
			}
			if !reflect.DeepEqual(reselected.AdvisoryRetraction, control.AdvisoryRetraction) {
				t.Fatalf("retraction = %+v, control %+v", reselected.AdvisoryRetraction, control.AdvisoryRetraction)
			}
		})
	}
}

// TestRepresentativeReselectionIsIdempotent builds the same facts twice: the mark
// rides on a per-call copy, so nothing carries from one build to the next.
func TestRepresentativeReselectionIsIdempotent(t *testing.T) {
	t.Parallel()
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5), repJoinOwnFact("deb"),
		repJoinOtherFact("affected-other", "nvd", "deb"), ubuntuCurl("22.04", repJoinVersion).envelope(),
	}
	first := BuildSupplyChainImpactFindings(slices.Clone(envelopes))
	second := BuildSupplyChainImpactFindings(slices.Clone(envelopes))
	if len(first) != 1 || !reflect.DeepEqual(first, second) {
		t.Fatalf("builds differ or are empty:\n%#v\n%#v", first, second)
	}
}

// TestAuthorizedPassFoldsRepresentativeOutcomesPerTarget pins the per-index count
// on the authorized path: the static index holds no row, each target index holds
// its own, and the pass total carries one point per group per index.
func TestAuthorizedPassFoldsRepresentativeOutcomesPerTarget(t *testing.T) {
	t.Parallel()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	cve.Payload["source"], cve.Payload["advisory_id"] = "ubuntu", "USN-9990-1"
	advisories := []facts.Envelope{
		repJoinAdvisory(cve, "ubuntu"),
		repJoinAdvisory(repJoinOwnFact("deb"), "ubuntu"),
		repJoinAdvisory(repJoinOtherFact("affected-nvd", "nvd", "deb"), "nvd"),
	}
	authority := []AdvisoryAuthorityDependency{repJoinAuthority("ubuntu"), repJoinAuthority("nvd")}
	loaded := supplyChainImpactLoadedEvidence{
		envelopes: advisories,
		asAuthorized: &AuthorizedSupplyChainImpactLoad{Targets: []AuthorizedScanImpactEvidence{
			authorizedOSVTarget(0, advisories, authority, ubuntuCurl("22.04", repJoinVersion)),
			authorizedOSVTarget(1, advisories, authority, debianCurl("12", repJoinVersion)),
		}},
	}
	findings, _, stats, err := buildAuthorizedSupplyChainImpactFindings(loaded)
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 for the Ubuntu target: %#v", len(findings), findings)
	}
	want := map[osRepresentativeOutcomeKey]int{
		{"ubuntu", osRepresentativeReselected}:    1,
		{"other", osRepresentativeNoInstalledRow}: 2,
	}
	if !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, want)
	}
}

// TestRecordBuildStatsWritesOSRepresentativeSignals pins both surfaces: the
// sub-signals are written even when zero, and the counter carries only its closed
// labels, one point per non-zero cell.
func TestRecordBuildStatsWritesOSRepresentativeSignals(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	subSignals := map[string]float64{}
	SupplyChainImpactHandler{Instruments: inst}.recordBuildStats(context.Background(), supplyChainImpactBuildStats{
		osRepresentativeOutcomes: map[osRepresentativeOutcomeKey]int{
			{"ubuntu", osRepresentativeReselected}: 2,
			{"debian", osRepresentativeNoJoinable}: 3,
			{"other", osRepresentativeNoJoinable}:  1,
			{"alpine", osRepresentativeJoined}:     4,
			{"other", osRepresentativeOtherAnchor}: 5,
			{"rocky", osRepresentativeOtherAnchor}: 0,
		},
	}, subSignals)
	for key, want := range map[string]float64{
		osRepresentativesJoinedSubSignal:         4,
		osRepresentativesReselectedSubSignal:     2,
		osRepresentativesOtherAnchorSubSignal:    5,
		osRepresentativesNoJoinableSubSignal:     4,
		osRepresentativesNoInstalledRowSubSignal: 0,
	} {
		if got, ok := subSignals[key]; !ok || got != want {
			t.Errorf("%s = %v (present %v), want %v", key, got, ok, want)
		}
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_os_representative_joins_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("data point labels = %v, want only source and outcome", point.Attributes)
				}
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				outcome, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				got[source.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{
		"ubuntu/reselected": 2, "debian/no_joinable_representative": 3,
		"other/no_joinable_representative": 1, "alpine/joined": 4, "other/other_anchor": 5,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
}

// plainIDFact is a fact on the package id "curl". The id has no OS shape, so only
// the fact's own ecosystem or purl can give the group one.
func plainIDFact(factID, purl string) facts.Envelope {
	payload := map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "EXT-" + factID, "source": "nvd",
		"package_id": "curl", "package_name": "curl", "fixed_versions": []any{"1.0-9"},
	}
	if purl != "" {
		payload["purl"] = purl
	}
	return facts.Envelope{FactID: factID, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: payload}
}

// TestOutcomeReadsEveryFactOfTheGroup pins that the outcome reads the group, not only
// the fact that names it: the representative "a-named" has no OS shape and no row under
// its keys, and "b-purl" gives the group both, so the group counts as rows no fact joins.
func TestOutcomeReadsEveryFactOfTheGroup(t *testing.T) {
	t.Parallel()
	_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		plainIDFact("a-named", ""),
		plainIDFact("b-purl", repJoinPURL),
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	want := map[osRepresentativeOutcomeKey]int{{"other", osRepresentativeNoJoinable}: 1}
	if !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("outcomes = %v, want %v", stats.osRepresentativeOutcomes, want)
	}
}

// TestLoneOSVFactLabelsThePURLVendor pins the source label on the only OS shape a
// producer writes today: an OSV fact spelled os counts under its purl's vendor, not
// under other.
func TestLoneOSVFactLabelsThePURLVendor(t *testing.T) {
	t.Parallel()
	osv := facts.Envelope{FactID: "affected-osv", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "OSV-9990", "source": "osv", "ecosystem": "os",
		"package_id": repJoinPackageID, "package_name": "curl", "purl": repJoinPURL,
		"fixed_versions": []any{"1.0-5"},
	}}
	findings, _, stats, err := buildSupplyChainImpactFindingsAndExclusions([]facts.Envelope{
		vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5),
		osv,
		ubuntuCurl("22.04", repJoinVersion).envelope(),
	})
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	want := map[osRepresentativeOutcomeKey]int{{"ubuntu", osRepresentativeJoined}: 1}
	if len(findings) != 1 || !maps.Equal(stats.osRepresentativeOutcomes, want) {
		t.Fatalf("findings = %d, outcomes = %v, want 1 and %v", len(findings), stats.osRepresentativeOutcomes, want)
	}
}
