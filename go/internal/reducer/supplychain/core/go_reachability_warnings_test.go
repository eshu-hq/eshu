// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
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

// TestGoWarningRelativePathScopesToItsModule is the #11002 regression: a
// warning naming one failing go.mod (relative_path) joins only the findings
// whose go_module_evidence rows include that path, via the module evidence
// rows behind each finding. A module-scoped warning must not mark the other
// modules' findings in the same repository; a blank or absent relative_path
// keeps repository scope (see TestGoWarningFailureClassReachesMissingEvidence).
func TestGoWarningRelativePathScopesToItsModule(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	const otherModule = "example.com/othermod"
	envelopes = append(envelopes,
		packageConsumptionFactWithRange("consume-other", "gomod://proxy.golang.org/"+otherModule, realShapeRepositoryID, "v1.2.3"),
		goModuleEvidenceEnvelope(t, realShapeRepositoryID, "tools/go.mod", otherModule, "v1.2.3", false, "", ""),
		vulnerabilityCVEFact("cve-GO-2099-0001", "GO-2099-0001", 7.5),
		vulnerabilityAffectedPackageGoRangeFact("affected-GO-2099-0001", "GO-2099-0001", "gomod://proxy.golang.org/"+otherModule, otherModule, "1.2.4"),
		facts.Envelope{
			FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "toolchain_unsupported", "rel": "tools/go.mod"}),
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
				"relative_path":       "tools/go.mod",
			},
		},
	)
	findings := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))
	other, ok := findings["GO-2099-0001"]
	if !ok {
		t.Fatal("no finding built for the warned module example.com/othermod")
	}
	if other.Reachability == nil {
		t.Fatal("GO-2099-0001: Reachability is nil")
	}
	if !stringSliceContains(other.Reachability.MissingEvidence, "govulncheck scan toolchain_unsupported at tools/go.mod") {
		t.Fatalf("GO-2099-0001: MissingEvidence = %#v, want the module-scoped warning naming its go.mod", other.Reachability.MissingEvidence)
	}
	for osv, got := range findings {
		if osv == "GO-2099-0001" {
			continue
		}
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		for _, entry := range got.Reachability.MissingEvidence {
			if strings.HasPrefix(entry, "govulncheck scan toolchain_unsupported") {
				t.Fatalf("%s: MissingEvidence = %#v, want no warning from another module's go.mod", osv, got.Reachability.MissingEvidence)
			}
		}
	}
}

// TestGoWarningPaddedRelativePathJoinsNothing pins the byte-equal rule
// (#11002): a whitespace-padded path is non-blank, so it takes module
// scope, but it byte-matches no evidence row, so it marks nothing and
// must not fall back to repository scope.
func TestGoWarningPaddedRelativePathJoinsNothing(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	const otherModule = "example.com/othermod"
	envelopes = append(envelopes,
		packageConsumptionFactWithRange("consume-other", "gomod://proxy.golang.org/"+otherModule, realShapeRepositoryID, "v1.2.3"),
		goModuleEvidenceEnvelope(t, realShapeRepositoryID, "tools/go.mod", otherModule, "v1.2.3", false, "", ""),
		vulnerabilityCVEFact("cve-GO-2099-0001", "GO-2099-0001", 7.5),
		vulnerabilityAffectedPackageGoRangeFact("affected-GO-2099-0001", "GO-2099-0001", "gomod://proxy.golang.org/"+otherModule, otherModule, "1.2.4"),
		facts.Envelope{
			FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "toolchain_unsupported", "rel": "padded"}),
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
				"relative_path":       " tools/go.mod ",
			},
		},
	)
	findings := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))
	for osv, got := range findings {
		if got.Reachability == nil {
			continue
		}
		for _, entry := range got.Reachability.MissingEvidence {
			if strings.HasPrefix(entry, "govulncheck scan toolchain_unsupported") {
				t.Fatalf("%s: MissingEvidence = %#v, want no join from a padded path that byte-matches nothing", osv, got.Reachability.MissingEvidence)
			}
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

// TestGoWarningOtherAnalyzerJoinsNothing pins the analyzer gate: a warning
// from an analyzer other than go_reachability is not Go scan evidence, so
// it must not mark Go findings.
func TestGoWarningOtherAnalyzerJoinsNothing(t *testing.T) {
	t.Parallel()
	stream := loadGovulncheckStream(t, "xtext_v0_3_5_symbol_source.jsonl")
	envelopes := realShapeImpactEnvelopes(t, stream, nil)
	envelopes = append(envelopes, facts.Envelope{
		FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID, "class": "toolchain_unsupported"}),
		FactKind:      "scanner_worker.warning",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"analyzer":            "sbom",
			"target_kind":         "repository",
			"target_locator_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			"reason":              "toolchain_unsupported",
			"warning_class":       "scanner_worker_warning",
			"analysis_status":     "not_scanned",
			"coverage_status":     "unsupported",
			"repository_id":       realShapeRepositoryID,
		},
	})
	for osv, got := range supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes)) {
		if got.Reachability == nil {
			t.Fatalf("%s: Reachability is nil", osv)
		}
		if stringSliceContains(got.Reachability.MissingEvidence, "govulncheck scan toolchain_unsupported") {
			t.Fatalf("%s: MissingEvidence = %#v, want no warning from another analyzer", osv, got.Reachability.MissingEvidence)
		}
	}
}

