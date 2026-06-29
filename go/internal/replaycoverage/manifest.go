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
	// ScenarioGoTest is a Go package or test file whose scenario is proven by a
	// Go test gate. It is used for deterministic replay depth scenarios whose
	// artifact is executable test code rather than a cassette or B-12 snapshot id.
	ScenarioGoTest ScenarioType = "go_test"
	// ScenarioProofArtifact is a repo-relative proof contract or evidence file
	// whose greenness is proven by the named proof gate.
	ScenarioProofArtifact ScenarioType = "proof_artifact"
)

// DepthScenarioType is the scenario-depth class a replay artifact covers for a
// supported surface. The baseline class preserves the original breadth/existence
// contract; the other classes are the C-8 depth dimensions from deterministic
// replay design §11 and issue #4187.
type DepthScenarioType string

const (
	// ScenarioTypeBaseline is the default happy-path scenario for a supported
	// surface: the original C-1 breadth coverage.
	ScenarioTypeBaseline DepthScenarioType = "baseline"
	// ScenarioTypeDeltaTombstone proves generation deltas and retract/tombstone
	// behavior for surfaces whose truth can be removed or superseded.
	ScenarioTypeDeltaTombstone DepthScenarioType = "delta_tombstone"
	// ScenarioTypeFault proves boundary or read-path fault handling such as
	// timeouts, partial responses, and upstream 5xx errors.
	ScenarioTypeFault DepthScenarioType = "fault"
	// ScenarioTypeOrdering proves deterministic behavior under alternate
	// delivery or projection ordering.
	ScenarioTypeOrdering DepthScenarioType = "ordering"
	// ScenarioTypeCrash proves crash/restart recovery at deterministic replay
	// crash points.
	ScenarioTypeCrash DepthScenarioType = "crash"
	// ScenarioTypeCost proves deterministic cost-counting or budget behavior.
	ScenarioTypeCost DepthScenarioType = "cost"
)

// validScenarioTypes is the closed set of scenario artifact kinds.
var validScenarioTypes = map[ScenarioType]struct{}{
	ScenarioCassette:        {},
	ScenarioParserFixture:   {},
	ScenarioAPIMCPGolden:    {},
	ScenarioCorrelation:     {},
	ScenarioCapabilityClaim: {},
	ScenarioProductClaim:    {},
	ScenarioGoTest:          {},
	ScenarioProofArtifact:   {},
}

// validDepthScenarioTypes is the closed set of C-8 scenario-depth classes.
var validDepthScenarioTypes = map[DepthScenarioType]struct{}{
	ScenarioTypeBaseline:       {},
	ScenarioTypeDeltaTombstone: {},
	ScenarioTypeFault:          {},
	ScenarioTypeOrdering:       {},
	ScenarioTypeCrash:          {},
	ScenarioTypeCost:           {},
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
	// ScenarioType is the depth class this scenario covers for Surface.
	ScenarioType DepthScenarioType
	// Ref is the scenario artifact: a repo-relative path (cassette dir,
	// parser-fixture file) or a snapshot id (rc-* / query-shape key).
	Ref string
	// ProofGate names the CI gate that runs the scenario and proves it green. It
	// is required on every coverage entry: it is what ties existence-only coverage
	// to an actual passing verifier, so a covered surface always has a named gate
	// proving its scenario green.
	ProofGate string
}

// ScenarioRequirement declares which depth scenario types are required for one
// supported surface. Every requirement must include baseline; surfaces without
// an explicit requirement require only baseline. The manifest uses requirements
// to opt surfaces into additional depth as the underlying R-11/R-13/R-14/R-16/R-17
// artifacts land.
type ScenarioRequirement struct {
	// Surface is the canonical coverage key the requirement applies to.
	Surface string
	// ScenarioTypes are the required depth classes for Surface.
	ScenarioTypes []DepthScenarioType
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
	// Requirements are the per-surface depth scenario types required beyond the
	// default baseline breadth coverage.
	Requirements []ScenarioRequirement
	// Exemptions are the deliberately-uncovered surfaces with reasons.
	Exemptions []Exemption
}

type manifestFile struct {
	Version      string                    `yaml:"version"`
	Coverage     []manifestFileEntry       `yaml:"coverage"`
	Requirements []manifestFileRequirement `yaml:"scenario_requirements"`
	Exemptions   []manifestFileExempt      `yaml:"exemptions"`
}

type manifestFileEntry struct {
	Surface      string `yaml:"surface"`
	Scenario     string `yaml:"scenario"`
	ScenarioType string `yaml:"scenario_type"`
	Ref          string `yaml:"ref"`
	ProofGate    string `yaml:"proof_gate"`
}

