// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const analysisFailedTestRepo = "repo://example/analysis-failed"

const analysisFailedTestModule = "example.com/vulnmod"

// analysisFailedWarningEnvelope builds a repository-scoped unfinished-scan
// warning for the go_reachability analyzer, the same shape
// deriveGoRunFacts emits for a failed govulncheck run.
func analysisFailedWarningEnvelope(t *testing.T, osvID string) facts.Envelope {
	t.Helper()
	return facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": analysisFailedTestRepo, "osv": osvID}),
		FactKind:      "scanner_worker.warning",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"analyzer":            "go_reachability",
			"target_kind":         "repository",
			"target_locator_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"reason":              "toolchain_unsupported",
			"warning_class":       "scanner_worker_warning",
			"analysis_status":     "not_scanned",
			"coverage_status":     "unsupported",
			"repository_id":       analysisFailedTestRepo,
		},
	}
}

// analysisFailedRowEnvelopes assembles the full impact envelope set for one
// contract row: consumption, module evidence, CVE, affected package, an
// optional govulncheck reachability fact, and an optional unfinished-scan
// warning. A nil reachLevel means the scan produced no reachability fact.
func analysisFailedRowEnvelopes(t *testing.T, osvID string, reachLevel *string, withWarning bool) []facts.Envelope {
	t.Helper()
	packageID := "gomod://proxy.golang.org/" + analysisFailedTestModule
	envelopes := []facts.Envelope{
		packageConsumptionFactWithRange("consume-"+osvID, packageID, analysisFailedTestRepo, "v1.2.3"),
		goModuleEvidenceEnvelope(t, analysisFailedTestRepo, "go.mod", analysisFailedTestModule, "v1.2.3", false, "", ""),
		vulnerabilityCVEFact("cve-"+osvID, osvID, 7.5),
		vulnerabilityAffectedPackageGoRangeFact("affected-"+osvID, osvID, packageID, analysisFailedTestModule, "1.2.4"),
	}
	if reachLevel != nil {
		packagePath, symbol := "", ""
		if *reachLevel == "symbol" {
			packagePath, symbol = analysisFailedTestModule+"/vulnpkg", "VulnFunc"
		}
		if *reachLevel == "import" {
			packagePath = analysisFailedTestModule + "/vulnpkg"
		}
		envelopes = append(envelopes, goReachabilityEnvelope(t, analysisFailedTestRepo, osvID, analysisFailedTestModule, "v1.2.3", packagePath, symbol, *reachLevel))
	}
	if withWarning {
		envelopes = append(envelopes, analysisFailedWarningEnvelope(t, osvID))
	}
	return envelopes
}

func strptr(value string) *string {
	return &value
}

