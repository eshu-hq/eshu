// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestGoScanProvenanceSurvivesTypedDecodeToFinding proves R1 of
// eshu-hq/eshu#10028: scan provenance on a typed
// vulnerability.go_call_reachability fact (scan_level, scan_mode,
// scanner_version, trace_count) survives the typed decode into the
// reducer-owned GoVulnerabilityFinding.
func TestGoScanProvenanceSurvivesTypedDecodeToFinding(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		goModuleEvidenceEnvelope(t, "repo-1", "go.mod", "example.com/vuln", "v1.2.3", false, "", ""),
		goAffectedPackageEnvelope(t, "GO-2024-9999", "example.com/vuln"),
		goReachabilityEnvelopeWithProvenance(t, "repo-1", "GO-2024-9999", "example.com/vuln", "v1.2.3", "example.com/vuln/inner", "DangerousCall", "symbol", "symbol", "source", "v1.9.0", 3),
	}
	findings := ClassifyGoVulnerabilityReachability(envelopes)
	if got, want := len(findings), 1; got != want {
		t.Fatalf("findings = %d, want %d", got, want)
	}
	finding := findings[0]
	if finding.ScanLevel != "symbol" {
		t.Fatalf("ScanLevel = %q, want symbol", finding.ScanLevel)
	}
	if finding.ScanMode != "source" {
		t.Fatalf("ScanMode = %q, want source", finding.ScanMode)
	}
	if finding.ScannerVersion != "v1.9.0" {
		t.Fatalf("ScannerVersion = %q, want v1.9.0", finding.ScannerVersion)
	}
	if finding.TraceCount == nil || *finding.TraceCount != 3 {
		t.Fatalf("TraceCount = %v, want 3", finding.TraceCount)
	}
}

// TestGoScanProvenanceReachesExplainPayload proves the second half of R1:
// the provenance on the classified Go finding reaches the explain payload
// (supplyChainReachabilityPayload) on the impact finding.
func TestGoScanProvenanceReachesExplainPayload(t *testing.T) {
	t.Parallel()

	const (
		repoID     = "repo://example/prov"
		modulePath = "example.com/vuln"
		osvID      = "GO-2024-9999"
		version    = "v1.2.3"
	)
	packageID := "gomod://proxy.golang.org/" + modulePath
	envelopes := []facts.Envelope{
		packageConsumptionFactWithRange("consume-prov", packageID, repoID, version),
		goModuleEvidenceEnvelope(t, repoID, "go.mod", modulePath, version, false, "", ""),
		vulnerabilityCVEFact("cve-prov", osvID, 7.5),
		vulnerabilityAffectedPackageGoRangeFact("affected-prov", osvID, packageID, modulePath, "v9.9.9"),
		goReachabilityEnvelopeWithProvenance(t, repoID, osvID, modulePath, version, modulePath+"/inner", "DangerousCall", "symbol", "symbol", "source", "v1.9.0", 3),
	}
	findings := BuildSupplyChainImpactFindings(envelopes)
	byCVE := supplyChainImpactFindingsByCVE(findings)
	got, ok := byCVE[osvID]
	if !ok {
		t.Fatalf("no finding for %s: %#v", osvID, findings)
	}
	payload := supplyChainReachabilityPayload(got.Reachability)
	if payload == nil {
		t.Fatalf("reachability payload is nil")
	}
	if payload["scan_level"] != "symbol" {
		t.Fatalf("scan_level = %v, want symbol", payload["scan_level"])
	}
	if payload["scan_mode"] != "source" {
		t.Fatalf("scan_mode = %v, want source", payload["scan_mode"])
	}
	if payload["scanner_version"] != "v1.9.0" {
		t.Fatalf("scanner_version = %v, want v1.9.0", payload["scanner_version"])
	}
	if payload["trace_count"] != 3 {
		t.Fatalf("trace_count = %v (%T), want 3", payload["trace_count"], payload["trace_count"])
	}
}

// TestGoScanProvenanceAbsentStaysAbsent pins the wire-compatibility edge:
// a go_call_reachability fact written before the provenance fields existed
// emits an explain payload with no scan keys, not zero-valued ones.
func TestGoScanProvenanceAbsentStaysAbsent(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		goModuleEvidenceEnvelope(t, "repo-1", "go.mod", "example.com/vuln", "v1.2.3", false, "", ""),
		goAffectedPackageEnvelope(t, "GO-2024-9999", "example.com/vuln"),
		goReachabilityEnvelope(t, "repo-1", "GO-2024-9999", "example.com/vuln", "v1.2.3", "example.com/vuln/inner", "DangerousCall", "symbol"),
	}
	findings := ClassifyGoVulnerabilityReachability(envelopes)
	if got, want := len(findings), 1; got != want {
		t.Fatalf("findings = %d, want %d", got, want)
	}
	finding := findings[0]
	if finding.ScanLevel != "" || finding.ScanMode != "" || finding.ScannerVersion != "" {
		t.Fatalf("provenance should be blank, got level=%q mode=%q version=%q",
			finding.ScanLevel, finding.ScanMode, finding.ScannerVersion)
	}
	if finding.TraceCount != nil {
		t.Fatalf("TraceCount = %v, want nil", *finding.TraceCount)
	}
}

// goReachabilityEnvelopeWithProvenance builds a
// vulnerability.go_call_reachability fixture carrying scan provenance,
// mirroring what the govulncheck emitter writes (scan_level, scan_mode,
// scanner_version on every fact; trace_count counting the folded traces).
func goReachabilityEnvelopeWithProvenance(
	t *testing.T,
	repoID, osvID, modulePath, moduleVersion, packagePath, symbol, level string,
	scanLevel, scanMode, scannerVersion string,
	traceCount int,
) facts.Envelope {
	t.Helper()
	trace := []map[string]string{}
	frame := map[string]string{
		"module":  modulePath,
		"version": moduleVersion,
	}
	if packagePath != "" {
		frame["package"] = packagePath
	}
	if symbol != "" {
		frame["function"] = symbol
	}
	trace = append(trace, frame)
	return facts.Envelope{
		FactID:        facts.StableID(chain.VulnerabilityGoCallReachabilityFactKind, map[string]any{"osv": osvID, "module": modulePath, "package": packagePath, "prov": "1"}),
		FactKind:      chain.VulnerabilityGoCallReachabilityFactKind,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"repository_id":      repoID,
			"osv_id":             osvID,
			"deepest_module":     modulePath,
			"deepest_package":    packagePath,
			"deepest_symbol":     symbol,
			"reachability_level": level,
			"scan_level":         scanLevel,
			"scan_mode":          scanMode,
			"scanner_version":    scannerVersion,
			"trace_count":        traceCount,
			"trace":              trace,
		},
	}
}
