// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reconcileNotInAdvisoryMissing is the missing evidence the binding arbiter
// ruling (issuecomment-6594799120, decision Q1.3) sets behind
// os_release_not_in_advisory: the advisory does not list the installed
// release.
const reconcileNotInAdvisoryMissing = "advisory does not list the installed release"

// osvUbuntuCurlFocalJammyFact is an OSV OS fact the converter emits with
// release tags: focal fixed at 7.68.0-1ubuntu2.20, jammy at
// 7.81.0-1ubuntu1.15. A noble host is on no listed line.
func osvUbuntuCurlFocalJammyFact() supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "UBUNTU-CVE-2025-99991", Source: "osv", AdvisoryID: "UBUNTU-CVE-2025-99991",
		PackageID: "os://ubuntu/curl", Ecosystem: "os", Name: "curl",
		PURL: "pkg:deb/ubuntu/curl?arch=source&distro=jammy",
		AffectedRanges: []supplychainmodel.AffectedRange{
			taggedRange("7.68.0-1ubuntu2.20", "ubuntu:20.04"),
			taggedRange("7.81.0-1ubuntu1.15", "ubuntu:22.04"),
		},
		FixedVersions: []string{"7.68.0-1ubuntu2.20", "7.81.0-1ubuntu1.15"},
	}
}

func nobleHostRow() supplychainmodel.OSPackage {
	return supplychainmodel.OSPackage{Distro: "ubuntu", DistroVersion: "24.04"}
}

// TestReconcileUnmatchedReleaseNotInAdvisory pins ruling decisions Q1.3-Q1.5:
// a tagged advisory that does not list the host's release yields
// possibly_affected with the new closed reason os_release_not_in_advisory,
// never known-fixed, never exact, never no-finding. Both rows run through the
// production chain (selectOSAdvisoryRelease, then the matcher fed the same
// single fixed version the finding builder derives).
func TestReconcileUnmatchedReleaseNotInAdvisory(t *testing.T) {
	t.Parallel()

	row := nobleHostRow()
	for _, tc := range []struct {
		name     string
		observed string
	}{
		{"listed range contains the unlisted host version", "7.81.0-1ubuntu1.10"},
		{"host above every listed fix is still not known fixed", "8.0.0-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected, decision := classifyTaggedOSFact(osvUbuntuCurlFocalJammyFact(), row, tc.observed)
			if !selected.ReleaseUnlisted {
				t.Fatalf("fact not marked ReleaseUnlisted")
			}
			if len(selected.FixedVersions) != 0 {
				t.Fatalf("unlisted fact keeps fixes at the boundary: %v, want blank", selected.FixedVersions)
			}
			if decision.Status != SupplyChainImpactPossiblyAffected ||
				decision.Reason != supplyChainVersionReasonOSReleaseNotInAdvisory {
				t.Fatalf("decision = %s/%s, want %s/%s",
					decision.Status, decision.Reason,
					SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
			}
			if len(decision.MissingEvidence) != 1 || decision.MissingEvidence[0] != reconcileNotInAdvisoryMissing {
				t.Fatalf("missing evidence = %v, want [%q]", decision.MissingEvidence, reconcileNotInAdvisoryMissing)
			}
			if decision.Status == SupplyChainImpactNotAffectedKnownFixed {
				t.Fatalf("unmatched release read known fixed (%s)", decision.Reason)
			}
		})
	}
}

