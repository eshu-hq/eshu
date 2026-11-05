// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/entrypoints"
)

const collectorEntrypointsGateID = "collector-entrypoints-generated"

// generatedCollectorOutputs derives every file the collector entrypoints
// generator writes from the committed manifest and the generator itself, the
// same two sources scripts/generate-collector-entrypoints.sh uses. Nothing
// here is a hand list of collector names.
func generatedCollectorOutputs(t *testing.T, root string) []string {
	t.Helper()
	manifests, err := entrypoints.LoadManifestFile(
		filepath.Join(root, "go", "internal", "collector", "entrypoints", "collector_entrypoints.yaml"))
	if err != nil {
		t.Fatalf("load collector entrypoints manifest: %v", err)
	}
	var outputs []string
	for _, manifest := range manifests {
		files, err := entrypoints.Generate(manifest)
		if err != nil {
			t.Fatalf("generate %s: %v", manifest.RuntimeName, err)
		}
		for _, file := range files {
			outputs = append(outputs, path.Join(manifest.CommandDir, file.Name))
		}
	}
	if len(outputs) == 0 {
		t.Fatal("collector entrypoints generator produced no output paths")
	}
	return outputs
}

func collectorEntrypointsGate(t *testing.T, reg *Registry) Gate {
	t.Helper()
	for _, g := range reg.Gates {
		if g.ID == collectorEntrypointsGateID {
			return g
		}
	}
	t.Fatalf("%s gate missing", collectorEntrypointsGateID)
	return Gate{}
}

// TestCollectorEntrypointsGate_SelectedByEveryGeneratedFile is the #10139
// seeded violation: a hand edit to only a generated go/cmd/collector-<name>
// file must select the drift gate locally, because CI verify-contracts fails
// on it ("generated file ... is stale").
func TestCollectorEntrypointsGate_SelectedByEveryGeneratedFile(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range generatedCollectorOutputs(t, root) {
		var selected bool
		for _, sel := range reg.Select([]string{output}, TierPrePR) {
			if sel.Gate.ID == collectorEntrypointsGateID {
				selected = sel.Selected
			}
		}
		if !selected {
			t.Errorf("a change to only generated file %s does not select %s; "+
				"add a trigger for its command directory", output, collectorEntrypointsGateID)
		}
	}
}

// TestCollectorEntrypointsGate_SeededMissingOutputTriggerIsDetected proves the
// output-coverage check is not vacuous: the same registry with the output
// triggers removed must report each generated file as unselected.
func TestCollectorEntrypointsGate_SeededMissingOutputTriggerIsDetected(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	seeded := collectorEntrypointsGate(t, reg)
	var kept []string
	for _, trigger := range seeded.Triggers {
		if strings.HasPrefix(trigger, "go/cmd/collector-") {
			continue
		}
		kept = append(kept, trigger)
	}
	for _, output := range generatedCollectorOutputs(t, root) {
		if anyTriggerMatches(kept, output) {
			t.Errorf("seeded gate without go/cmd/collector-* triggers still selects %s; "+
				"the output-coverage check would not notice a missing trigger", output)
		}
	}
}
