// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Tests in this file feed the reducer facts derived from RECORDED govulncheck
// output (testdata/govulncheck, see its README) instead of hand-written
// payloads, so a wrong reading of the real wire shape fails here. Issue
// eshu-hq/eshu#10028, epic #10002.
//
// Recorded module: golang.org/x/text v0.3.5 with main calling language.Parse.
//   GO-2021-0113  module + package + symbol finding  -> called
//   GO-2022-1059  module + package findings only     -> imported, not called
//   GO-2026-5970  module finding only                -> required, not imported
//   GO-2026-6629  module finding only                -> required, not imported

const (
	realShapeRepositoryID = "repo://example/gvcexp"
	realShapeModule       = "golang.org/x/text"
	realShapeVersion      = "v0.3.5"
)

// realShapeImpactEnvelopes builds the full fact set the supply-chain impact
// reducer needs for the recorded x/text advisories: advisory, affected range,
// manifest consumption, go.mod evidence, plus the supplied reachability facts.
func realShapeImpactEnvelopes(t *testing.T, stream govulncheckStreamFixture, reach []facts.Envelope) []facts.Envelope {
	t.Helper()
	packageID := "gomod://proxy.golang.org/" + realShapeModule
	out := []facts.Envelope{
		packageConsumptionFactWithRange("consume-xtext", packageID, realShapeRepositoryID, realShapeVersion),
		goModuleEvidenceEnvelope(t, realShapeRepositoryID, "go.mod", realShapeModule, realShapeVersion, false, "", ""),
	}
	seen := map[string]bool{}
	for _, finding := range stream.findings {
		if finding.Trace[0].Module != realShapeModule || seen[finding.OSV] {
			continue
		}
		seen[finding.OSV] = true
		fixed := strings.TrimPrefix(finding.FixedVersion, "v")
		out = append(out,
			vulnerabilityCVEFact("cve-"+finding.OSV, finding.OSV, 7.5),
			vulnerabilityAffectedPackageGoRangeFact("affected-"+finding.OSV, finding.OSV, packageID, realShapeModule, fixed),
		)
	}
	return append(out, reach...)
}

func realShapeFindings(t *testing.T, stream govulncheckStreamFixture, reach []facts.Envelope) map[string]SupplyChainImpactFinding {
	t.Helper()
	findings := BuildSupplyChainImpactFindings(realShapeImpactEnvelopes(t, stream, reach))
	return supplyChainImpactFindingsByCVE(findings)
}

func recordedSymbolScan(t *testing.T) (govulncheckStreamFixture, []facts.Envelope) {
	t.Helper()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	if stream.config.ScanLevel != "symbol" || stream.config.ScanMode != "source" {
		t.Fatalf("fixture config = %+v, want symbol/source", stream.config)
	}
	var xtext []govulncheckFinding
	for _, finding := range stream.findings {
		if finding.Trace[0].Module == realShapeModule {
			xtext = append(xtext, finding)
		}
	}
	stream.findings = xtext
	return stream, deriveGoCallReachabilityFacts(t, stream, realShapeRepositoryID)
}

func wantReachability(t *testing.T, got SupplyChainImpactFinding, state SupplyChainReachabilityState, runtime string) {
	t.Helper()
	if got.Reachability == nil {
		t.Fatalf("%s: Reachability is nil", got.CVEID)
	}
	if got.Reachability.State != state || got.RuntimeReachability != runtime {
		t.Fatalf("%s: state=%q runtime=%q, want state=%q runtime=%q (reason %q)",
			got.CVEID, got.Reachability.State, got.RuntimeReachability, state, runtime, got.Reachability.Reason)
	}
}