// TestGoAnalysisFailedContract pins the #10043 verdict rule: a joined
// unfinished-scan warning rewrites an untrustworthy not_called, module_only,
// or unknown verdict to analysis_failed, while a finished scan's reachable
// verdict wins untouched and a finding without a joined warning keeps its
// fact state.
func TestGoAnalysisFailedContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// reachLevel is the govulncheck reachability level behind the
		// finding; nil means the scan produced no reachability fact.
		reachLevel *string
		warning    bool
		// wantGo is the classifier verdict and wantGoMissing its exact
		// MissingEvidence set.
		wantGo        GoVulnerabilityReachability
		wantGoMissing []string
		// wantState and wantRuntime are the impact envelope values.
		wantState   SupplyChainReachabilityState
		wantRuntime string
	}{
		{
			name:          "symbol fact with warning stays reachable and takes no warning entry",
			reachLevel:    strptr("symbol"),
			warning:       true,
			wantGo:        GoVulnReachabilitySymbolReachable,
			wantGoMissing: nil,
			wantState:     SupplyChainReachabilityReachable,
			wantRuntime:   string(GoVulnReachabilitySymbolReachable),
		},
		{
			name:          "import fact with warning stays reachable and takes no warning entry",
			reachLevel:    strptr("import"),
			warning:       true,
			wantGo:        GoVulnReachabilityPackageImportReachable,
			wantGoMissing: []string{"symbol-level call trace missing"},
			wantState:     SupplyChainReachabilityReachable,
			wantRuntime:   string(GoVulnReachabilityPackageImportReachable),
		},
		{
			name:          "not called fact with warning reads analysis failed",
			reachLevel:    strptr("not_called"),
			warning:       true,
			wantGo:        GoVulnReachabilityAnalysisFailed,
			wantGoMissing: []string{"govulncheck call-graph evidence missing", "govulncheck scan toolchain_unsupported"},
			wantState:     SupplyChainReachabilityMissingEvidence,
			wantRuntime:   string(GoVulnReachabilityAnalysisFailed),
		},
		{
			name:          "module fact with warning reads analysis failed",
			reachLevel:    strptr("module"),
			warning:       true,
			wantGo:        GoVulnReachabilityAnalysisFailed,
			wantGoMissing: []string{"govulncheck call-graph evidence missing", "govulncheck scan toolchain_unsupported", "import-graph evidence missing"},
			wantState:     SupplyChainReachabilityMissingEvidence,
			wantRuntime:   string(GoVulnReachabilityAnalysisFailed),
		},
		{
			name:          "unknown fact with warning reads analysis failed",
			reachLevel:    strptr("unknown"),
			warning:       true,
			wantGo:        GoVulnReachabilityAnalysisFailed,
			wantGoMissing: []string{"govulncheck call-graph evidence missing", "govulncheck scan toolchain_unsupported"},
			wantState:     SupplyChainReachabilityMissingEvidence,
			wantRuntime:   string(GoVulnReachabilityAnalysisFailed),
		},
		{
			name:          "no fact with warning reads analysis failed",
			reachLevel:    nil,
			warning:       true,
			wantGo:        GoVulnReachabilityAnalysisFailed,
			wantGoMissing: []string{"govulncheck call-graph evidence missing", "govulncheck scan toolchain_unsupported"},
			wantState:     SupplyChainReachabilityMissingEvidence,
			wantRuntime:   string(GoVulnReachabilityAnalysisFailed),
		},
		{
			name:          "module fact without warning keeps its fact state",
			reachLevel:    strptr("module"),
			warning:       false,
			wantGo:        GoVulnReachabilityModuleOnly,
			wantGoMissing: []string{"import-graph evidence missing"},
			wantState:     SupplyChainReachabilityUnknown,
			wantRuntime:   string(GoVulnReachabilityModuleOnly),
		},
		{
			name:          "no fact without warning keeps module only",
			reachLevel:    nil,
			warning:       false,
			wantGo:        GoVulnReachabilityModuleOnly,
			wantGoMissing: []string{"govulncheck call-graph evidence missing"},
			wantState:     SupplyChainReachabilityUnknown,
			wantRuntime:   string(GoVulnReachabilityModuleOnly),
		},
		{
			name:          "symbol fact without warning stays reachable",
			reachLevel:    strptr("symbol"),
			warning:       false,
			wantGo:        GoVulnReachabilitySymbolReachable,
			wantGoMissing: nil,
			wantState:     SupplyChainReachabilityReachable,
			wantRuntime:   string(GoVulnReachabilitySymbolReachable),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			osvID := "GO-2099-10043"
			envelopes := analysisFailedRowEnvelopes(t, osvID, tc.reachLevel, tc.warning)

			classified := ClassifyGoVulnerabilityReachability(envelopes)
			if len(classified) != 1 {
				t.Fatalf("classified = %d findings, want 1 for %s", len(classified), osvID)
			}
			got := classified[0]
			if got.Reachability != tc.wantGo {
				t.Fatalf("Go verdict = %q, want %q (reason %q)", got.Reachability, tc.wantGo, got.Reason)
			}
			if len(got.MissingEvidence) != len(tc.wantGoMissing) {
				t.Fatalf("Go MissingEvidence = %#v, want %#v", got.MissingEvidence, tc.wantGoMissing)
			}
			for _, want := range tc.wantGoMissing {
				if !stringSliceContains(got.MissingEvidence, want) {
					t.Fatalf("Go MissingEvidence = %#v, want entry %q", got.MissingEvidence, want)
				}
			}

			findings := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))
			impact, ok := findings[osvID]
			if !ok {
				t.Fatalf("no impact finding built for %s", osvID)
			}
			wantReachability(t, impact, tc.wantState, tc.wantRuntime)
			for _, want := range tc.wantGoMissing {
				if !stringSliceContains(impact.Reachability.MissingEvidence, want) {
					t.Fatalf("impact MissingEvidence = %#v, want entry %q", impact.Reachability.MissingEvidence, want)
				}
			}
			if tc.warning && (tc.wantGo == GoVulnReachabilitySymbolReachable || tc.wantGo == GoVulnReachabilityPackageImportReachable) {
				for _, entry := range impact.Reachability.MissingEvidence {
					if entry == "govulncheck scan toolchain_unsupported" {
						t.Fatalf("reachable impact MissingEvidence = %#v, want no joined warning entry", impact.Reachability.MissingEvidence)
					}
				}
			}
		})
	}
}

