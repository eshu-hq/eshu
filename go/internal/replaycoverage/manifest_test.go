// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "replay-coverage-manifest.v1.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestLoadManifest(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: testdata/cassettes/awscloud
    proof_gate: golden-corpus-gate
  - surface: parser:hcl
    scenario: parser_fixture
    scenario_type: baseline
    ref: go/internal/parser/hcl/testdata/fixture.json
    proof_gate: parser-fixture-tests
  - surface: capability:cap.profiled
    scenario: capability_claim
    scenario_type: cost
    ref: cap.profiled
    proof_gate: capability-inventory
scenario_requirements:
  - surface: collector:aws
    scenario_types: [baseline, fault]
  - surface: capability:cap.profiled
    scenario_types: [baseline, cost]
exemptions:
  - surface: capability:cap.design_only
    reason: research-only capability with no runtime path
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Version != "v1" {
		t.Errorf("version = %q, want v1", m.Version)
	}
	if len(m.Coverage) != 3 || len(m.Exemptions) != 1 {
		t.Fatalf("coverage=%d exemptions=%d, want 3/1", len(m.Coverage), len(m.Exemptions))
	}
	if m.Coverage[0].Scenario != ScenarioCassette || m.Coverage[0].ProofGate != "golden-corpus-gate" {
		t.Errorf("entry0 = %+v", m.Coverage[0])
	}
	if m.Coverage[2].Scenario != ScenarioCapabilityClaim {
		t.Errorf("entry2 scenario = %q, want %q", m.Coverage[2].Scenario, ScenarioCapabilityClaim)
	}
	if m.Coverage[2].ScenarioType != ScenarioTypeCost {
		t.Errorf("entry2 scenario_type = %q, want %q", m.Coverage[2].ScenarioType, ScenarioTypeCost)
	}
	if len(m.Requirements) != 2 {
		t.Fatalf("requirements=%d, want 2", len(m.Requirements))
	}
	if got := m.Requirements[0].ScenarioTypes; len(got) != 2 || got[0] != ScenarioTypeBaseline || got[1] != ScenarioTypeFault {
		t.Errorf("collector requirements = %v, want baseline/fault", got)
	}
}

func TestLoadManifestMissingFileIsEmpty(t *testing.T) {
	// A missing manifest is an empty manifest, not an error: a brand-new repo
	// state legitimately covers nothing, and the gate then reports every surface
	// uncovered (the keystone's red worklist) rather than failing to run.
	m, err := LoadManifest(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("LoadManifest(absent) = %v, want nil", err)
	}
	if len(m.Coverage) != 0 || len(m.Exemptions) != 0 {
		t.Errorf("absent manifest not empty: %+v", m)
	}
}

func TestLoadManifestRejectsInvalidScenarioType(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: collector:aws
    scenario: bogus
    scenario_type: baseline
    ref: x
    proof_gate: golden-corpus-gate
`)
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for invalid scenario type")
	}
}

func TestLoadManifestRejectsInvalidDepthScenarioType(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: impossible
    ref: x
    proof_gate: golden-corpus-gate
`)
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for invalid depth scenario_type")
	}
}

func TestLoadManifestRejectsBlankFields(t *testing.T) {
	cases := map[string]string{
		"blank surface": `version: "v1"
coverage:
  - surface: ""
    scenario: cassette
    scenario_type: baseline
    ref: x
    proof_gate: golden-corpus-gate
`,
		"blank ref": `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: ""
    proof_gate: golden-corpus-gate
`,
		"blank scenario_type": `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    ref: x
    proof_gate: golden-corpus-gate
`,
		"blank proof_gate": `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: x
`,
		"blank exemption reason": `version: "v1"
exemptions:
  - surface: collector:aws
    reason: ""
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadManifest(writeManifest(t, body)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestLoadManifestRejectsDuplicateAndConflictingSurface(t *testing.T) {
	// proof_gate is set on every entry so the duplicate/conflict paths are the
	// ones that fire, not the blank-proof_gate guard that precedes them.
	dup := `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: a
    proof_gate: golden-corpus-gate
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: b
    proof_gate: golden-corpus-gate
`
	if _, err := LoadManifest(writeManifest(t, dup)); err == nil {
		t.Fatal("expected error for duplicate coverage surface")
	}
	conflict := `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: a
    proof_gate: golden-corpus-gate
exemptions:
  - surface: collector:aws
    reason: cannot be both covered and exempt
`
	if _, err := LoadManifest(writeManifest(t, conflict)); err == nil {
		t.Fatal("expected error for surface both covered and exempt")
	}
}

func TestLoadManifestAllowsSameSurfaceWithDifferentDepthScenarioTypes(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: happy-path
    proof_gate: golden-corpus-gate
  - surface: collector:aws
    scenario: cassette
    scenario_type: fault
    ref: fault-path
    proof_gate: golden-corpus-gate
scenario_requirements:
  - surface: collector:aws
    scenario_types: [baseline, fault]
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Coverage) != 2 {
		t.Fatalf("coverage=%d, want 2", len(m.Coverage))
	}
}

func TestLoadManifestRejectsDepthRequirementsWithoutBaseline(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: fault
    ref: fault-path
    proof_gate: golden-corpus-gate
scenario_requirements:
  - surface: collector:aws
    scenario_types: [fault]
`)
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for scenario requirement that omits baseline")
	}
}

