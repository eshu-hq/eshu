// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const collectorManifestRel = "go/internal/collector/entrypoints/collector_entrypoints.yaml"

func loadRealRegistry(t *testing.T) (string, *Registry) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return root, reg
}

// withEntrypointsGate returns a copy of reg whose collector-entrypoints-generated
// gate has mutate applied, leaving every other gate untouched.
func withEntrypointsGate(t *testing.T, reg *Registry, mutate func(*Gate)) *Registry {
	t.Helper()
	out := *reg
	out.Gates = append([]Gate(nil), reg.Gates...)
	for i := range out.Gates {
		if out.Gates[i].ID == "collector-entrypoints-generated" {
			mutate(&out.Gates[i])
			return &out
		}
	}
	t.Fatal("collector-entrypoints-generated gate missing")
	return nil
}

// writeManifestWithSecondCollector writes, under a temporary repo root, the
// committed manifest plus a second collector copied from the first with a
// different command directory. It is the future "add collector #2" edit.
func writeManifestWithSecondCollector(t *testing.T, realRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(realRoot, collectorManifestRel))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	const head = "collectors:\n"
	idx := strings.Index(text, head)
	if idx < 0 {
		t.Fatal("manifest has no collectors: key")
	}
	second := strings.ReplaceAll(text[idx+len(head):], "collector-pagerduty", "collector-zzprobe")
	second = strings.ReplaceAll(second, "collector_pagerduty", "collector_zzprobe")
	tmp := t.TempDir()
	dest := filepath.Join(tmp, collectorManifestRel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(text+second), 0o600); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// TestCheckGeneratorOutputCoverage_CleanTreePasses is the GREEN half: the
// committed registry covers every file the committed manifest generates.
func TestCheckGeneratorOutputCoverage_CleanTreePasses(t *testing.T) {
	root, reg := loadRealRegistry(t)
	for _, err := range checkGeneratorOutputCoverage(root, reg) {
		t.Errorf("clean tree generator output coverage error: %v", err)
	}
}

// TestCheckGeneratorOutputCoverage_SecondCollectorWithoutTriggerFails is the
// #10139 review F1 seeded violation: a manifest that gains a collector whose
// command directory no trigger covers must fail the production check, naming
// the new collector's files and not the covered one.
func TestCheckGeneratorOutputCoverage_SecondCollectorWithoutTriggerFails(t *testing.T) {
	realRoot, reg := loadRealRegistry(t)
	tmp := writeManifestWithSecondCollector(t, realRoot)
	errs := checkGeneratorOutputCoverage(tmp, reg)
	if len(errs) != 3 {
		t.Fatalf("checkGeneratorOutputCoverage = %v, want one error per generated file of the uncovered collector (3)", errs)
	}
	for _, err := range errs {
		msg := err.Error()
		if !strings.Contains(msg, "go/cmd/collector-zzprobe/") || strings.Contains(msg, "collector-pagerduty") {
			t.Errorf("error %q must name only the uncovered collector-zzprobe output", msg)
		}
		if !strings.Contains(msg, "collector-entrypoints-generated") {
			t.Errorf("error %q must name the gate", msg)
		}
	}
}

// TestDriftCheck_SeededGeneratorOutputGapFails runs the whole production
// DriftCheck, the path `ci-gates validate` takes, against the committed
// registry with the gate's output triggers removed. It proves both the check
// and its wiring into DriftCheck.
func TestDriftCheck_SeededGeneratorOutputGapFails(t *testing.T) {
	root, reg := loadRealRegistry(t)
	seeded := withEntrypointsGate(t, reg, func(g *Gate) {
		kept := g.Triggers[:0:0]
		for _, trigger := range g.Triggers {
			if !strings.HasPrefix(trigger, "go/cmd/collector-") {
				kept = append(kept, trigger)
			}
		}
		g.Triggers = kept
	})
	var found []string
	for _, err := range DriftCheck(root, seeded) {
		if strings.Contains(err.Error(), "generated collector output") {
			found = append(found, err.Error())
		}
	}
	if len(found) != 3 {
		t.Fatalf("seeded DriftCheck generated-output errors = %v, want exactly 3 (config.go, main.go, service.go)", found)
	}
}

// TestCheckGeneratorOutputCoverage_MissingManifestFails keeps an unreadable
// manifest from reading as "nothing generated": the gate is registered, so the
// check must be able to derive its output set.
func TestCheckGeneratorOutputCoverage_MissingManifestFails(t *testing.T) {
	_, reg := loadRealRegistry(t)
	if errs := checkGeneratorOutputCoverage(t.TempDir(), reg); len(errs) != 1 {
		t.Fatalf("checkGeneratorOutputCoverage with no manifest = %v, want exactly one error", errs)
	}
}

// TestCheckGeneratorOutputCoverage_RegistryWithoutGateIsSkipped keeps
// hermetic fixture registries that never register the gate from needing a
// manifest.
func TestCheckGeneratorOutputCoverage_RegistryWithoutGateIsSkipped(t *testing.T) {
	reg := &Registry{Gates: []Gate{{ID: "unrelated", Triggers: []string{"x/**"}}}}
	if errs := checkGeneratorOutputCoverage(t.TempDir(), reg); len(errs) != 0 {
		t.Fatalf("checkGeneratorOutputCoverage = %v, want none for a registry without the gate", errs)
	}
}

// TestCollectorManifestEditSelectsRegistryGate proves the local-selection half
// of the fix with the real selector: a PR touching only the manifest or the
// generator that decides the output set selects ci-gate-registry, so the
// coverage check runs in the local floor instead of first in CI go-core.
func TestCollectorManifestEditSelectsRegistryGate(t *testing.T) {
	_, reg := loadRealRegistry(t)
	for _, changed := range []string{
		collectorManifestRel,
		"go/internal/collector/entrypoints/generate.go",
	} {
		selected := false
		for _, sel := range reg.Select([]string{changed}, TierPrePR) {
			if sel.Gate.ID == "ci-gate-registry" {
				selected = sel.Selected
			}
		}
		if !selected {
			t.Errorf("a change to only %s does not select ci-gate-registry; "+
				"its coverage check would first fail in CI", changed)
		}
	}
}
