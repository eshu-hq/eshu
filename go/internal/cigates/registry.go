// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tier is the ordered stage at which a gate runs.
// Ordering: pre-commit < pre-push < pre-pr < ci-heavy < manual.
type Tier string

const (
	// TierPreCommit runs at git commit time (fast structural checks).
	TierPreCommit Tier = "pre-commit"
	// TierPrePush runs at git push time (slightly heavier checks).
	TierPrePush Tier = "pre-push"
	// TierPrePR runs before a pull request is opened (full local suite).
	TierPrePR Tier = "pre-pr"
	// TierCIHeavy runs only in CI (requires services, long wall time).
	TierCIHeavy Tier = "ci-heavy"
	// TierManual runs on demand only.
	TierManual Tier = "manual"
)

// tierOrder maps each Tier to a numeric rank for comparisons.
var tierOrder = map[Tier]int{
	TierPreCommit: 1,
	TierPrePush:   2,
	TierPrePR:     3,
	TierCIHeavy:   4,
	TierManual:    5,
}

// TierAtMost reports whether t is at most other in the ordering.
// Returns false for unknown tiers.
func TierAtMost(t, other Tier) bool {
	a, aOK := tierOrder[t]
	b, bOK := tierOrder[other]
	return aOK && bOK && a <= b
}

// Category classifies a gate's concern.
type Category string

const (
	// CategoryExactness gates that verify contract drift (OpenAPI, route coverage, …).
	CategoryExactness Category = "exactness"
	// CategoryRace gates that run the Go race detector.
	CategoryRace Category = "race"
	// CategorySecurity security-scanner gates.
	CategorySecurity Category = "security"
	// CategoryDocs documentation build gates.
	CategoryDocs Category = "docs"
	// CategoryFrontend JavaScript/TypeScript gates.
	CategoryFrontend Category = "frontend"
	// CategoryBuild compilation and build gates.
	CategoryBuild Category = "build"
	// CategoryTelemetry observability coverage gates.
	CategoryTelemetry Category = "telemetry"
	// CategoryHygiene style, formatting, and structural hygiene gates.
	CategoryHygiene Category = "hygiene"
	// CategoryRelease release and publish gates.
	CategoryRelease Category = "release"
)

// Requirement names a tool or service a gate needs to run.
type Requirement string

const (
	// ReqGo requires the Go toolchain.
	ReqGo Requirement = "go"
	// ReqNode requires Node.js.
	ReqNode Requirement = "node"
	// ReqDocker requires Docker.
	ReqDocker Requirement = "docker"
	// ReqNornicDB requires a running NornicDB instance.
	ReqNornicDB Requirement = "nornicdb"
	// ReqPostgres requires a running Postgres instance.
	ReqPostgres Requirement = "postgres"
	// ReqNetwork requires external network access.
	ReqNetwork Requirement = "network"
	// ReqCredentials requires deployment credentials.
	ReqCredentials Requirement = "credentials"
	// ReqReleaseToken requires a release token.
	ReqReleaseToken Requirement = "release_token"
)

// Local holds the local execution config for a gate.
type Local struct {
	// Command is the shell command to run the gate locally.
	Command string
	// TestCommand is the optional self-test mirror command.
	TestCommand string
}

// CI holds the CI execution config for a gate.
type CI struct {
	// Workflow is the GitHub Actions workflow filename under .github/workflows/.
	Workflow string
	// Job is the display name of the CI job.
	Job string
}

// Gate is a single CI/local gate entry in the registry.
type Gate struct {
	// ID is the stable kebab-case identifier, unique within the registry.
	ID string
	// Name is the human-readable display name.
	Name string
	// Category classifies the gate's concern.
	Category Category
	// Tier is the ordered stage at which this gate runs.
	Tier Tier
	// Blocking controls whether a gate failure is required (true) or advisory (false).
	Blocking bool
	// Triggers are the path globs that activate this gate when matched against changed paths.
	// Must be non-empty.
	Triggers []string
	// Local is the local execution config. Nil when the gate is CI-only.
	Local *Local
	// CI is the CI execution config.
	CI CI
	// Requirements lists the tools or services this gate needs.
	Requirements []Requirement
	// CIOnlyReason is required and non-empty when Local is nil; it explains why
	// the gate cannot run locally.
	CIOnlyReason string
	// HookID is the .pre-commit-config.yaml hook id that this gate corresponds to.
	// Empty when the gate has no direct local hook equivalent.
	HookID string
}

// HygieneHook declares a .pre-commit-config.yaml local hook id that is
// intentional basic hygiene and deliberately NOT a registry gate.
type HygieneHook struct {
	// ID is the pre-commit hook id.
	ID string
	// Reason explains why this hook is hygiene rather than a gate.
	Reason string
}

// NonGateWorkflow declares a .github/workflows/*.yml filename that is
// intentionally not a PR gate (e.g. a deploy, release, schedule, or bot workflow).
type NonGateWorkflow struct {
	// File is the workflow filename (basename only, e.g. "deploy-root-docs.yml").
	File string
	// Reason explains why this workflow is not a PR gate.
	Reason string
}

// Registry is the parsed ci-gates registry.
type Registry struct {
	// Version is the registry schema version.
	Version string
	// Gates is the ordered list of gate entries.
	Gates []Gate
	// HygieneHooks lists pre-commit hook ids that are basic hygiene and
	// intentionally not registry gates.
	HygieneHooks []HygieneHook
	// NonGateWorkflows lists .github/workflows/*.yml files that are not PR gates.
	NonGateWorkflows []NonGateWorkflow
}

// --- YAML parse types ---

type registryFile struct {
	Version          string                `yaml:"version"`
	Gates            []gateFile            `yaml:"gates"`
	HygieneHooks     []hygieneHookFile     `yaml:"hygiene_hooks"`
	NonGateWorkflows []nonGateWorkflowFile `yaml:"non_gate_workflows"`
}

