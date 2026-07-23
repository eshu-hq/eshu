// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactHandlerLoadsOSPackageFromAdvisoryTargetReader is the
// issue #5463/#5705 end-to-end acceptance fixture: it proves the real
// Handle/loadSupplyChainImpactEvidence path produces an os_package
// supply-chain-impact finding whose SubjectDigest is the real scanner-observed
// image digest, when the ONLY source of the vulnerability.os_package fact is
// the cross-scope advisory-target reader (ListOSPackageAdvisoryTargets) — not
// ListActiveSupplyChainImpactFacts, which is how every other os_package
// fixture in this package (e.g.
// TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope)
// injects it. Before the load stage this test drives existed,
// loadSupplyChainImpactEvidence never called ListOSPackageAdvisoryTargets at
// all, so os_package supply-chain-impact findings were inert end-to-end (the
// AGENTS.md "end-to-end inertness today" note this PR resolves) and this test
// failed with zero findings.
func TestSupplyChainImpactHandlerLoadsOSPackageFromAdvisoryTargetReader(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl-advisory-reader"
		intentGenerationID = "generation-intel-5705"
		scanScopeID        = "scan-target-debian-app-os-package-advisory-reader"
		scanGenerationID   = "generation-scan-5705"
		imageRef           = "registry.example/debian-app@" + testScannerAnalysisImageDigest
		purl               = "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12"
	)

	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)

	loader := &scanScopedSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-advisory-reader",
					"CVE-2026-5705",
					"debian",
					"DSA-2026-5705",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-advisory-reader",
					"CVE-2026-5705",
					"debian",
					"DSA-2026-5705",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
			},
			// No os_package entry here: it must arrive ONLY through
			// ListOSPackageAdvisoryFactEnvelopes below.
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
		// This is exactly the envelope shape
		// postgres.osPackageAdvisoryFactEnvelopeFromTarget reconstructs from
		// one OSPackageAdvisoryTarget row (installed_advisory_targets_os_package_envelope.go).
		osPackageAdvisoryFactEnvelopes: []facts.Envelope{
			{
				FactID:       "dpkg-os-openssl-advisory-reader",
				FactKind:     facts.VulnerabilityOSPackageFactKind,
				ScopeID:      scanScopeID,
				GenerationID: scanGenerationID,
				Payload: map[string]any{
					"distro":                 "debian",
					"distro_version":         "12",
					"package_manager":        "dpkg",
					"name":                   "openssl",
					"arch":                   "amd64",
					"repository_class":       "vendor",
					"vendor_advisory_source": "debian",
					"installed_version_raw":  "3.0.11-1~deb12u2",
					"purl":                   purl,
				},
			},
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-advisory-reader-digest",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}

	if got := len(writer.write.Findings); got != 1 {
		t.Fatalf("len(Findings) = %d, want 1 (os_package finding sourced from the advisory-target reader): %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.SubjectDigest != testScannerAnalysisImageDigest {
		t.Fatalf(
			"SubjectDigest = %q, want the scan-scope scanner_worker.analysis digest %q; the reconstructed "+
				"os_package envelope must carry the advisory target's ScopeID/GenerationID so the sibling "+
				"scanner-analysis-scope load stage and digest join fire",
			got.SubjectDigest, testScannerAnalysisImageDigest,
		)
	}
	if got.ImageRef != imageRef {
		t.Fatalf("ImageRef = %q, want %q", got.ImageRef, imageRef)
	}

	if len(loader.osPackageAdvisoryEcosystemCalls) == 0 {
		t.Fatal("loader received no ListOSPackageAdvisoryFactEnvelopes call; ecosystems must be derived from the loaded affected_package facts")
	}
	ecosystems := loader.osPackageAdvisoryEcosystemCalls[0]
	if len(ecosystems) != 1 || ecosystems[0] != "debian" {
		t.Fatalf("ListOSPackageAdvisoryFactEnvelopes ecosystems = %#v, want [\"debian\"] derived from the debian affected_package fact", ecosystems)
	}

	if result.SubSignals["os_package_advisory_facts"] != 1 {
		t.Fatalf("SubSignals[os_package_advisory_facts] = %v, want 1", result.SubSignals["os_package_advisory_facts"])
	}
	if result.SubSignals["scanner_analysis_scope_facts"] != 1 {
		t.Fatalf("SubSignals[scanner_analysis_scope_facts] = %v, want 1", result.SubSignals["scanner_analysis_scope_facts"])
	}
}

// TestSupplyChainImpactOSPackageAdvisoryEcosystemsDerivesVendorSourceNotRawEcosystem
// proves the ecosystem values passed to ListOSPackageAdvisoryTargets are the
// classified vendor-advisory-source strings (e.g. "debian") the SQL loader's
// ecosystem column actually matches (LOWER(vendor_advisory_source OR distro)),
// not the affected_package's raw purl-type ecosystem field (e.g. "deb"), which
// would never match any installed-evidence row.
func TestSupplyChainImpactOSPackageAdvisoryEcosystemsDerivesVendorSourceNotRawEcosystem(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		vulnerabilityAffectedPackageFactWithSource(
			"debian-affected-ecosystem-derive",
			"CVE-2026-9001",
			"debian",
			"DSA-2026-9001",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"alpine-affected-ecosystem-derive",
			"CVE-2026-9002",
			"alpine",
			"",
			"pkg:apk/alpine/curl",
			"apk",
			"curl",
			"8.5.0-r0",
			"8.5.0-r1",
		),
		// Not an affected_package fact: must be ignored by the derivation.
		vulnerabilityCVEFactWithProvenance(
			"cve-ecosystem-derive",
			"CVE-2026-9003",
			"nvd",
			"",
			5.0,
			"",
			"MEDIUM",
			"2026-06-05T12:00:00Z",
		),
	}

	got := supplyChainImpactOSPackageAdvisoryEcosystems(envelopes)
	want := []string{"alpine", "debian"}
	if len(got) != len(want) {
		t.Fatalf("supplyChainImpactOSPackageAdvisoryEcosystems() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("supplyChainImpactOSPackageAdvisoryEcosystems() = %#v, want %#v", got, want)
		}
	}
}
