// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// The value-flow expectation gate (#6192) is blocking and ci-heavy, which puts
// it in the one shape where a trigger mismatch fails silently and forever.
// RequiredGates selects it from the registry's triggers, GitHub starts it from
// the workflow's own pull_request paths, and nothing reconciles the two: a
// registry trigger the workflow does not mirror marks a gate that GitHub never
// starts, so required-gates-complete sits pending until the timeout with no red
// check anywhere to explain it.
//
// Select() cannot cover this. It refuses ci-heavy gates before it ever looks at
// a trigger ("tier ci-heavy is CI/manual-only — skipped in local lane"), so the
// local selector reports SKIPPED for every path including the ones that must
// select. RequiredGates is the function that actually decides, and it is what
// these tests drive.
const (
	valueFlowGateID       = "value-flow-conformance-expectation"
	valueFlowGateWorkflow = "value-flow-conformance-expectation.yml"
	valueFlowGateJob      = "Value flow conformance expectation"
)

// loadCommittedRegistry returns the real specs/ci-gates.v1.yaml, not a fixture.
// The property under test is about the committed pair of files; a fixture would
// pass while the shipped registry drifted.
func loadCommittedRegistry(t *testing.T) (*cigates.Registry, string) {
	t.Helper()
	repoRoot := filepath.Join("..", "..", "..")
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}
	return reg, repoRoot
}

// valueFlowGate returns the committed gate entry, failing if it has been
// removed. Removing it is a legitimate change — it is what happens when
// upstream lands the NornicDB fixes — but it takes the workflow and the scripts
// with it, so this test file goes at the same time.
func valueFlowGate(t *testing.T, reg *cigates.Registry) cigates.Gate {
	t.Helper()
	for _, gate := range reg.Gates {
		if gate.ID == valueFlowGateID {
			return gate
		}
	}
	t.Fatalf("registry has no gate %q", valueFlowGateID)
	return cigates.Gate{}
}

// workflowPullRequestPaths reads on.pull_request.paths out of a workflow file.
// It walks yaml.Node rather than unmarshalling into a struct because GitHub's
// `on:` key round-trips badly through Go struct tags.
func workflowPullRequestPaths(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- test-local path built from repo root
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	node := &document
	for _, key := range []string{"", "on", "pull_request", "paths"} {
		if key == "" {
			if len(node.Content) == 0 {
				t.Fatalf("%s: empty YAML document", path)
			}
			node = node.Content[0]
			continue
		}
		node = mappingChild(t, node, key, path)
	}
	if node.Kind != yaml.SequenceNode {
		t.Fatalf("%s: on.pull_request.paths is not a sequence", path)
	}
	paths := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		paths = append(paths, strings.TrimSpace(item.Value))
	}
	return paths
}

