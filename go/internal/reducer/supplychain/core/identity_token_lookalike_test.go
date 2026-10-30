// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestSupplyChainIdentityTokensKeepLookalikesApart pins #9584 for the
// ecosystem, package-manager, distro, vendor-source, and package-name tokens
// the supply-chain reducer compares: each folds ASCII letters only, so a token
// spelled with U+0130 or U+212A is unknown rather than the ASCII token it
// imitates. ASCII case variants still match.
func TestSupplyChainIdentityTokensKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	const (
		alpine = "Alp\u0130ne"
		debian = "Deb\u0130an"
		apk    = "AP\u212A"
		rocky  = "Roc\u212Ay"
	)
	if got := osRemediationFamily(alpine); got != "" {
		t.Errorf("osRemediationFamily(%q) = %q, want empty", alpine, got)
	}
	if got := osRemediationFamily("ALPINE"); got != "apk" {
		t.Errorf("osRemediationFamily(ALPINE) = %q, want apk", got)
	}
	if got := osRemediationFamilyFromSource(debian); got != "" {
		t.Errorf("osRemediationFamilyFromSource(%q) = %q, want empty", debian, got)
	}
	if osRemediationSourceSupported("rpm", rocky) {
		t.Errorf("osRemediationSourceSupported(rpm, %q) = true, want false", rocky)
	}
	if supportedOSPackageImpactManager(apk) {
		t.Errorf("supportedOSPackageImpactManager(%q) = true, want false", apk)
	}
	if got := osPackageFamilyFromPackageManager(apk); got != "" {
		t.Errorf("osPackageFamilyFromPackageManager(%q) = %q, want empty", apk, got)
	}
	for _, tc := range []struct{ ecosystem, packageManager, vendorSource, distro string }{
		{ecosystem: alpine, vendorSource: "alpine"},
		{ecosystem: "alpine", vendorSource: alpine},
		{ecosystem: "apk", packageManager: apk},
		{ecosystem: "debian", distro: debian},
	} {
		if osPackageEcosystemMatchesVendor(tc.ecosystem, tc.packageManager, tc.vendorSource, tc.distro) {
			t.Errorf("osPackageEcosystemMatchesVendor(%+v) = true, want false", tc)
		}
	}
	if !osPackageEcosystemMatchesVendor("ALPINE", "", "alpine", "") {
		t.Error("osPackageEcosystemMatchesVendor rejected an ASCII case variant")
	}
	if got := normalizeNPMName("\u212Aafka"); got == "kafka" {
		t.Errorf("normalizeNPMName folded a Kelvin-sign lookalike to %q", got)
	}
	if purlType, _ := osPackagePURLTypeAndNamespace("pkg:AP\u212A/alpine/busybox@1.0"); purlType == "apk" {
		t.Errorf("osPackagePURLTypeAndNamespace folded a lookalike PURL type to apk")
	}
	if got := osPackageIdentityFromAffectedPackage(supplychainmodel.AffectedPackage{
		PURL: "pkg:apk/alpine/busybox@1.0", Name: "\u212Aernel",
	}); got == "os://alpine/kernel" {
		t.Errorf("osPackageIdentityFromAffectedPackage folded a lookalike name to %q", got)
	}
	if got := osPackageIdentityFromAffectedPackage(supplychainmodel.AffectedPackage{
		PackageID: "OS://alpine/\u212Aernel",
	}); got == "os://alpine/kernel" {
		t.Errorf("osPackageIdentityFromAffectedPackage folded a lookalike package ID to %q", got)
	}
	if _, ok := remediationEcosystemOperations(alpine); ok {
		t.Errorf("remediationEcosystemOperations(%q) found operations, want none", alpine)
	}
	if got := advisorySourcePriority(debian, "debian"); got != advisorySourcePriority("unknown-ecosystem", "debian") {
		t.Errorf("advisorySourcePriority(%q, debian) = %d, want the unknown-ecosystem rank", debian, got)
	}
	if got := advisorySourcePriority("debian", debian); got != advisorySourcePriority("debian", "unknown-source") {
		t.Errorf("advisorySourcePriority(debian, %q) = %d, want the unknown-source rank", debian, got)
	}
	alert := supplyChainAffectedPackageFromSecurityAlert(securityalert.ProviderSecurityAlert{
		SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{Ecosystem: "pyp\u0130"},
	})
	if alert.Ecosystem == "pypi" {
		t.Errorf("supplyChainAffectedPackageFromSecurityAlert folded a lookalike ecosystem to pypi")
	}
}