type gateFile struct {
	ID           string     `yaml:"id"`
	Name         string     `yaml:"name"`
	Category     string     `yaml:"category"`
	Tier         string     `yaml:"tier"`
	Blocking     bool       `yaml:"blocking"`
	Triggers     []string   `yaml:"triggers"`
	Local        *localFile `yaml:"local"`
	CI           ciFile     `yaml:"ci"`
	Requirements []string   `yaml:"requirements"`
	CIOnlyReason string     `yaml:"ci_only_reason"`
	HookID       string     `yaml:"hook_id"`
}

type localFile struct {
	Command     string `yaml:"command"`
	TestCommand string `yaml:"test_command"`
}

type ciFile struct {
	Workflow string `yaml:"workflow"`
	Job      string `yaml:"job"`
}

type hygieneHookFile struct {
	ID     string `yaml:"id"`
	Reason string `yaml:"reason"`
}

type nonGateWorkflowFile struct {
	File   string `yaml:"file"`
	Reason string `yaml:"reason"`
}

// validCategories is the closed set of allowed Category values.
var validCategories = map[Category]struct{}{
	CategoryExactness: {},
	CategoryRace:      {},
	CategorySecurity:  {},
	CategoryDocs:      {},
	CategoryFrontend:  {},
	CategoryBuild:     {},
	CategoryTelemetry: {},
	CategoryHygiene:   {},
	CategoryRelease:   {},
}

// validRequirements is the closed set of allowed Requirement values.
var validRequirements = map[Requirement]struct{}{
	ReqGo:           {},
	ReqNode:         {},
	ReqDocker:       {},
	ReqNornicDB:     {},
	ReqPostgres:     {},
	ReqNetwork:      {},
	ReqCredentials:  {},
	ReqReleaseToken: {},
}

// Load reads and structurally validates the ci-gates registry at path.
// Validation checks: unique IDs, non-empty triggers, valid category/tier/requirement
// enum values, and that local==null gates have a non-empty ci_only_reason.
func Load(path string) (*Registry, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured gate registry under specs/, not external input
	if err != nil {
		return nil, fmt.Errorf("read ci-gates registry %s: %w", path, err)
	}
	var parsed registryFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse ci-gates registry %s: %w", path, err)
	}

	reg := &Registry{Version: parsed.Version}
	seenIDs := make(map[string]struct{}, len(parsed.Gates))

	for i, gf := range parsed.Gates {
		id := strings.TrimSpace(gf.ID)
		if id == "" {
			return nil, fmt.Errorf("ci-gates registry %s: gate[%d] has blank id", path, i)
		}
		if _, dup := seenIDs[id]; dup {
			return nil, fmt.Errorf("ci-gates registry %s: gate id %q is not unique", path, id)
		}
		seenIDs[id] = struct{}{}

		cat := Category(strings.TrimSpace(gf.Category))
		if _, ok := validCategories[cat]; !ok {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has invalid category %q", path, id, gf.Category)
		}

		tier := Tier(strings.TrimSpace(gf.Tier))
		if _, ok := tierOrder[tier]; !ok {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has invalid tier %q", path, id, gf.Tier)
		}

		if len(gf.Triggers) == 0 {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has empty triggers (must be non-empty)", path, id)
		}

		reqs := make([]Requirement, 0, len(gf.Requirements))
		for _, r := range gf.Requirements {
			req := Requirement(strings.TrimSpace(r))
			if _, ok := validRequirements[req]; !ok {
				return nil, fmt.Errorf("ci-gates registry %s: gate %q has invalid requirement %q", path, id, r)
			}
			reqs = append(reqs, req)
		}

		var local *Local
		if gf.Local != nil {
			local = &Local{
				Command:     strings.TrimSpace(gf.Local.Command),
				TestCommand: strings.TrimSpace(gf.Local.TestCommand),
			}
		}

		ciOnlyReason := strings.TrimSpace(gf.CIOnlyReason)
		if local == nil && ciOnlyReason == "" {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has local==null but empty ci_only_reason (required when local is absent)", path, id)
		}

		reg.Gates = append(reg.Gates, Gate{
			ID:           id,
			Name:         strings.TrimSpace(gf.Name),
			Category:     cat,
			Tier:         tier,
			Blocking:     gf.Blocking,
			Triggers:     gf.Triggers,
			Local:        local,
			CI:           CI{Workflow: strings.TrimSpace(gf.CI.Workflow), Job: strings.TrimSpace(gf.CI.Job)},
			Requirements: reqs,
			CIOnlyReason: ciOnlyReason,
			HookID:       strings.TrimSpace(gf.HookID),
		})
	}

	// Parse hygiene_hooks (back-compatible: absent list is valid).
	for i, hf := range parsed.HygieneHooks {
		id := strings.TrimSpace(hf.ID)
		if id == "" {
			return nil, fmt.Errorf("ci-gates registry %s: hygiene_hooks[%d] has blank id", path, i)
		}
		reg.HygieneHooks = append(reg.HygieneHooks, HygieneHook{
			ID:     id,
			Reason: strings.TrimSpace(hf.Reason),
		})
	}

	// Parse non_gate_workflows (back-compatible: absent list is valid).
	for i, nf := range parsed.NonGateWorkflows {
		file := strings.TrimSpace(nf.File)
		if file == "" {
			return nil, fmt.Errorf("ci-gates registry %s: non_gate_workflows[%d] has blank file", path, i)
		}
		reg.NonGateWorkflows = append(reg.NonGateWorkflows, NonGateWorkflow{
			File:   file,
			Reason: strings.TrimSpace(nf.Reason),
		})
	}

	return reg, nil
}