// TestReconcileESMPocketTagNeverMatchesBaseHost pins ruling decisions Q1.8
// and Q1.18 at the reducer boundary: an ESM pocket tag ("ubuntu:pro:18.04")
// is distinct from the base release, so an 18.04 host reads the fact as
// not-in-advisory, never as a match and never as known-fixed.
func TestReconcileESMPocketTagNeverMatchesBaseHost(t *testing.T) {
	t.Parallel()

	fact := supplychainmodel.AffectedPackage{
		CVEID: "UBUNTU-CVE-2025-99992", Source: "osv", AdvisoryID: "UBUNTU-CVE-2025-99992",
		PackageID: "os://ubuntu/curl", Ecosystem: "os", Name: "curl",
		PURL:             "pkg:deb/ubuntu/curl?arch=source&distro=bionic",
		AffectedRanges:   []supplychainmodel.AffectedRange{taggedRange("7.58.0-2ubuntu3.24+esm1", "ubuntu:pro:18.04")},
		FixedVersions:    []string{"7.58.0-2ubuntu3.24+esm1"},
		AffectedVersions: []string{"7.58.0-2ubuntu3.23"},
	}
	host := supplychainmodel.OSPackage{Distro: "ubuntu", DistroVersion: "18.04"}
	selected, decision := classifyTaggedOSFact(fact, host, "7.58.0-2ubuntu3.23")
	if !selected.ReleaseUnlisted {
		t.Fatalf("fact not marked ReleaseUnlisted: pocket tag must not match the base host line")
	}
	if decision.Status != SupplyChainImpactPossiblyAffected ||
		decision.Reason != supplyChainVersionReasonOSReleaseNotInAdvisory {
		t.Fatalf("decision = %s/%s, want %s/%s",
			decision.Status, decision.Reason,
			SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
	}
}

// TestReconcileUnmatchedSingleFixNotKnownFixed pins the scalar-leak half of
// ruling decision Q1.3: a single-fix tagged fact that names no range for the
// host's line must not reach the already-fixed shortcut through the listed
// line's fix, even when the host sits above it.
func TestReconcileUnmatchedSingleFixNotKnownFixed(t *testing.T) {
	t.Parallel()

	fact := osvDebianCurlFactWith(taggedRange("7.88.1-10+deb12u3", "debian:12"))
	bullseye := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "11"}
	selected, decision := classifyTaggedOSFact(fact, bullseye, "9.0.0-1")
	if !selected.ReleaseUnlisted {
		t.Fatalf("fact not marked ReleaseUnlisted")
	}
	if len(selected.FixedVersions) != 0 {
		t.Fatalf("unlisted fact keeps its fix at the boundary: %v, want blank", selected.FixedVersions)
	}
	if decision.Status != SupplyChainImpactPossiblyAffected ||
		decision.Reason != supplyChainVersionReasonOSReleaseNotInAdvisory {
		t.Fatalf("decision = %s/%s, want %s/%s",
			decision.Status, decision.Reason,
			SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
	}
}

// TestReconcileNotInAdvisoryRemediationBlank pins ruling decisions Q2.9 and
// Q2.13 through the remediation production entry: a not-in-advisory finding
// keeps FirstPatchedVersion blank.
func TestReconcileNotInAdvisoryRemediationBlank(t *testing.T) {
	t.Parallel()

	remediation := BuildSupplyChainImpactRemediation(SupplyChainImpactFinding{
		Ecosystem:          "ubuntu",
		ObservedVersion:    "8.0.0-1",
		Status:             SupplyChainImpactPossiblyAffected,
		MatchReason:        supplyChainVersionReasonOSReleaseNotInAdvisory,
		FixedVersionSource: "osv",
	})
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want blank for a not-in-advisory row", remediation.FirstPatchedVersion)
	}
	if remediation.Reason != SupplyChainRemediationReasonPackageManagerUnsupported {
		t.Fatalf("Reason = %q, want package_manager_unsupported (reason carries no remediation support)", remediation.Reason)
	}
}

// TestReconcileF4TrixieNamesOwnFix pins ruling proof row F4: trixie 8.10.1-2
// against DEBIAN-CVE-2023-38039 reads known fixed (above trixie's own fix)
// and never names bookworm's fix. The selected boundary carries trixie's fix
// only; remediation stays blank for the OSV-sourced row.
func TestReconcileF4TrixieNamesOwnFix(t *testing.T) {
	t.Parallel()

	trixie := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "13"}
	selected, decision := classifyTaggedOSFact(osvDebianCurlFact(), trixie, "8.10.1-2")
	if selected.ReleaseLine != "debian:13" {
		t.Fatalf("ReleaseLine = %q, want the host's own line", selected.ReleaseLine)
	}
	if len(selected.FixedVersions) != 1 || selected.FixedVersions[0] != "8.3.0-1" {
		t.Fatalf("selected fixes = %v, want [8.3.0-1] (trixie's own fix)", selected.FixedVersions)
	}
	for _, fix := range selected.FixedVersions {
		if fix == "7.88.1-10+deb12u3" {
			t.Fatalf("bookworm's fix leaked into the host-selected set")
		}
	}
	if decision.Status != SupplyChainImpactNotAffectedKnownFixed {
		t.Fatalf("decision = %s/%s, want known fixed above trixie's own fix", decision.Status, decision.Reason)
	}
	remediation := BuildSupplyChainImpactRemediation(SupplyChainImpactFinding{
		Ecosystem:          "debian",
		ObservedVersion:    "8.10.1-2",
		Status:             SupplyChainImpactNotAffectedKnownFixed,
		MatchReason:        decision.Reason,
		FixedVersion:       "8.3.0-1",
		FixedVersionSource: "osv",
		FixedVersionBranches: []FixedVersionBranch{
			{Version: "8.3.0-1", Source: "osv"},
		},
	})
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want blank (never bookworm's fix)", remediation.FirstPatchedVersion)
	}
}

