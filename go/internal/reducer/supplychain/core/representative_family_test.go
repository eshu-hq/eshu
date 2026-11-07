// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// RED for #10034 item 2: the version family comes from the joined row's
// package manager, not from the first purl in fact order.
//
// Shape A (borrowed purl): a blank-ecosystem fact that joins the host's row
// carries no purl, and the only OS purl in the group belongs to a fact that
// cannot join. The group is decided by the joining fact alone, so the family
// is the row's (dpkg) and the verdict equals the control without the other
// fact: affected_exact on the exact-version list, not unsupported_ecosystem.
//
// Shape B (foreign purl, all join): on a Debian host a blank vendor fact
// beside a blank fact from debian that carries a pkg:apk/alpine/curl purl
// joins the same row through the package id. Both fact orders must read the
// row's family (dpkg) and agree with each other.

// familyBorrowedScenario is test 4.4's borrowed shape: the blank joining fact
// lists the installed version, the debian-spelled fact that cannot join holds
// the only OS purl.
func familyBorrowedScenario() repDirectScenario {
	return repDirectBorrowedScenario()
}

// TestJoinedRowSuppliesTheVersionFamily is shape A: the borrowed group reads
// the row's dpkg verdict, equal to the control without the fact that cannot
// join. The blank joining fact carries no purl, so its line is unreadable and
// it holds no release evidence: the dpkg matcher reads os_release_unverified,
// not exact (#9945). The point is the family, not the fix: unsupported is
// gone, and the verdict equals the control in every order.
func TestJoinedRowSuppliesTheVersionFamily(t *testing.T) {
	t.Parallel()
	sc := familyBorrowedScenario()
	check := func(t *testing.T, runs, controls []repDirectRun) {
		t.Helper()
		assertRepDirectEqualsControl(t, runs, controls)
		for _, run := range runs {
			got := run.findings[0]
			if got.Status != SupplyChainImpactPossiblyAffected || got.MatchReason != supplyChainVersionReasonOSReleaseUnverified {
				t.Fatalf("order %s: verdict = %s/%s, want %s/%s", run.order, got.Status, got.MatchReason,
					SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseUnverified)
			}
		}
	}
	t.Run("fleet", func(t *testing.T) { check(t, sc.fleet(t, true), sc.fleet(t, false)) })
	t.Run("authorized", func(t *testing.T) { check(t, sc.authorized(t, true), sc.authorized(t, false)) })
}

// familyForeignPURLScenario is shape B: both blank facts share the Debian
// package id so both join the host row; one carries a foreign apk purl.
func familyForeignPURLScenario() repDirectScenario {
	debPkg := "pkg:deb/debian/curl"
	vendor := facts.Envelope{FactID: "affected-vendor", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "DSA-9990-1", "source": "debian",
		"package_id": debPkg, "package_name": "curl", "purl": "pkg:deb/debian/curl?arch=amd64&distro=debian-12",
		"affected_versions": []any{repJoinVersion},
	}}
	foreign := facts.Envelope{FactID: "affected-foreign", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": vendorLineCVE, "advisory_id": "DSA-9990-1", "source": "debian",
		"package_id": debPkg, "package_name": "curl", "purl": "pkg:apk/alpine/curl?arch=x86_64",
		"affected_versions": []any{repJoinVersion},
	}}
	return repDirectScenario{
		own:         vendor,
		other:       foreign,
		otherSource: "debian",
		host:        debianCurl("12", repJoinVersion),
	}
}

// TestForeignPURLDoesNotDecideTheVersionFamily is shape B: every fact joins,
// so both fact orders read the row's dpkg verdict and agree.
func TestForeignPURLDoesNotDecideTheVersionFamily(t *testing.T) {
	t.Parallel()
	sc := familyForeignPURLScenario()
	for _, withOther := range []bool{true, false} {
		runs := sc.fleet(t, withOther)
		want := repDirectSummary(runs[0].findings)
		for _, run := range runs[1:] {
			if got := repDirectSummary(run.findings); got != want {
				t.Fatalf("withOther %v: order %s reads %s, order %s reads %s",
					withOther, run.order, got, runs[0].order, want)
			}
		}
		for _, run := range runs {
			if len(run.findings) != 1 {
				t.Fatalf("withOther %v order %s: findings = %d, want 1", withOther, run.order, len(run.findings))
			}
			got := run.findings[0]
			if got.Status != SupplyChainImpactAffectedExact || got.MatchReason != supplyChainVersionReasonDPKGExactAffected {
				t.Fatalf("withOther %v order %s: verdict = %s/%s, want %s/%s", withOther, run.order,
					got.Status, got.MatchReason, SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGExactAffected)
			}
		}
	}
}

