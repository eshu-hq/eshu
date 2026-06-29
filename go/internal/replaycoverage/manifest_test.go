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
    ref: testdata/cassettes/awscloud
    proof_gate: golden-corpus-gate
  - surface: parser:hcl
    scenario: parser_fixture
    ref: go/internal/parser/hcl/testdata/fixture.json
    proof_gate: parser-fixture-tests
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
	if len(m.Coverage) != 2 || len(m.Exemptions) != 1 {
		t.Fatalf("coverage=%d exemptions=%d, want 2/1", len(m.Coverage), len(m.Exemptions))
	}
	if m.Coverage[0].Scenario != ScenarioCassette || m.Coverage[0].ProofGate != "golden-corpus-gate" {
		t.Errorf("entry0 = %+v", m.Coverage[0])
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
    ref: x
`)
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected error for invalid scenario type")
	}
}

func TestLoadManifestRejectsBlankFields(t *testing.T) {
	cases := map[string]string{
		"blank surface": `version: "v1"
coverage:
  - surface: ""
    scenario: cassette
    ref: x
`,
		"blank ref": `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    ref: ""
`,
		"blank proof_gate": `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
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
    ref: a
    proof_gate: golden-corpus-gate
  - surface: collector:aws
    scenario: cassette
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
