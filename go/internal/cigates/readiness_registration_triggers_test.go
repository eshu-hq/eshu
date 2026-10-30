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
// The ground truth comes from the generator itself: every pathlib
// glob/rglob call in the Python files the gate lists as literal triggers.
// #9621 declares those classes once, machine-readably, in the gate's
// generator_inputs; this test asserts every parsed class is covered by a
// declared input, and then asserts, through the real selector, that every
// declared input selects the gate. Adding a generator scan without declaring
// it fails the first half; removing or narrowing the covering trigger fails
// the second (and the registry drift check).
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

	// Ground truth is the scan module alone. The driver script
	// (generate-live-postgres-readiness.py) also globs, but only for
	// orphan OUTPUT sweeps over generated inventory fragments — outputs,
	// not input classes — so parsing every .py trigger would demand
	// declaring generated files as inputs of their own gate.
	samples := map[string]string{}
	source, err := os.ReadFile(filepath.Join(root, "scripts/lib/live_postgres_readiness_gen_scan.py"))
	if err != nil {
		t.Fatalf("read generator scan module: %v", err)
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
	// The scan must find the marker inputs, or a parse change would pass
	// the test vacuously.
	for _, required := range []string{"go/**/*_live_test.go", "go/**/*_test.go"} {
		if _, ok := samples[required]; !ok {
			t.Fatalf("generator scan found no %s input class; found %v", required, samples)
		}
	}

	// Every class the generator actually scans must be covered by a
	// declared generator_inputs entry; the drift check then proves each
	// declared entry is trigger-covered. Coverage is probed one sample
	// path per class through MatchGlob — the same primitive the drift
	// check uses — so a declared entry covers a scanned class exactly
	// when it matches that class's sample. A declared entry containing
	// the literal text "sample" would probe true for any class and must
	// never be declared.
	for class := range samples {
		covered := false
		for _, declared := range gate.GeneratorInputs {
			if MatchGlob(declared, sampleForInputClass(class)) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("generator scans input class %s, which no declared generator_inputs entry covers (declared: %v); declare it on gate %s",
				class, gate.GeneratorInputs, gateID)
		}
	}
	if len(gate.GeneratorInputs) == 0 {
		t.Fatalf("gate %s declares no generator_inputs; the scanned classes above have no declared source", gateID)
	}

	for _, declared := range gate.GeneratorInputs {
		sample := sampleForInputClass(declared)
		selected := false
		for _, sel := range registry.Select([]string{sample}, TierPrePR) {
			if sel.Gate.ID == gateID {
				selected = sel.Selected
				if !selected {
					t.Errorf("declared generator input %s: changed path %s does not select %s (%s)",
						declared, sample, gateID, sel.Reason)
				}
			}
		}
		if selected {
			t.Logf("declared input %s: %s selects %s", declared, sample, gateID)
		}
	}
}
