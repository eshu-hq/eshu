// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path/filepath"
	"strings"
	"testing"
)

// seedReadinessGate returns a copy of the live-readiness-registration-drift
// gate with the go inlet trigger removed: the #9588 shape, where a PR
// changing only a live test's marker no longer selects the drift gate.
func seedReadinessGate(t *testing.T, reg *Registry) Gate {
	t.Helper()
	for _, g := range reg.Gates {
		if g.ID != "live-readiness-registration-drift" {
			continue
		}
		seeded := g
		kept := g.Triggers[:0:0]
		for _, trigger := range g.Triggers {
			if trigger == "go/**/*.go" {
				continue
			}
			kept = append(kept, trigger)
		}
		if len(kept) == len(g.Triggers) {
			t.Fatal("seed expects the go inlet trigger to exist on the clean tree")
		}
		seeded.Triggers = kept
		return seeded
	}
	t.Fatal("live-readiness-registration-drift gate missing")
	return Gate{}
}

func TestParseGeneratorInputs_TrimsAndRejectsBlanks(t *testing.T) {
	got, err := parseGeneratorInputs("registry", "gate", []string{"  go/**/*.go  ", "specs/ci-gates.d/**"})
	if err != nil {
		t.Fatalf("parseGeneratorInputs = %v, want nil", err)
	}
	if len(got) != 2 || got[0] != "go/**/*.go" || got[1] != "specs/ci-gates.d/**" {
		t.Fatalf("parseGeneratorInputs = %q, want trimmed entries", got)
	}
	if _, err := parseGeneratorInputs("registry", "gate", []string{"go/**/*.go", "  "}); err == nil {
		t.Fatal("parseGeneratorInputs with a blank entry = nil, want error")
	}
	if _, err := parseGeneratorInputs("registry", "gate", []string{"go/**/*.go", "go/**/*.go"}); err == nil {
		t.Fatal("parseGeneratorInputs with a duplicate entry = nil, want error")
	}
}

func TestCheckGeneratorInputCoverage_SeededGapFails(t *testing.T) {
	reg := &Registry{Gates: []Gate{{
		ID:              "live-readiness-registration-drift",
		Triggers:        []string{"scripts/generate-live-postgres-readiness.py"},
		GeneratorInputs: []string{"go/**/*_test.go"},
	}}}
	errs := checkGeneratorInputCoverage("testdata", reg, nil)
	if len(errs) != 1 {
		t.Fatalf("checkGeneratorInputCoverage = %v, want exactly one error", errs)
	}
	if !strings.Contains(errs[0].Error(), "go/**/*_test.go") {
		t.Fatalf("checkGeneratorInputCoverage error = %v, want it to name the uncovered input class", errs[0])
	}
}

func TestCheckGeneratorInputCoverage_RegenerateWithoutInputsFails(t *testing.T) {
	reg := &Registry{Gates: []Gate{{
		ID:         "some-regen-gate",
		Triggers:   []string{"scripts/gen.sh"},
		Regenerate: map[string]string{"out.gen": "bash scripts/gen.sh"},
	}}}
	errs := checkGeneratorInputCoverage("testdata", reg, nil)
	if len(errs) != 1 {
		t.Fatalf("checkGeneratorInputCoverage = %v, want exactly one error", errs)
	}
	if !strings.Contains(errs[0].Error(), "generator_inputs") {
		t.Fatalf("checkGeneratorInputCoverage error = %v, want it to demand generator_inputs", errs[0])
	}
}

// TestDriftCheck_GeneratorInputCoverageClean proves the subcheck passes on
// the committed tree with the #9621 declarations in place.
func TestDriftCheck_GeneratorInputCoverageClean(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tracked, err := loadTrackedPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range checkGeneratorInputCoverage(root, reg, tracked) {
		t.Errorf("clean tree generator input coverage error: %v", err)
	}
}

// TestDriftCheck_SeededGeneratorInputGapFails shapes #9588 through the full
// DriftCheck: the readiness generator's go inlet trigger removed, so a PR
// changing only a live test's marker no longer selects the drift gate. The
// run must fail naming the uncovered input class.
func TestDriftCheck_SeededGeneratorInputGapFails(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	seeded := *reg
	seeded.Gates = append([]Gate(nil), reg.Gates...)
	replaced := false
	for i := range seeded.Gates {
		if seeded.Gates[i].ID == "live-readiness-registration-drift" {
			seeded.Gates[i] = seedReadinessGate(t, reg)
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("live-readiness-registration-drift gate missing")
	}
	errs := DriftCheck(root, &seeded)
	var found []string
	for _, err := range errs {
		if strings.Contains(err.Error(), "generator input") {
			found = append(found, err.Error())
		}
	}
	// The removed trigger was the only cover for both declared inputs.
	if len(found) != 2 {
		t.Fatalf("seeded DriftCheck generator input errors = %v, want exactly two (one per uncovered input)", errs)
	}
	matchedTest := false
	for _, msg := range found {
		if !strings.Contains(msg, "live-readiness-registration-drift") {
			t.Fatalf("seeded DriftCheck error = %v, want it to name the gate", msg)
		}
		matchedTest = matchedTest || strings.Contains(msg, "go/**/*_test.go")
	}
	if !matchedTest {
		t.Fatalf("seeded DriftCheck errors = %v, want one naming the #9588 input class go/**/*_test.go", found)
	}
}