// TestGoWarningSkipBranchesAreExplicit pins each skip arm of the warning
// extractor directly: no repository, completed scan, and blank reason each
// yield no rows without quarantine or error.
func TestGoWarningSkipBranchesAreExplicit(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"analyzer":            "go_reachability",
		"target_kind":         "repository",
		"target_locator_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"reason":              "toolchain_unsupported",
		"warning_class":       "scanner_worker_warning",
		"analysis_status":     "not_scanned",
		"coverage_status":     "unsupported",
		"repository_id":       realShapeRepositoryID,
	}
	with := func(mut func(map[string]any)) facts.Envelope {
		payload := map[string]any{}
		for k, v := range base {
			payload[k] = v
		}
		mut(payload)
		return facts.Envelope{
			FactID:        facts.StableID("scanner_worker.warning", map[string]any{"repo": realShapeRepositoryID}),
			FactKind:      facts.ScannerWorkerWarningFactKind,
			SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
			Payload:       payload,
		}
	}
	noRepo := with(func(p map[string]any) { delete(p, "repository_id") })
	completed := with(func(p map[string]any) { p["analysis_status"] = "completed" })
	blankReason := with(func(p map[string]any) { p["reason"] = "  " })
	rows, quarantined, err := extractGoReachabilityWarningsWithQuarantine([]facts.Envelope{noRepo, completed, blankReason})
	if err != nil {
		t.Fatalf("extract error = %v, want nil for skipped warnings", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want none; every skip arm must drop its warning", rows)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none; skips are not malformed payloads", quarantined)
	}
}

// TestGoWarningFatalAbortsClassification pins the fault-isolation boundary
// one level up: when the warning batch carries an unsupported schema major,
// the classifier returns the fatal error instead of partial findings, so a
// version-skewed warning can never silently narrow to a subset of findings.
func TestGoWarningFatalAbortsClassification(t *testing.T) {
	t.Parallel()
	envelopes := []facts.Envelope{{
		FactID:        "warning-unsupported-major",
		FactKind:      "scanner_worker.warning",
		SchemaVersion: "2.0.0",
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
	}}
	findings, quarantined, fatal := classifyGoVulnerabilityReachabilityWithQuarantine(envelopes)
	if fatal == nil {
		t.Fatal("fatal = nil, want non-nil; an unsupported schema major must abort classification")
	}
	if !errors.Is(fatal, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("fatal = %v, want errors.Is factschema.ErrUnsupportedSchemaMajor", fatal)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0; a fatal error discards the partial index", len(findings))
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none; an unsupported major is never a per-fact quarantine", quarantined)
	}
}

// TestGoWarningUnsupportedMajorIsFatal pins the fault-isolation boundary:
// a warning on an unsupported schema major must fail the whole intent
// (never a per-fact quarantine), so version skew gets durable triage.
func TestGoWarningUnsupportedMajorIsFatal(t *testing.T) {
	t.Parallel()
	envelopes := []facts.Envelope{{
		FactID:        "warning-unsupported-major",
		FactKind:      "scanner_worker.warning",
		SchemaVersion: "2.0.0",
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
	}}
	_, quarantined, fatal := extractGoReachabilityWarningsWithQuarantine(envelopes)
	if fatal == nil {
		t.Fatalf("fatal = nil, want non-nil; an unsupported schema major (2.0.0) on a registered scanner_worker fact must fail the whole intent, not be quarantined")
	}
	if !errors.Is(fatal, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("fatal = %v, want errors.Is factschema.ErrUnsupportedSchemaMajor", fatal)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %#v, want none; an unsupported major is fatal, never a per-fact quarantine", quarantined)
	}
}