type manifestFileRequirement struct {
	Surface       string   `yaml:"surface"`
	ScenarioTypes []string `yaml:"scenario_types"`
}

type manifestFileExempt struct {
	Surface string `yaml:"surface"`
	Reason  string `yaml:"reason"`
}

// LoadManifest reads the replay coverage manifest from path. A missing file is an
// empty manifest (every surface then reports uncovered — the keystone's honest
// red state), not an error. The loader rejects blank surfaces/refs/reasons,
// blank proof_gates, invalid artifact kinds or depth scenario types, duplicate
// surface+scenario_type pairs, requirements that drop baseline, and a surface
// declared both covered/required and exempt, because any of those silently
// corrupts the coverage truth.
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
	declaredSurfaces := map[string]string{} // surface -> origin ("coverage"/"exemption")
	declaredCoverage := map[string]struct{}{}

	for _, entry := range parsed.Coverage {
		surface := strings.TrimSpace(entry.Surface)
		ref := strings.TrimSpace(entry.Ref)
		proofGate := strings.TrimSpace(entry.ProofGate)
		scenario := ScenarioType(strings.TrimSpace(entry.Scenario))
		scenarioType := DepthScenarioType(strings.TrimSpace(entry.ScenarioType))
		if surface == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: entry has blank surface", path)
		}
		if ref == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has blank ref", path, surface)
		}
		if _, ok := validScenarioTypes[scenario]; !ok {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has invalid scenario type %q", path, surface, entry.Scenario)
		}
		if _, ok := validDepthScenarioTypes[scenarioType]; !ok {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has invalid scenario_type %q", path, surface, entry.ScenarioType)
		}
		// proof_gate is required, not optional: it is the gate that proves the
		// scenario green. Without it, Reconcile would mark the surface covered on
		// artifact existence alone — a false green that lets the blocking gate and
		// the C-7 report claim a passing replay scenario nothing actually verifies.
		if proofGate == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has blank proof_gate (every coverage entry must name the gate that proves its scenario green)", path, surface)
		}
		coverageKey := manifestCoverageKey(surface, scenarioType)
		if _, dup := declaredCoverage[coverageKey]; dup {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q declares scenario_type %q twice", path, surface, scenarioType)
		}
		declaredCoverage[coverageKey] = struct{}{}
		declaredSurfaces[surface] = "coverage"
		m.Coverage = append(m.Coverage, CoverageEntry{
			Surface:      surface,
			Scenario:     scenario,
			ScenarioType: scenarioType,
			Ref:          ref,
			ProofGate:    proofGate,
		})
	}

	declaredRequirements := map[string]struct{}{}
	for _, req := range parsed.Requirements {
		surface := strings.TrimSpace(req.Surface)
		if surface == "" {
			return Manifest{}, fmt.Errorf("coverage manifest %s: scenario requirement has blank surface", path)
		}
		if _, dup := declaredRequirements[surface]; dup {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has duplicate scenario requirement", path, surface)
		}
		declaredRequirements[surface] = struct{}{}
		seenTypes := map[DepthScenarioType]struct{}{}
		var scenarioTypes []DepthScenarioType
		hasBaseline := false
		for _, rawType := range req.ScenarioTypes {
			scenarioType := DepthScenarioType(strings.TrimSpace(rawType))
			if _, ok := validDepthScenarioTypes[scenarioType]; !ok {
				return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has invalid required scenario_type %q", path, surface, rawType)
			}
			if _, dup := seenTypes[scenarioType]; dup {
				return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q requires scenario_type %q twice", path, surface, scenarioType)
			}
			seenTypes[scenarioType] = struct{}{}
			if scenarioType == ScenarioTypeBaseline {
				hasBaseline = true
			}
			scenarioTypes = append(scenarioTypes, scenarioType)
		}
		if len(scenarioTypes) == 0 {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q has no required scenario_types", path, surface)
		}
		if !hasBaseline {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q scenario requirement must include baseline", path, surface)
		}
		m.Requirements = append(m.Requirements, ScenarioRequirement{Surface: surface, ScenarioTypes: scenarioTypes})
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
		if origin, dup := declaredSurfaces[surface]; dup {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q declared twice (already a %s entry)", path, surface, origin)
		}
		if _, required := declaredRequirements[surface]; required {
			return Manifest{}, fmt.Errorf("coverage manifest %s: surface %q declared twice (already a scenario requirement)", path, surface)
		}
		declaredSurfaces[surface] = "exemption"
		m.Exemptions = append(m.Exemptions, Exemption{Surface: surface, Reason: reason})
	}

	return m, nil
}

func manifestCoverageKey(surface string, scenarioType DepthScenarioType) string {
	return surface + "|" + string(scenarioType)
}
