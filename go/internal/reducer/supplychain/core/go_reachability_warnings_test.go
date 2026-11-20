// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestGoWarningFailureClassReachesMissingEvidence is the R2 regression for
// eshu-hq/eshu#10029 (moved from #10028): a failed govulncheck run emits no
// reachability fact and one scanner_worker.warning naming the failure class.
// The reducer must join that class into Reachability.MissingEvidence so the
// explain, list, MCP, and console surfaces can say why the verdict is
// missing instead of reporting a bare gap.
func TestGoWarningFailureClassReachesMissingEvidence(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	envelopes = append(envelopes, facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "toolchain_unsupported"}),
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
			"repository_id":       realShapeRepositoryID,
		},
	})
	findings := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))
	if len(findings) == 0 {
		t.Fatal("no findings built from the x/text stream with a warning joined")
	}
	for osv, got := range findings {
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		if !stringSliceContains(got.Reachability.MissingEvidence, "govulncheck scan toolchain_unsupported") {
			t.Fatalf("%s: MissingEvidence = %#v, want the warning failure class named", osv, got.Reachability.MissingEvidence)
		}
	}
}

// TestGoWarningWithoutRepositoryJoinsNothing pins the join boundary: a
// warning that names no repository (a pre-contract emitter) cannot be
// attributed to any finding, so it stays out of MissingEvidence rather than
// landing on every Go finding in the pass.
func TestGoWarningWithoutRepositoryJoinsNothing(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	envelopes = append(envelopes, facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "toolchain_unsupported"}),
		FactKind:      "scanner_worker.warning",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"analyzer":        "go_reachability",
			"target_kind":     "repository",
			"reason":          "toolchain_unsupported",
			"warning_class":   "scanner_worker_warning",
			"analysis_status": "not_scanned",
			"coverage_status": "unsupported",
		},
	})
	for osv, got := range supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes)) {
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		if stringSliceContains(got.Reachability.MissingEvidence, "govulncheck scan toolchain_unsupported") {
			t.Fatalf("%s: MissingEvidence = %#v, want no un-attributable warning class", osv, got.Reachability.MissingEvidence)
		}
	}
}

// TestGoCompletedWarningJoinsNothing pins the other boundary: a warning from
// a finished scan (analysis completed, nothing unscanned) is not missing
// evidence, so it must not mark findings partial.
func TestGoWarningCompletedJoinsNothing(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	envelopes = append(envelopes, facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "no_go_modules"}),
		FactKind:      "scanner_worker.warning",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"analyzer":        "go_reachability",
			"target_kind":     "repository",
			"reason":          "no_go_modules",
			"warning_class":   "scanner_worker_warning",
			"analysis_status": "completed",
			"coverage_status": "scanned",
			"repository_id":   realShapeRepositoryID,
		},
	})
	for osv, got := range supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes)) {
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		if stringSliceContains(got.Reachability.MissingEvidence, "govulncheck scan no_go_modules") {
			t.Fatalf("%s: MissingEvidence = %#v, want no completed-scan warning named", osv, got.Reachability.MissingEvidence)
		}
	}
}