func mappingChild(t *testing.T, node *yaml.Node, key, path string) *yaml.Node {
	t.Helper()
	if node.Kind != yaml.MappingNode {
		t.Fatalf("%s: expected a mapping while looking for %q", path, key)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	t.Fatalf("%s: no %q key", path, key)
	return nil
}

// TestValueFlowExpectationRegistryAndWorkflowTriggersMatch is the lockstep
// check. Set equality in BOTH directions, because the two failure modes are
// different and both are bad: a registry trigger the workflow lacks hangs
// required-gates-complete forever, and a workflow path the registry lacks runs
// a 45-minute Docker job that nothing was waiting for.
func TestValueFlowExpectationRegistryAndWorkflowTriggersMatch(t *testing.T) {
	t.Parallel()

	reg, repoRoot := loadCommittedRegistry(t)
	gate := valueFlowGate(t, reg)

	registryTriggers := append([]string(nil), gate.Triggers...)
	workflowPaths := workflowPullRequestPaths(
		t,
		filepath.Join(repoRoot, ".github", "workflows", valueFlowGateWorkflow),
	)
	slices.Sort(registryTriggers)
	slices.Sort(workflowPaths)

	if !slices.Equal(registryTriggers, workflowPaths) {
		t.Errorf("registry triggers and workflow pull_request paths have drifted\n"+
			"  specs/ci-gates.v1.yaml %q\n"+
			"  .github/workflows/%s %q",
			registryTriggers, valueFlowGateWorkflow, workflowPaths)
	}
}

// TestValueFlowExpectationIsRequiredForItsOwnTriggers proves the gate is
// actually selected for every path it claims to watch, and — the half that
// makes the other half mean something — that it is NOT selected for a Go change
// outside its blast radius. Without the control, a gate triggering on "**"
// would pass the first assertion just as well.
func TestValueFlowExpectationIsRequiredForItsOwnTriggers(t *testing.T) {
	t.Parallel()

	reg, _ := loadCommittedRegistry(t)
	gate := valueFlowGate(t, reg)

	// One concrete file per trigger. A glob trigger needs a real path under it:
	// asserting on the glob's own text would prove the string is present, not
	// that anything selects through it.
	selecting := map[string]string{
		"go/internal/backendconformance/**":                             "go/internal/backendconformance/corpus_value_flow.go",
		"go/internal/reducer/valueflow/value_flow_cloud_sink_loader.go": "go/internal/reducer/valueflow/value_flow_cloud_sink_loader.go",
		"docker-compose.yaml":                                           "docker-compose.yaml",
		"docker-compose.neo4j.yml":                                      "docker-compose.neo4j.yml",
		"scripts/verify_backend_conformance_live.sh":                    "scripts/verify_backend_conformance_live.sh",
		"scripts/verify-value-flow-conformance-expectation.sh":          "scripts/verify-value-flow-conformance-expectation.sh",
		"scripts/test-verify-value-flow-conformance-expectation.sh":     "scripts/test-verify-value-flow-conformance-expectation.sh",
		"scripts/ci/install-apt-packages.sh":                            "scripts/ci/install-apt-packages.sh",
		".github/workflows/value-flow-conformance-expectation.yml":      ".github/workflows/value-flow-conformance-expectation.yml",
	}
	if len(selecting) != len(gate.Triggers) {
		t.Fatalf("this test covers %d trigger(s) but the gate declares %d: %q",
			len(selecting), len(gate.Triggers), gate.Triggers)
	}
	for _, trigger := range gate.Triggers {
		if _, ok := selecting[trigger]; !ok {
			t.Fatalf("trigger %q has no concrete path in this test; add one", trigger)
		}
	}

	for trigger, path := range selecting {
		t.Run(trigger, func(t *testing.T) {
			t.Parallel()
			if !requiresValueFlowGate(t, reg, path) {
				t.Errorf("%q did not select %s; required-gates would never wait for it",
					path, valueFlowGateID)
			}
		})
	}

	// The control. Ordinary Go work outside the value-flow surface must not pay
	// for two Docker stacks and a NornicDB source build.
	for _, control := range []string{
		"go/internal/query/handler.go",
		"docs/public/reference/backend-conformance.md",
	} {
		t.Run("control:"+control, func(t *testing.T) {
			t.Parallel()
			if requiresValueFlowGate(t, reg, control) {
				t.Errorf("%q selected %s; its triggers are wider than its blast radius",
					control, valueFlowGateID)
			}
		})
	}
}

// requiresValueFlowGate reports whether RequiredGates — the selector the
// required-gates aggregator actually runs — returns the value-flow job for the
// given changed path.
func requiresValueFlowGate(t *testing.T, reg *cigates.Registry, path string) bool {
	t.Helper()
	required, err := reg.RequiredGates([]string{path})
	if err != nil {
		t.Fatalf("RequiredGates(%q): %v", path, err)
	}
	for _, entry := range required {
		if entry.Workflow == valueFlowGateWorkflow && entry.Job == valueFlowGateJob {
			return true
		}
	}
	return false
}
