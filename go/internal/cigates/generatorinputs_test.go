// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedCapabilitySurfaceGate returns a copy of the capability-surface-inventory
// gate with the console inlet trigger removed: the #9655 shape, where a PR
// changing only the console router no longer selects the drift gate even
// though the generator enumerates console pages from that router.
func seedCapabilitySurfaceGate(t *testing.T, reg *Registry) Gate {
	t.Helper()
	for _, g := range reg.Gates {
		if g.ID != "capability-surface-inventory" {
			continue
		}
		seeded := g
		kept := g.Triggers[:0:0]
		for _, trigger := range g.Triggers {
			if trigger == "apps/console/src/**" {
				continue
			}
			kept = append(kept, trigger)
		}
		if len(kept) == len(g.Triggers) {
			t.Fatal("seed expects the console inlet trigger to exist on the clean tree")
		}
		seeded.Triggers = kept
		return seeded
	}
	t.Fatal("capability-surface-inventory gate missing")
	return Gate{}
}

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

// TestDriftCheck_SeededCapabilitySurfaceGapFails shapes #9655 through the full
// DriftCheck: the console inlet trigger removed, so a PR changing only the
// console router no longer selects the drift gate. The run must fail naming
// the uncovered console input class.
func TestDriftCheck_SeededCapabilitySurfaceGapFails(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	seeded := *reg
	seeded.Gates = append([]Gate(nil), reg.Gates...)
	replaced := false
	for i := range seeded.Gates {
		if seeded.Gates[i].ID == "capability-surface-inventory" {
			seeded.Gates[i] = seedCapabilitySurfaceGate(t, reg)
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("capability-surface-inventory gate missing")
	}
	errs := DriftCheck(root, &seeded)
	var found []string
	for _, err := range errs {
		if strings.Contains(err.Error(), "generator input") {
			found = append(found, err.Error())
		}
	}
	if len(found) == 0 {
		t.Fatalf("seeded DriftCheck generator input errors = %v, want at least one naming the uncovered console class", errs)
	}
	matchedConsole := false
	for _, msg := range found {
		if !strings.Contains(msg, "capability-surface-inventory") {
			t.Fatalf("seeded DriftCheck error = %v, want it to name the gate", msg)
		}
		matchedConsole = matchedConsole || strings.Contains(msg, "apps/console/src/App.tsx")
	}
	if !matchedConsole {
		t.Fatalf("seeded DriftCheck errors = %v, want one naming the #9655 console input class apps/console/src/App.tsx", found)
	}
}

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

// syntheticCollectorManifest is a minimal valid entrypoint manifest with one
// collector. The committed manifest is empty since #10619 removed the last
// generated collector, so seeded-violation tests build their collector from
// this fixture instead of copying a committed entry. It mirrors the shape
// manifest_test.go accepts (test scope kind, no auth).
const syntheticCollectorManifest = `schema_version: 1
collectors:
  - command_dir: go/cmd/collector-zzprobe
    runtime_name: collector-zzprobe
    binary_name: eshu-collector-zzprobe
    collector_label: zzprobe collector
    go_name: Zzprobe
    env:
      collector_instances: ESHU_COLLECTOR_INSTANCES_JSON
      instance_id: ESHU_ZZPROBE_COLLECTOR_INSTANCE_ID
      poll_interval: ESHU_ZZPROBE_POLL_INTERVAL
      claim_lease_ttl: ESHU_ZZPROBE_CLAIM_LEASE_TTL
      heartbeat_interval: ESHU_ZZPROBE_HEARTBEAT_INTERVAL
      owner_id: ESHU_ZZPROBE_COLLECTOR_OWNER_ID
      owner_id_const_name: envOwnerID
    store_name: collector_zzprobe
    claim_id_prefix: zzprobe-claim
    collector_kind_expr: scope.CollectorKind("zzprobe")
    scope_kind: test
    auth_mode: none
    target_list_field: targets
    target_identity_fields: [scope_id]
    target_auth_fields: [none]
    source:
      import_path: github.com/eshu-hq/eshu/go/internal/collector/zzprobe
      package_name: zzprobe
      config_type: zzprobe.SourceConfig
      constructor: zzprobe.NewClaimedSource
      config_loader: loadZzprobeSourceConfig
      config_attacher: attachZzprobeRuntimeSignals
      runtime_config_type: zzprobeRuntimeConfiguration
`

// writeSyntheticCollectorManifest writes the synthetic collector manifest
// under a temporary repo root. It is the future "add collector #2" edit for
// checks that read only the manifest.
func writeSyntheticCollectorManifest(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	dest := filepath.Join(tmp, collectorManifestRel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(syntheticCollectorManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// mirrorRootWithManifest mirrors realRoot into a temporary directory,
// overriding the collector entrypoint manifest with manifest text. Ancestor
// directories of the manifest are real directories; every other entry is a
// symlink. Removing or writing through a top-level symlink would mutate the
// real tree, so the whole ancestor chain stays real. It lets DriftCheck run
// its full production path (hooks, workflows, gates) against a seeded
// manifest without mutating the worktree.
func mirrorRootWithManifest(t *testing.T, realRoot, manifest string) string {
	t.Helper()
	abs, err := filepath.Abs(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	rel := filepath.FromSlash(collectorManifestRel)
	// Real directories along the manifest's ancestor chain.
	dir := tmp
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mirror := func(real, shadow string, override string) {
		entries, err := os.ReadDir(real)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Name() == override {
				continue
			}
			if err := os.Symlink(filepath.Join(real, entry.Name()), filepath.Join(shadow, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Symlink siblings at each ancestor level, descending to the manifest.
	real := abs
	shadow := tmp
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))
	for _, part := range parts {
		mirror(real, shadow, part)
		real = filepath.Join(real, part)
		shadow = filepath.Join(shadow, part)
	}
	mirror(real, shadow, filepath.Base(rel))
	if err := os.WriteFile(filepath.Join(shadow, filepath.Base(rel)), []byte(manifest), 0o600); err != nil {
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
	_, reg := loadRealRegistry(t)
	tmp := writeSyntheticCollectorManifest(t)
	errs := checkGeneratorOutputCoverage(tmp, reg)
	if len(errs) != 3 {
		t.Fatalf("checkGeneratorOutputCoverage = %v, want one error per generated file of the uncovered collector (3)", errs)
	}
	for _, err := range errs {
		msg := err.Error()
		// The trigger list in the message legitimately names pagerduty's
		// directory, so assert on the reported output path, not the whole text.
		if !strings.Contains(msg, `output "go/cmd/collector-zzprobe/`) || strings.Contains(msg, `output "go/cmd/collector-pagerduty/`) {
			t.Errorf("error %q must report only an uncovered collector-zzprobe output", msg)
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
	realRoot, reg := loadRealRegistry(t)
	seeded := withEntrypointsGate(t, reg, func(g *Gate) {
		kept := g.Triggers[:0:0]
		for _, trigger := range g.Triggers {
			if !strings.HasPrefix(trigger, "go/cmd/collector-") {
				kept = append(kept, trigger)
			}
		}
		g.Triggers = kept
	})
	root := mirrorRootWithManifest(t, realRoot, syntheticCollectorManifest)
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
