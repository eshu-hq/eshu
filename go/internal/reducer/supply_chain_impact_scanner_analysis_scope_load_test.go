// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// scanScopedSupplyChainImpactFactLoader is a scope/generation-aware
// FactLoader fake that TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope
// uses to prove the real Handle/loadSupplyChainImpactEvidence path (not the
// BuildSupplyChainImpactFindings direct-builder path other tests in this
// package use) actually loads a scanner_worker.analysis fact from an
// os_package's OWN scan scope, not the intent's scope.
//
// The existing stubSupplyChainImpactFactLoader in supply_chain_impact_test.go
// is intentionally scope-blind (ListFactsByKind ignores the scopeID/
// generationID it is called with and always returns the same fixed slice),
// which is exactly what let issue #5463's missing load stage go undetected:
// every existing Handle-level test only ever has one scope in play. This
// fake instead partitions facts by (scopeID, generationID) so a call for the
// wrong scope observably returns nothing.
type scanScopedSupplyChainImpactFactLoader struct {
	factsByScope map[string][]facts.Envelope
	activeFacts  []facts.Envelope
	activeCalled bool
	kindCalls    map[string][][]string
}

func scanScopedFactLoaderKey(scopeID, generationID string) string {
	return scopeID + "|" + generationID
}

func (l *scanScopedSupplyChainImpactFactLoader) ListFacts(
	_ context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.factsByScope[scanScopedFactLoaderKey(scopeID, generationID)]...), nil
}

func (l *scanScopedSupplyChainImpactFactLoader) ListFactsByKind(
	_ context.Context,
	scopeID string,
	generationID string,
	kinds []string,
) ([]facts.Envelope, error) {
	key := scanScopedFactLoaderKey(scopeID, generationID)
	if l.kindCalls == nil {
		l.kindCalls = map[string][][]string{}
	}
	l.kindCalls[key] = append(l.kindCalls[key], append([]string(nil), kinds...))
	var matched []facts.Envelope
	for _, envelope := range l.factsByScope[key] {
		if slices.Contains(kinds, envelope.FactKind) {
			matched = append(matched, envelope)
		}
	}
	return matched, nil
}

// ListActiveSupplyChainImpactFacts returns the fixture's cross-scope active
// evidence (the os_package fact, in production terms) exactly once, mirroring
// the real SQL active-evidence stage settling after the first round because
// supplyChainImpactFilter does not add any new filter values for an
// os_package fact (it has no case for VulnerabilityOSPackageFactKind).
func (l *scanScopedSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	_ SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	if l.activeCalled {
		return nil, nil
	}
	l.activeCalled = true
	return append([]facts.Envelope(nil), l.activeFacts...), nil
}

// TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope is
// the regression guard for the scanner-analysis-scope LOAD STAGE: it proves
// loadSupplyChainImpactScannerAnalysisScopeFacts fetches the sibling
// scanner_worker.analysis from an os_package's OWN scan scope (which differs
// from the intent's vulnerability-intelligence scope) rather than the intent
// scope. It injects the os_package fact through the fake loader's
// ListActiveSupplyChainImpactFacts so that, GIVEN an os_package in the
// evidence set, the load stage discovers its scan scope and loads the sibling;
// before the stage existed, index.scannerAnalyses stayed empty in this shape,
// the ScopeID+GenerationID join never fired, and SubjectDigest stayed blank.
// This is a STAGE-level guard, not a claim that the production Handle path
// loads os_package today: loadSupplyChainImpactEvidence does not currently load
// vulnerability.os_package at all, so os_package supply-chain-impact findings
// (and therefore this digest join) are inert end-to-end until os_package Handle
// loading is wired (tracked in #5705, part of #5464). The direct-builder tests
// in supply_chain_impact_scanner_analysis_test.go prove the classify/join logic
// on a hand-built envelope set; this test additionally proves the load stage's
// scope handling. Neither exercises real os_package delivery into the Handle
// pipeline, which #5705/#5464 add.
func TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl"
		intentGenerationID = "generation-intel-5463"
		scanScopeID        = "scan-target-debian-app-os-package-crossscope"
		scanGenerationID   = "generation-scan-5463"
		imageRef           = "registry.example/debian-app@" + testScannerAnalysisImageDigest
	)

	osPackage := facts.Envelope{
		FactID:       "dpkg-os-openssl-crossscope",
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
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
		},
	}
	scannerAnalysis := scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef)

	loader := &scanScopedSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-crossscope",
					"CVE-2026-5463",
					"debian",
					"DSA-2026-5463",
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-crossscope",
					"CVE-2026-5463",
					"debian",
					"DSA-2026-5463",
					"pkg:deb/debian/openssl",
					"deb",
					"openssl",
					"3.0.11-1~deb12u2",
					"3.0.11-1~deb12u3",
				),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {scannerAnalysis},
		},
		activeFacts: []facts.Envelope{osPackage},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-crossscope-digest",
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
		t.Fatalf("len(Findings) = %d, want 1: %#v", got, writer.write.Findings)
	}
	got := writer.write.Findings[0]
	if got.SubjectDigest != testScannerAnalysisImageDigest {
		t.Fatalf(
			"SubjectDigest = %q, want the scan-scope scanner_worker.analysis digest %q; "+
				"the sibling analysis fact must be loaded from the os_package's own scope (%s), not the intent's scope (%s)",
			got.SubjectDigest, testScannerAnalysisImageDigest, scanScopeID, intentScopeID,
		)
	}
	if got.ImageRef != imageRef {
		t.Fatalf("ImageRef = %q, want %q", got.ImageRef, imageRef)
	}

	scanScopeCalls := loader.kindCalls[scanScopedFactLoaderKey(scanScopeID, scanGenerationID)]
	if len(scanScopeCalls) == 0 {
		t.Fatalf("loader received no ListFactsByKind call for the os_package's own scan scope %q/%q", scanScopeID, scanGenerationID)
	}
	foundScannerAnalysisKindCall := false
	for _, kinds := range scanScopeCalls {
		if slices.Contains(kinds, facts.ScannerWorkerAnalysisFactKind) {
			foundScannerAnalysisKindCall = true
			break
		}
	}
	if !foundScannerAnalysisKindCall {
		t.Fatalf("loader kind calls for scan scope = %#v, want a call requesting %q", scanScopeCalls, facts.ScannerWorkerAnalysisFactKind)
	}

	if result.SubSignals["scanner_analysis_scope_facts"] != 1 {
		t.Fatalf("SubSignals[scanner_analysis_scope_facts] = %v, want 1", result.SubSignals["scanner_analysis_scope_facts"])
	}
}