// TestReconcileUntaggedSilentFactOrderIndependent pins ruling decision Q1.5
// (weakest signal, order independent): a tagged-unlisted fact plus an
// untagged fact that says nothing still yields not-in-advisory, in both fact
// orders.
func TestReconcileUntaggedSilentFactOrderIndependent(t *testing.T) {
	t.Parallel()

	silent := supplychainmodel.AffectedPackage{
		CVEID: "UBUNTU-CVE-2025-99991", Source: "osv", AdvisoryID: "UBUNTU-CVE-2025-99991",
		PackageID: "os://ubuntu/curl", Ecosystem: "os", Name: "curl",
		PURL: "pkg:deb/ubuntu/curl?arch=source&distro=noble",
		AffectedRanges: []supplychainmodel.AffectedRange{
			{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "9.0.0-1"}}},
		},
	}
	for _, order := range [][]supplychainmodel.AffectedPackage{
		{osvUbuntuCurlFocalJammyFact(), silent},
		{silent, osvUbuntuCurlFocalJammyFact()},
	} {
		pkgs := selectOSAdvisoryRelease(order, nobleHostRow())
		var scalar string
		for _, pkg := range pkgs {
			if scalar == "" {
				scalar = singleFixedVersion(pkg.FixedVersions)
			}
		}
		decision := evaluateSupplyChainVersionMatch("os", "8.0.0-1", "", scalar, pkgs)
		if decision.Status != SupplyChainImpactPossiblyAffected ||
			decision.Reason != supplyChainVersionReasonOSReleaseNotInAdvisory {
			t.Fatalf("order %v: decision = %s/%s, want %s/%s",
				order, decision.Status, decision.Reason,
				SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
		}
	}
}

// TestReconcileUntaggedFixedKeepsPR2aBehavior pins ruling decision Q1.6: an
// untagged fact evaluated under the PR-2a rules still settles its own answer,
// and the tagged fact's non-coverage never vetoes it.
func TestReconcileUntaggedFixedKeepsPR2aBehavior(t *testing.T) {
	t.Parallel()

	untagged := supplychainmodel.AffectedPackage{
		CVEID: "UBUNTU-CVE-2025-99991", Source: "osv", AdvisoryID: "UBUNTU-CVE-2025-99991",
		PackageID: "os://ubuntu/curl", Ecosystem: "os", Name: "curl",
		PURL: "pkg:deb/ubuntu/curl?arch=source&distro=noble",
		AffectedRanges: []supplychainmodel.AffectedRange{
			{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "7.0.0-1"}}},
		},
		FixedVersions: []string{"7.0.0-1"},
	}
	for _, order := range [][]supplychainmodel.AffectedPackage{
		{osvUbuntuCurlFocalJammyFact(), untagged},
		{untagged, osvUbuntuCurlFocalJammyFact()},
	} {
		pkgs := selectOSAdvisoryRelease(order, nobleHostRow())
		var scalar string
		for _, pkg := range pkgs {
			if scalar == "" {
				scalar = singleFixedVersion(pkg.FixedVersions)
			}
		}
		decision := evaluateSupplyChainVersionMatch("os", "8.0.0-1", "", scalar, pkgs)
		if decision.Status != SupplyChainImpactNotAffectedKnownFixed {
			t.Fatalf("order %v: decision = %s/%s, want known fixed (untagged PR-2a answer stands)",
				order, decision.Status, decision.Reason)
		}
	}
}