// TestGoWarningCreatesNoFinding pins the warning invariant: a
// scanner_worker.warning names a failure class but anchors nothing, so on
// its own it produces no Go finding and no impact finding.
func TestGoWarningCreatesNoFinding(t *testing.T) {
	t.Parallel()
	envelopes := []facts.Envelope{analysisFailedWarningEnvelope(t, "GO-2099-0000")}
	if classified := ClassifyGoVulnerabilityReachability(envelopes); len(classified) != 0 {
		t.Fatalf("classified = %d findings, want 0 for a lone warning", len(classified))
	}
	if findings := BuildSupplyChainImpactFindings(envelopes); len(findings) != 0 {
		t.Fatalf("impact findings = %d, want 0 for a lone warning", len(findings))
	}
}

// TestGoWarningRetractsNoFinding pins the retraction half of the warning
// invariant: a tombstoned module row stays tombstoned, so a warning scoped
// to the retracted scope joins nothing — the impact finding keeps the exact
// verdict it has without the warning instead of flipping to analysis_failed.
func TestGoWarningRetractsNoFinding(t *testing.T) {
	t.Parallel()
	osvID := "GO-2099-10044"
	tombstoned := analysisFailedRowEnvelopes(t, osvID, nil, true)
	for i, envelope := range tombstoned {
		if envelope.FactKind == chain.VulnerabilityGoModuleEvidenceFactKind {
			envelope.IsTombstone = true
			tombstoned[i] = envelope
		}
	}
	if classified := ClassifyGoVulnerabilityReachability(tombstoned); len(classified) != 0 {
		t.Fatalf("classified = %d findings, want 0 for tombstoned module evidence with a warning", len(classified))
	}
	withWarning := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(tombstoned))[osvID]
	unwarned := tombstoned[:0:0]
	for _, envelope := range tombstoned {
		if envelope.FactKind == facts.ScannerWorkerWarningFactKind {
			continue
		}
		unwarned = append(unwarned, envelope)
	}
	withoutWarning := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(unwarned))[osvID]
	if withWarning.Reachability == nil || withoutWarning.Reachability == nil {
		t.Fatalf("reachability nil: with warning %#v, without %#v", withWarning.Reachability, withoutWarning.Reachability)
	}
	if withWarning.Reachability.State != withoutWarning.Reachability.State ||
		withWarning.RuntimeReachability != withoutWarning.RuntimeReachability {
		t.Fatalf("warning changed a retracted scope: state %q/%q runtime %q/%q, want identical",
			withWarning.Reachability.State, withoutWarning.Reachability.State,
			withWarning.RuntimeReachability, withoutWarning.RuntimeReachability)
	}
	for _, entry := range withWarning.Reachability.MissingEvidence {
		if entry == "govulncheck scan toolchain_unsupported" {
			t.Fatalf("retracted scope carries a joined warning entry: %#v", withWarning.Reachability.MissingEvidence)
		}
	}
}

// TestGoAnalysisFailedPriorityContribution pins the priority half of the
// #10043 verdict: an analysis_failed finding keeps the govulncheck
// missing-evidence contribution (-5), while a reachable finding keeps the
// reachable-code contribution instead of the missing-evidence one.
func TestGoAnalysisFailedPriorityContribution(t *testing.T) {
	t.Parallel()
	failed := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(
		analysisFailedRowEnvelopes(t, "GO-2099-10045", nil, true)))["GO-2099-10045"]
	if failed.Reachability == nil || failed.Reachability.State != SupplyChainReachabilityMissingEvidence {
		t.Fatalf("failed fixture state = %#v, want missing_evidence", failed.Reachability)
	}
	foundMissing := false
	for _, contribution := range failed.PriorityContributions {
		if contribution.ReasonCode == "reachability_missing_evidence" {
			foundMissing = true
			if contribution.Input != "reachability" || contribution.Value != string(SupplyChainReachabilityMissingEvidence) || contribution.Contribution != -5 {
				t.Fatalf("missing-evidence contribution = %#v, want reachability/missing_evidence/-5", contribution)
			}
		}
		if contribution.ReasonCode == "reachability_not_called" {
			t.Fatalf("analysis_failed carries a not_called contribution: %#v", contribution)
		}
	}
	if !foundMissing {
		t.Fatalf("PriorityContributions = %#v, want a reachability_missing_evidence row", failed.PriorityContributions)
	}

	reachable := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(
		analysisFailedRowEnvelopes(t, "GO-2099-10046", strptr("symbol"), true)))["GO-2099-10046"]
	for _, contribution := range reachable.PriorityContributions {
		if contribution.ReasonCode == "reachability_missing_evidence" {
			t.Fatalf("reachable finding carries a missing-evidence contribution: %#v", contribution)
		}
	}
}