// TestRecordedSymbolFindingKeepsRealTraceAndReadsReachable pins the one
// finding of the recorded run that has a call path: GO-2021-0113 reaches
// language.Parse from main. Real govulncheck lists the vulnerable symbol
// first; the fact the emitter writes (and the reducer preserves) lists the
// entry point first and the vulnerable symbol last.
func TestRecordedSymbolFindingKeepsRealTraceAndReadsReachable(t *testing.T) {
	t.Parallel()
	stream, reach := recordedSymbolScan(t)

	got := realShapeFindings(t, stream, reach)["GO-2021-0113"]
	wantReachability(t, got, SupplyChainReachabilityReachable, string(GoVulnReachabilitySymbolReachable))
	if got.Reachability.Confidence != "strong" {
		t.Fatalf("Confidence = %q, want strong for a symbol-level call path", got.Reachability.Confidence)
	}

	classified := ClassifyGoVulnerabilityReachability(realShapeImpactEnvelopes(t, stream, reach))
	var finding GoVulnerabilityFinding
	for _, candidate := range classified {
		if candidate.OSVID == "GO-2021-0113" {
			finding = candidate
		}
	}
	if finding.Symbol != "Parse" || finding.PackagePath != "golang.org/x/text/language" {
		t.Fatalf("deepest = %q in %q, want Parse in golang.org/x/text/language", finding.Symbol, finding.PackagePath)
	}
	if len(finding.CallTrace) != 2 {
		t.Fatalf("CallTrace = %#v, want 2 frames", finding.CallTrace)
	}
	entry, vulnerable := finding.CallTrace[0], finding.CallTrace[1]
	if entry.Function != "main" || entry.Module != "example.com/gvcexp" || entry.Position != "main.go:10:28" {
		t.Fatalf("first frame = %#v, want the entry point main at main.go:10:28", entry)
	}
	if vulnerable.Function != "Parse" || vulnerable.Package != "golang.org/x/text/language" ||
		vulnerable.Version != realShapeVersion || vulnerable.Position != "language/parse.go:33:6" {
		t.Fatalf("last frame = %#v, want the vulnerable symbol Parse at language/parse.go:33:6", vulnerable)
	}
}

// TestRecordedPackageLevelFindingReadsNotCalled covers GO-2022-1059: the
// symbol-level scan found the vulnerable package imported (package finding)
// but never called ParseAcceptLanguage. It must never read as reachable.
func TestRecordedPackageLevelFindingReadsNotCalled(t *testing.T) {
	t.Parallel()
	stream, reach := recordedSymbolScan(t)

	got := realShapeFindings(t, stream, reach)["GO-2022-1059"]
	wantReachability(t, got, SupplyChainReachabilityNotCalled, string(GoVulnReachabilityNotCalled))
	if got.Reachability.State == SupplyChainReachabilityReachable {
		t.Fatalf("import-only finding of a symbol-level scan read as reachable: %#v", got.Reachability)
	}
}

// TestRecordedModuleLevelFindingReadsNotCalled covers GO-2026-5970 and
// GO-2026-6629: the module is required but the code imports none of the
// vulnerable packages. A symbol-level scan that found nothing to call is
// not_called, not "module only" and not reachable.
func TestRecordedModuleLevelFindingReadsNotCalled(t *testing.T) {
	t.Parallel()
	stream, reach := recordedSymbolScan(t)

	findings := realShapeFindings(t, stream, reach)
	for _, osv := range []string{"GO-2026-5970", "GO-2026-6629"} {
		wantReachability(t, findings[osv], SupplyChainReachabilityNotCalled, string(GoVulnReachabilityNotCalled))
	}
}

// TestEmitterRulesNeverEmitImportForSymbolLevelScan guards the derivation
// contract itself: no fact of a symbol-level scan may carry level import or
// module, because the reducer reads import as reachable (see
// TestReducerReadsImportLevelAsReachablePackageScanOnly).
func TestEmitterRulesNeverEmitImportForSymbolLevelScan(t *testing.T) {
	t.Parallel()
	_, reach := recordedSymbolScan(t)

	levels := map[string]string{}
	for _, envelope := range reach {
		levels[envelope.Payload["osv_id"].(string)] = envelope.Payload["reachability_level"].(string)
	}
	want := map[string]string{
		"GO-2021-0113": "symbol",
		"GO-2022-1059": "not_called",
		"GO-2026-5970": "not_called",
		"GO-2026-6629": "not_called",
	}
	for osv, level := range want {
		if levels[osv] != level {
			t.Fatalf("level[%s] = %q, want %q (all: %v)", osv, levels[osv], level, levels)
		}
	}
}

// TestReducerReadsImportLevelAsReachablePackageScanOnly pins the reducer's
// handling of level import: a PACKAGE-level scan (govulncheck -scan package)
// cannot see calls, so a vulnerable imported package is reported reachable
// with the missing symbol evidence named. That reading is only valid for a
// package-level scan; the symbol-level emitter never produces import.
func TestReducerReadsImportLevelAsReachablePackageScanOnly(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_package_source.jsonl")
	if stream.config.ScanLevel != "package" {
		t.Fatalf("fixture scan_level = %q, want package", stream.config.ScanLevel)
	}
	reach := deriveGoCallReachabilityFacts(t, stream, realShapeRepositoryID)

	got := realShapeFindings(t, stream, reach)["GO-2022-1059"]
	wantReachability(t, got, SupplyChainReachabilityReachable, string(GoVulnReachabilityPackageImportReachable))
	if !stringSliceContains(got.Reachability.MissingEvidence, "symbol-level call trace missing") {
		t.Fatalf("MissingEvidence = %#v, want the missing symbol trace named", got.Reachability.MissingEvidence)
	}
}
