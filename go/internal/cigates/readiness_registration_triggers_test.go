// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// generatorGlobRE captures a pathlib scan in the readiness generator:
// root.joinpath("<dir>").glob("<pattern>") or .rglob("<pattern>"), with the
// pattern allowed on the next line.
var generatorGlobRE = regexp.MustCompile(`joinpath\("([^"]+)"\)\.(r?glob)\(\s*"([^"]+)"`)

// TestReadinessRegistrationDriftTriggersCoverGeneratorInputs pins #9614:
// live-readiness-registration-drift regenerates the readiness ledger from
// files its generator scans, so every class of file the generator reads must
// select the gate. #9588 changed only a live test's readiness marker, the
// gate's triggers named only the generator and the ledger files, the PR plan
// skipped the gate, and main went red on the merge push.
//
// The input classes come from the generator itself: every pathlib
// glob/rglob call in the Python files the gate lists as literal triggers.
// For each class the test builds a sample path and asserts, through the
// real selector, that the path selects the gate. Removing or narrowing the
// covering trigger fails the test.
func TestReadinessRegistrationDriftTriggersCoverGeneratorInputs(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	registry, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const gateID = "live-readiness-registration-drift"
	var gate *Gate
	for i := range registry.Gates {
		if registry.Gates[i].ID == gateID {
			gate = &registry.Gates[i]
		}
	}
	if gate == nil {
		t.Fatalf("gate %s missing", gateID)
	}

	samples := map[string]string{}
	for _, trigger := range gate.Triggers {
		if !strings.HasSuffix(trigger, ".py") || strings.ContainsAny(trigger, "*?[") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, trigger))
		if err != nil {
			t.Fatalf("read generator file %s: %v", trigger, err)
		}
		for _, m := range generatorGlobRE.FindAllStringSubmatch(string(source), -1) {
			dir, kind, pattern := m[1], m[2], m[3]
			name := strings.ReplaceAll(pattern, "*", "sample")
			class := dir + "/" + pattern
			sample := dir + "/" + name
			if kind == "rglob" {
				class = dir + "/**/" + pattern
				sample = dir + "/internal/sample/" + name
			}
			samples[class] = sample
		}
	}
	// The scan must find the marker inputs, or a parse change would pass
	// the test vacuously.
	for _, required := range []string{"go/**/*_live_test.go", "go/**/*_test.go"} {
		if _, ok := samples[required]; !ok {
			t.Fatalf("generator scan found no %s input class; found %v", required, samples)
		}
	}

	for class, sample := range samples {
		selected := false
		for _, sel := range registry.Select([]string{sample}, TierPrePR) {
			if sel.Gate.ID == gateID {
				selected = sel.Selected
				if !selected {
					t.Errorf("generator input class %s: changed path %s does not select %s (%s)",
						class, sample, gateID, sel.Reason)
				}
			}
		}
		if selected {
			t.Logf("input class %s: %s selects %s", class, sample, gateID)
		}
	}
}
