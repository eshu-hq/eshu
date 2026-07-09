// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import "testing"

// TestLoadManifestAcceptsOduScenario proves the "odu" scenario artifact kind
// (Ifá's own coverage manifest, #4394) loads through the same closed-set
// validation every other scenario kind uses, and that an unrelated bogus token
// still fails: adding "odu" must not open the set to arbitrary strings.
func TestLoadManifestAcceptsOduScenario(t *testing.T) {
	path := writeManifest(t, `version: "v1"
coverage:
  - surface: fact_kind:repository
    scenario: odu
    scenario_type: baseline
    ref: "odu:kustomize-deploys-from"
    proof_gate: ifa-contract-layer
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest(odu scenario): %v", err)
	}
	if len(m.Coverage) != 1 || m.Coverage[0].Scenario != ScenarioOdu {
		t.Fatalf("coverage = %+v, want one entry with scenario %q", m.Coverage, ScenarioOdu)
	}

	if _, err := LoadManifest(writeManifest(t, `version: "v1"
coverage:
  - surface: fact_kind:repository
    scenario: still-bogus
    scenario_type: baseline
    ref: x
    proof_gate: ifa-contract-layer
`)); err == nil {
		t.Fatal("expected error for bogus scenario type even after odu is valid")
	}
}