func TestLoadManifestRejectsRequirementForExemptSurface(t *testing.T) {
	path := writeManifest(t, `version: "v1"
scenario_requirements:
  - surface: collector:aws
    scenario_types: [baseline, fault]
exemptions:
  - surface: collector:aws
    reason: cannot be required and exempt
`)
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for surface both required and exempt")
	}
}

func TestLoadManifestParsesLanguageExemptions(t *testing.T) {
	path := writeManifest(t, `version: "v1"
language_exemptions:
  - surface: language:go
    reason: exercised end-to-end by the golden-corpus 20-repo corpus
  - surface: language:python
    reason: exercised end-to-end by the golden-corpus 20-repo corpus
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.LanguageExemptions) != 2 {
		t.Fatalf("language exemptions = %d, want 2", len(m.LanguageExemptions))
	}
	if m.LanguageExemptions[0].Surface != "language:go" || m.LanguageExemptions[0].Reason == "" {
		t.Errorf("first language exemption = %+v", m.LanguageExemptions[0])
	}
	// Language exemptions live in their own namespace and must NOT leak into the
	// blocking surface reconcile's exemption set.
	if len(m.Exemptions) != 0 {
		t.Errorf("language exemptions must not populate surface Exemptions; got %d", len(m.Exemptions))
	}
}

func TestLoadManifestRejectsBadLanguageExemptions(t *testing.T) {
	for name, body := range map[string]string{
		"missing prefix": "version: \"v1\"\nlanguage_exemptions:\n  - surface: go\n    reason: r\n",
		"blank name":     "version: \"v1\"\nlanguage_exemptions:\n  - surface: \"language:\"\n    reason: r\n",
		"blank reason":   "version: \"v1\"\nlanguage_exemptions:\n  - surface: language:go\n    reason: \"\"\n",
		"duplicate":      "version: \"v1\"\nlanguage_exemptions:\n  - surface: language:go\n    reason: r\n  - surface: language:go\n    reason: r2\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadManifest(writeManifest(t, body)); err == nil {
				t.Fatalf("%s language exemption must be a load error", name)
			}
		})
	}
}

func TestLoadRealManifestLanguageExemptionsMatchLedger(t *testing.T) {
	// Every committed language exemption must name a real ledger language, and the
	// scoreboard must enumerate all 32 — no language silently absent, no stale
	// exemption. This binds the manifest to the ledger in CI.
	specs := repoSpecsDir(t)
	m, err := LoadManifest(filepath.Join(specs, ManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest(real): %v", err)
	}
	ledger, err := LoadLanguageLedger(filepath.Join(specs, LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger(real): %v", err)
	}
	board := BuildLanguageScoreboard(ledger, m.LanguageExemptions)
	if len(board.StaleExemptions) != 0 {
		t.Fatalf("committed manifest has stale language exemptions: %v", board.StaleExemptions)
	}
	if board.Total != len(ledger.Languages) || board.Total != board.Exempt+board.Uncovered {
		t.Fatalf("scoreboard does not account for every ledger language: %+v", board)
	}
	if board.Exempt != len(m.LanguageExemptions) {
		t.Fatalf("exempt count %d != committed exemptions %d", board.Exempt, len(m.LanguageExemptions))
	}
}