// TestDisagreeingPURLsWithoutARowStayUnsupported pins the fallback: with no
// joined row there is no row manager to read, and purls that name several
// families name no family, so the verdict stays unsupported in every order.
func TestDisagreeingPURLsWithoutARowStayUnsupported(t *testing.T) {
	t.Parallel()
	deb := supplychainmodel.AffectedPackage{PURL: "pkg:deb/debian/curl?arch=amd64&distro=debian-12"}
	apk := supplychainmodel.AffectedPackage{PURL: "pkg:apk/alpine/curl?arch=x86_64"}
	for _, order := range [][]supplychainmodel.AffectedPackage{{deb, apk}, {apk, deb}} {
		decision := evaluateSupplyChainVersionMatchForRow("", repJoinVersion, "", "", order, "")
		if decision.Reason != supplyChainVersionReasonUnsupportedEcosystem {
			t.Fatalf("order %v: reason = %q, want unsupported_ecosystem", order, decision.Reason)
		}
		if decision.Status != SupplyChainImpactPossiblyAffected {
			t.Fatalf("order %v: status = %q, want possibly_affected", order, decision.Status)
		}
	}
}

// TestUnanimousPURLsYieldToTheJoinedRow pins the precedence the neighboring
// tests leave open (#10034 item 2): the joined row's package manager is host
// truth for a blank or "os" ecosystem even when the group purls unanimously
// name another family. Two unanimous apk purls with a dpkg row read the dpkg
// matcher (not apk, not unsupported), and unanimous deb purls with an apk
// row read the apk matcher, in both purl orders. Observed equals the fix, so
// each direction answers with its own matcher's known-fixed reason; the
// purl-first mutant (row manager as fallback) answers with the purl family's
// reason instead and fails here while passing the neighboring tests.
func TestUnanimousPURLsYieldToTheJoinedRow(t *testing.T) {
	t.Parallel()
	const version = "7.88.1-r0"
	apkA := supplychainmodel.AffectedPackage{PURL: "pkg:apk/alpine/curl?arch=x86_64"}
	apkB := supplychainmodel.AffectedPackage{PURL: "pkg:apk/alpine/curl?arch=aarch64"}
	debA := supplychainmodel.AffectedPackage{PURL: "pkg:deb/debian/curl?arch=amd64&distro=debian-12"}
	debB := supplychainmodel.AffectedPackage{PURL: "pkg:deb/debian/curl?arch=arm64&distro=debian-12"}
	cases := []struct {
		name       string
		ecosystem  string
		manager    string
		first      supplychainmodel.AffectedPackage
		second     supplychainmodel.AffectedPackage
		wantReason string
	}{
		{"apk purls dpkg row", "", "dpkg", apkA, apkB, supplyChainVersionReasonDPKGExactKnownFixed},
		{"apk purls dpkg row reversed", "", "dpkg", apkB, apkA, supplyChainVersionReasonDPKGExactKnownFixed},
		{"apk purls dpkg row os ecosystem", "os", "dpkg", apkA, apkB, supplyChainVersionReasonDPKGExactKnownFixed},
		{"apk purls dpkg row os ecosystem reversed", "os", "dpkg", apkB, apkA, supplyChainVersionReasonDPKGExactKnownFixed},
		{"deb purls apk row", "", "apk", debA, debB, supplyChainVersionReasonAPKExactKnownFixed},
		{"deb purls apk row reversed", "", "apk", debB, debA, supplyChainVersionReasonAPKExactKnownFixed},
		{"deb purls apk row os ecosystem", "os", "apk", debA, debB, supplyChainVersionReasonAPKExactKnownFixed},
		{"deb purls apk row os ecosystem reversed", "os", "apk", debB, debA, supplyChainVersionReasonAPKExactKnownFixed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := evaluateSupplyChainVersionMatchForRow(tc.ecosystem, version, "", version,
				[]supplychainmodel.AffectedPackage{tc.first, tc.second}, tc.manager)
			if got.Status != SupplyChainImpactNotAffectedKnownFixed || got.Reason != tc.wantReason {
				t.Fatalf("unanimous purls with %s row: verdict = %s/%s, want %s/%s",
					tc.manager, got.Status, got.Reason,
					SupplyChainImpactNotAffectedKnownFixed, tc.wantReason)
			}
		})
	}
}