// TestCountOSReleaseNotInAdvisoryGroupsByVendor pins the operator count
// behind eshu_dp_supply_chain_impact_os_release_not_in_advisory_total: only
// findings with the new closed reason count, grouped by the closed vendor
// label set, so an operator can watch the unmatched-release verdict rate.
func TestCountOSReleaseNotInAdvisoryGroupsByVendor(t *testing.T) {
	t.Parallel()

	findings := []SupplyChainImpactFinding{
		{PackageID: "os://debian/curl", MatchReason: supplyChainVersionReasonOSReleaseNotInAdvisory},
		{PackageID: "os://debian/openssl", MatchReason: supplyChainVersionReasonOSReleaseNotInAdvisory},
		{PackageID: "os://alpine/curl", MatchReason: supplyChainVersionReasonOSReleaseNotInAdvisory},
		{PackageID: "os://ubuntu/curl", MatchReason: supplyChainVersionReasonOSReleaseUnverified},
		{PackageID: "os://os/curl", MatchReason: supplyChainVersionReasonOSReleaseNotInAdvisory},
	}
	got := countOSReleaseNotInAdvisory(findings)
	want := map[string]int{"debian": 2, "alpine": 1, "other": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
	if got := countOSReleaseNotInAdvisory(nil); len(got) != 0 {
		t.Fatalf("counts for no findings = %v, want empty", got)
	}
}

// TestEmitOSReleaseNotInAdvisoryCarriesOnlySource pins the counter's closed
// label set: source (debian, ubuntu, alpine, other), one point per non-zero
// vendor cell.
func TestEmitOSReleaseNotInAdvisoryCarriesOnlySource(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := SupplyChainImpactHandler{Instruments: inst}
	handler.emitOSReleaseNotInAdvisory(context.Background(), map[string]int{
		"debian": 2, "alpine": 1, "ubuntu": 0,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_os_release_not_in_advisory_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 1 {
					t.Fatalf("data point labels = %v, want only source", point.Attributes)
				}
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				got[source.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"debian": 2, "alpine": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
}

// TestRecordBuildStatsWritesNotInAdvisorySubSignal pins the success-log
// sub-signal: the unmatched-release verdict count is written even when zero.
func TestRecordBuildStatsWritesNotInAdvisorySubSignal(t *testing.T) {
	t.Parallel()

	subSignals := map[string]float64{}
	SupplyChainImpactHandler{}.recordBuildStats(context.Background(), supplyChainImpactBuildStats{
		osReleaseNotInAdvisory: map[string]int{"ubuntu": 2},
	}, subSignals)
	if got, ok := subSignals[osReleaseNotInAdvisorySubSignal]; !ok || got != 2 {
		t.Fatalf("%s = %v (present %v), want 2", osReleaseNotInAdvisorySubSignal, got, ok)
	}

	empty := map[string]float64{}
	SupplyChainImpactHandler{}.recordBuildStats(context.Background(), supplyChainImpactBuildStats{}, empty)
	if got, ok := empty[osReleaseNotInAdvisorySubSignal]; !ok || got != 0 {
		t.Fatalf("%s = %v (present %v), want 0 on a clean pass", osReleaseNotInAdvisorySubSignal, got, ok)
	}
}

// TestReconcileAmbiguityBlankingBeforeAlreadyFixed pins ruling decision
// Q2.10: release filtering and multi-branch ambiguity blanking run before the
// already-fixed shortcut, so several host-line branches naming different
// fixes blank the answer instead of presenting one branch's fix.
func TestReconcileAmbiguityBlankingBeforeAlreadyFixed(t *testing.T) {
	t.Parallel()

	remediation := BuildSupplyChainImpactRemediation(SupplyChainImpactFinding{
		Ecosystem:          "debian",
		ObservedVersion:    "1.2.3-1",
		Status:             SupplyChainImpactNotAffectedKnownFixed,
		MatchReason:        supplyChainVersionReasonDPKGKnownFixed,
		FixedVersion:       "1.2.3-2",
		FixedVersionSource: "debian",
		FixedVersionBranches: []FixedVersionBranch{
			{Version: "1.2.3-2", Source: "debian"},
			{Version: "1.3.0-1", Source: "debian"},
		},
	})
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want blank: ambiguity blanks before already-fixed", remediation.FirstPatchedVersion)
	}
	if remediation.Reason != SupplyChainRemediationReasonPackageManagerUnsupported {
		t.Fatalf("Reason = %q, want package_manager_unsupported", remediation.Reason)
	}
	if !containsRemediationMissingEvidence(remediation, SupplyChainRemediationMissingFixedBranchAmbiguous) {
		t.Fatalf("MissingEvidence = %#v, want fixed branch ambiguity evidence", remediation.MissingEvidence)
	}
}
