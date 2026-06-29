// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFileName is the replay coverage manifest inside the specs directory.
// It is the curated, reviewable declaration that maps each supported surface to
// the replay scenario that exercises it. It exists because the natural keys
// differ across registries and artifacts (a "collector:aws" surface is exercised
// by the cassette under testdata/cassettes/awscloud) — a mapping no single
// registry can express on its own.
const ManifestFileName = "replay-coverage-manifest.v1.yaml"

// ScenarioType is the kind of replay scenario that covers a surface. It mirrors
// the design §2 chain: a cassette drives a collector, a parser fixture drives a
// parser, an api/mcp golden drives a read surface, a correlation rc-* drives a
// graph correlation, a capability claim points at the matrix row whose
// per-profile verification refs prove supported answers and unsupported
// capability refusals, and a product claim points at the public claim ledger row
// whose proof is checked by capability-inventory docs mode.
type ScenarioType string

const (
	// ScenarioCassette is a recorded fact cassette under testdata/cassettes/.
	ScenarioCassette ScenarioType = "cassette"
	// ScenarioParserFixture is an R-7 parser-fixture replay file.
	ScenarioParserFixture ScenarioType = "parser_fixture"
	// ScenarioAPIMCPGolden is a B-12 snapshot query shape (HTTP or MCP).
	ScenarioAPIMCPGolden ScenarioType = "api_mcp_golden"
	// ScenarioCorrelation is a B-12 snapshot required correlation (rc-*).
	ScenarioCorrelation ScenarioType = "correlation"
	// ScenarioCapabilityClaim is a capability-matrix row with per-profile
	// verification refs for supported answers and unsupported-capability refusals.
	ScenarioCapabilityClaim ScenarioType = "capability_claim"
	// ScenarioProductClaim is a product-claims ledger row with deterministic
	// proof command/signals for a broad public promise.
	ScenarioProductClaim ScenarioType = "product_claim"
)

// validScenarioTypes is the closed set of scenario types.
var validScenarioTypes = map[ScenarioType]struct{}{
	ScenarioCassette:        {},
	ScenarioParserFixture:   {},
	ScenarioAPIMCPGolden:    {},
	ScenarioCorrelation:     {},
	ScenarioCapabilityClaim: {},
	ScenarioProductClaim:    {},
}

// CoverageEntry declares that one supported surface is exercised by one replay
// scenario. The gate verifies the referenced scenario artifact actually exists;
// it does not re-run it. ProofGate names the CI gate that proves the scenario
// green (e.g. golden-corpus-gate), tying coverage to a green-proving gate without
// this gate duplicating that work.
type CoverageEntry struct {
	// Surface is the canonical coverage key (matches SupportedSurface.Key).
	Surface string
	// Scenario is the kind of replay scenario.
	Scenario ScenarioType
	// Ref is the scenario artifact: a repo-relative path (cassette dir,
	// parser-fixture file) or a snapshot id (rc-* / query-shape key).
	Ref string
	// ProofGate names the CI gate that runs the scenario and proves it green. It
	// is required on every coverage entry: it is what ties existence-only coverage
	// to an actual passing verifier, so a covered surface always has a named gate
	// proving its scenario green.
	ProofGate string
}

// Exemption records a supported surface that is deliberately not required to have
// a replay scenario, with a reason. Exemptions are surfaced in the coverage
// report (never silently dropped) so a reviewer can audit every one.
type Exemption struct {
	// Surface is the canonical coverage key the exemption applies to.
	Surface string
	// Reason explains why the surface needs no replay scenario.
	Reason string
}

// Manifest is the parsed replay coverage manifest.
type Manifest struct {
	// Version is the manifest schema version.
	Version string
	// Coverage are the declared surface-to-scenario mappings.
	Coverage []CoverageEntry
	// Exemptions are the deliberately-uncovered surfaces with reasons.
	Exemptions []Exemption
}

type manifestFile struct {
	Version    string               `yaml:"version"`
	Coverage   []manifestFileEntry  `yaml:"coverage"`
	Exemptions []manifestFileExempt `yaml:"exemptions"`
}

type manifestFileEntry struct {
	Surface   string `yaml:"surface"`
	Scenario  string `yaml:"scenario"`
	Ref       string `yaml:"ref"`
	ProofGate string `yaml:"proof_gate"`
}

type manifestFileExempt struct {
	Surface string `yaml:"surface"`
	Reason  string `yaml:"reason"`
}

// LoadManifest reads the replay coverage manifest from path. A missing file is an
// empty manifest (every surface then reports uncovered — the keystone's honest
// red state), not an error. The loader rejects blank surfaces/refs/reasons,
// blank proof_gates, invalid scenario types, duplicate surfaces, and a surface
// declared both covered and exempt, because any of those silently corrupts the
// coverage truth.
func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured coverage manifest under specs/, not external input
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, nil
		}
		return Manifest{}, fmt.Errorf("read coverage manifest %s: %w", path, err)
	}
	var parsed manifestFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return Manifest{}, fmt.Errorf("parse coverage manifest %s: %w", path, err)
	}

	m := Manifest{Version: parsed.Version}
	declared := map[string]string{} // surface -> origin ("coverage"/"exemption")

	for _, entry := range parsed.Coverage {
		surface := strings.TrimSpace(entry.Surface)
		ref := strings.TrimSpace(entry.Ref)
		proofGate := strings.TrimSpace(entry.ProofGate)
		scenario := ScenarioType(strings.TrimSpace(entry.Scenario))
		if surface == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: entry has blank surface", path)
		}
		if ref == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has blank ref", path, surface)
		}
		if _, ok := validScenarioTypes[scenario]; !ok {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has invalid scenario type %q", path, surface, entry.Scenario)
		}
		// proof_gate is required, not optional: it is the gate that proves the
		// scenario green. Without it, Reconcile would mark the surface covered on
		// artifact existence alone — a false green that lets the blocking gate and
		// the C-7 report claim a passing replay scenario nothing actually verifies.
		if proofGate == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has blank proof_gate (every coverage entry must name the gate that proves its scenario green)", path, surface)
		}
		if origin, dup := declared[surface]; dup {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q declared twice (already a %s entry)", path, surface, origin)
		}
		declared[surface] = "coverage"
		m.Coverage = append(m.Coverage, CoverageEntry{
			Surface:   surface,
			Scenario:  scenario,
			Ref:       ref,
			ProofGate: proofGate,
		})
	}

	for _, ex := range parsed.Exemptions {
		surface := strings.TrimSpace(ex.Surface)
		reason := strings.TrimSpace(ex.Reason)
		if surface == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: exemption has blank surface", path)
		}
		if reason == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: exemption %q has blank reason", path, surface)
		}
		if origin, dup := declared[surface]; dup {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q declared twice (already a %s entry)", path, surface, origin)
		}
		declared[surface] = "exemption"
		m.Exemptions = append(m.Exemptions, Exemption{Surface: surface, Reason: reason})
	}

	return m, nil
}
