// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPartialRunNeverReadsNotCalledOrReachable covers a govulncheck run that
// failed before analysis (go.mod requires a newer toolchain than the scanner
// image carries, GOTOOLCHAIN=local). govulncheck printed only its config
// message and exited 1. The emitter must emit no reachability fact, and the
// reducer must not turn the missing evidence into not_called or reachable.
func TestPartialRunNeverReadsNotCalledOrReachable(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "toolchain_unsupported_config_only.jsonl")
	stderr, err := os.ReadFile(filepath.Join("testdata", "govulncheck", "toolchain_unsupported.stderr.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.findings) != 0 {
		t.Fatalf("fixture has %d findings, want a config-only stream", len(stream.findings))
	}

	// The same config-only stream with exit 0 is a clean module: still no fact.
	cleanReach, cleanWarning := deriveGoRunFacts(t, stream, 0, "", realShapeRepositoryID)
	if len(cleanReach) != 0 || cleanWarning != nil {
		t.Fatalf("exit 0 config-only stream = %d facts, warning %v; want a clean module", len(cleanReach), cleanWarning)
	}

	reach, warning := deriveGoRunFacts(t, stream, 1, string(stderr), realShapeRepositoryID)
	if len(reach) != 0 {
		t.Fatalf("failed run emitted %d reachability facts, want none", len(reach))
	}
	if warning == nil || warning.Payload["warning_class"] != "toolchain_unsupported" {
		t.Fatalf("failed run warning = %#v, want warning_class toolchain_unsupported", warning)
	}

	full := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := append(realShapeImpactEnvelopes(t, full, nil), *warning)
	for osv, got := range supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes)) {
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		switch got.Reachability.State {
		case SupplyChainReachabilityNotCalled, SupplyChainReachabilityReachable:
			t.Fatalf("%s: partial run read as %q: %#v", osv, got.Reachability.State, got.Reachability)
		}
		if got.Reachability.State != SupplyChainReachabilityUnknown || got.RuntimeReachability != string(GoVulnReachabilityModuleOnly) {
			t.Fatalf("%s: state=%q runtime=%q, want unknown/module_only", osv, got.Reachability.State, got.RuntimeReachability)
		}
		if !stringSliceContains(got.Reachability.MissingEvidence, "govulncheck call-graph evidence missing") {
			t.Fatalf("%s: MissingEvidence = %#v, want the govulncheck gap named", osv, got.Reachability.MissingEvidence)
		}
	}
	// Known gap, owned by the query/readiness lane (#10002 design ruling):
	// the reducer neither loads nor joins scanner_worker.warning (it carries
	// target_locator_hash, not repository_id), so the failure class
	// toolchain_unsupported does not reach Reachability.MissingEvidence here.
}

// TestStdlibFindingsStaySilentWithoutToolchainEvidence documents what the
// reducer does with govulncheck findings for module "stdlib" (23 of the 27
// findings in the recorded run). Eshu has no go_module_evidence row for the Go
// toolchain, so the reachability fact for a stdlib advisory has no module row
// to join: no impact finding appears, nothing is quarantined, and the x/text
// findings are unaffected. Standard-library advisories therefore never surface
// until the owner decides to emit toolchain module evidence.
func TestStdlibFindingsStaySilentWithoutToolchainEvidence(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	reach := deriveGoCallReachabilityFacts(t, stream, realShapeRepositoryID)

	stdlibFacts := 0
	envelopes := realShapeImpactEnvelopes(t, stream, reach)
	packageID := "gomod://proxy.golang.org/stdlib"
	seen := map[string]bool{}
	for _, finding := range stream.findings {
		if finding.Trace[0].Module != "stdlib" || seen[finding.OSV] {
			continue
		}
		seen[finding.OSV] = true
		envelopes = append(envelopes,
			vulnerabilityCVEFact("cve-"+finding.OSV, finding.OSV, 7.5),
			vulnerabilityAffectedPackageGoRangeFact("affected-"+finding.OSV, finding.OSV, packageID, "stdlib", "1.26.9"),
		)
	}
	for _, envelope := range reach {
		if envelope.Payload["deepest_module"] == "stdlib" {
			stdlibFacts++
		}
	}
	if stdlibFacts == 0 || len(seen) == 0 {
		t.Fatalf("fixture lost its stdlib findings (facts=%d advisories=%d)", stdlibFacts, len(seen))
	}

	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("build findings: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("stdlib reachability facts were quarantined: %#v", quarantined)
	}
	for _, finding := range findings {
		if seen[finding.CVEID] {
			t.Fatalf("stdlib advisory %s produced a finding without toolchain evidence: %#v", finding.CVEID, finding)
		}
	}
	if got := len(supplyChainImpactFindingsByCVE(findings)); got != 4 {
		t.Fatalf("findings = %d, want only the 4 x/text advisories", got)
	}
	if classified := ClassifyGoVulnerabilityReachability(envelopes); len(classified) != 4 {
		t.Fatalf("classified = %d, want only the 4 x/text advisories", len(classified))
	}
}
