// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesExactAPKKnownFixedProfile(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"alpine-cve-apk-known-fixed",
			"CVE-2026-3046",
			"alpine",
			"ALPINE-2026-3046",
			8.1,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"alpine-affected-apk-known-fixed",
			"CVE-2026-3046",
			"alpine",
			"ALPINE-2026-3046",
			"pkg:apk/alpine/openssl",
			"apk",
			"openssl",
			"3.1.4-r5",
			"3.1.4-r6",
		),
		osPackageFact(
			"apk-os-openssl-known-fixed",
			"image://registry.example/alpine-app@sha256:3046",
			map[string]any{
				"distro":                 "alpine",
				"distro_version":         "3.19.1",
				"package_manager":        "apk",
				"name":                   "openssl",
				"arch":                   "x86_64",
				"repository_class":       "vendor",
				"vendor_advisory_source": "alpine",
				"installed_version_raw":  "3.1.4-r6",
				"purl":                   "pkg:apk/alpine/openssl@3.1.4-r6?arch=x86_64&distro=alpine-3.19.1",
			},
		),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 APK-backed finding: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.MatchReason != supplyChainVersionReasonAPKExactKnownFixed {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonAPKExactKnownFixed)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise", got.DetectionProfile)
	}
}
