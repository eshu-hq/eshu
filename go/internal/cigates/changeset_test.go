// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path/filepath"
	"testing"
)

func TestIsShellScriptPath(t *testing.T) {
	t.Parallel()
	shell := []string{
		"scripts/dev/pre-push.sh",
		"scripts/lib/go-toolchain.sh",
		"go/internal/collector/repo/git/testdata/content_version/deploy.sh",
		"run.bash",
	}
	for _, path := range shell {
		if !IsShellScriptPath(path) {
			t.Errorf("IsShellScriptPath(%q) = false, want true", path)
		}
	}
	notShell := []string{
		"",
		"scripts/",
		"scripts/lib/go-toolchain.sh.bak",
		"go/cmd/ci-gates/main.go",
		"specs/ci-images.v1.json",
		"README.md",
	}
	for _, path := range notShell {
		if IsShellScriptPath(path) {
			t.Errorf("IsShellScriptPath(%q) = true, want false", path)
		}
	}
}

func TestIsJSONConfigPath(t *testing.T) {
	t.Parallel()
	config := []string{
		"specs/ci-images.v1.json",
		"scripts/lib/ifa_scheduled_inventory.json",
		"docker-compose.yaml",
		".github/workflows/test.yml",
		"config.toml",
	}
	for _, path := range config {
		if !IsJSONConfigPath(path) {
			t.Errorf("IsJSONConfigPath(%q) = false, want true", path)
		}
	}
	notConfig := []string{
		"",
		"configs/",
		"specs/ci-images.v1.json.bak",
		"go/cmd/ci-gates/main.go",
		"scripts/dev/pre-push.sh",
		"README.md",
	}
	for _, path := range notConfig {
		if IsJSONConfigPath(path) {
			t.Errorf("IsJSONConfigPath(%q) = true, want false", path)
		}
	}
}

func TestAllShellChanged(t *testing.T) {
	t.Parallel()
	if allShellChanged(nil) || allShellChanged([]string{}) {
		t.Error("allShellChanged must fail closed on an empty path list")
	}
	if !allShellChanged([]string{"scripts/dev/pre-push.sh", "go/internal/x/deploy.sh"}) {
		t.Error("allShellChanged(shell-only) = false, want true")
	}
	if allShellChanged([]string{"scripts/dev/pre-push.sh", "go/cmd/ci-gates/main.go"}) {
		t.Error("allShellChanged(mixed shell+code) = true, want false")
	}
	if allShellChanged([]string{"specs/ci-images.v1.json"}) {
		t.Error("allShellChanged(json-only) = true, want false")
	}
}

func TestAllJSONConfigChanged(t *testing.T) {
	t.Parallel()
	if allJSONConfigChanged(nil) || allJSONConfigChanged([]string{}) {
		t.Error("allJSONConfigChanged must fail closed on an empty path list")
	}
	if !allJSONConfigChanged([]string{"specs/ci-images.v1.json", "docker-compose.yaml"}) {
		t.Error("allJSONConfigChanged(json+yaml) = false, want true")
	}
	if allJSONConfigChanged([]string{"specs/ci-images.v1.json", "go/cmd/ci-gates/main.go"}) {
		t.Error("allJSONConfigChanged(mixed config+code) = true, want false")
	}
	if allJSONConfigChanged([]string{"scripts/dev/pre-push.sh"}) {
		t.Error("allJSONConfigChanged(shell-only) = true, want false")
	}
	// A lockstep test input stays code even when it is JSON-shaped: editing
	// it alone must still run the asserting test's lanes.
	if allJSONConfigChanged([]string{"specs/remote-validation-inventory.generated.json"}) {
		t.Error("allJSONConfigChanged(lockstep JSON) = true, want false")
	}
}

// goLaneGate builds a minimal Go compile/lint gate in the shape the registry
// gives go-build, go-lint, go-vet, and go-fmt: test.yml/CI-owned with triggers.
func goLaneGate(id string, job string, triggers ...string) Gate {
	return Gate{
		ID:       id,
		Blocking: true,
		Tier:     TierPrePush,
		Triggers: triggers,
		Local:    &Local{Command: "true"},
		CI:       CI{Workflow: "test.yml", Job: job},
	}
}

func TestShellOnlySkipsGoCompileLanes(t *testing.T) {
	t.Parallel()
	// A shell script under go/ matches the blanket go/** trigger, so today it
	// selects the compile/lint lanes even though no Go source changed.
	changed := []string{"go/internal/collector/repo/git/testdata/content_version/deploy.sh"}
	for _, gate := range []Gate{
		goLaneGate("go-build", "go-core", "go/**"),
		goLaneGate("go-vet", "go-core", "go/**"),
		goLaneGate("go-fmt", "go-core", "go/**"),
		goLaneGate("go-lint", "go-core", "go/**"),
	} {
		if sel := selectGate(gate, changed, TierPrePR); sel.Selected {
			t.Errorf("shell-only %v must not select %q: %s", changed, gate.ID, sel.Reason)
		}
	}
}

func TestShellOnlyKeepsProvenConsumerLanes(t *testing.T) {
	t.Parallel()
	// go-fmt and go-lint name their own implementation scripts as literal
	// triggers: editing the lane's script is a proven-consumer change and
	// must keep selecting the lane (checkScriptTriggerCoverage demands the
	// trigger; this test demands the selection).
	changed := []string{"scripts/dev/precommit-go.sh"}
	for _, gate := range []Gate{
		goLaneGate("go-fmt", "go-core", "go/**", "scripts/dev/precommit-go.sh"),
		goLaneGate("go-lint", "go-core", "go/**", "scripts/dev/precommit-go.sh", "scripts/lib/go-toolchain.sh"),
	} {
		if sel := selectGate(gate, changed, TierPrePR); !sel.Selected {
			t.Errorf("lane-implementation change %v must keep selecting %q: %s", changed, gate.ID, sel.Reason)
		}
	}
	// A mixed shell+code change is not shell-only: the lanes stay.
	mixed := []string{"scripts/dev/precommit-go.sh", "go/cmd/ci-gates/main.go"}
	if sel := selectGate(goLaneGate("go-build", "go-core", "go/**"), mixed, TierPrePR); !sel.Selected {
		t.Errorf("mixed shell+code must keep selecting go-build: %s", sel.Reason)
	}
}

func TestJSONOnlySkipsGoCompileLanes(t *testing.T) {
	t.Parallel()
	// No committed JSON lives under go/ today, so the fixture is synthetic by
	// necessity: a JSON data file under a go/** blanket trigger. Its content
	// cannot affect compilation, vet, lint, or formatting of Go sources.
	changed := []string{"go/internal/replay/inputtape/testdata/corpus.json"}
	for _, gate := range []Gate{
		goLaneGate("go-build", "go-core", "go/**"),
		goLaneGate("go-vet", "go-core", "go/**"),
		goLaneGate("go-fmt", "go-core", "go/**"),
		goLaneGate("go-lint", "go-core", "go/**"),
	} {
		if sel := selectGate(gate, changed, TierPrePR); sel.Selected {
			t.Errorf("json-only %v must not select %q: %s", changed, gate.ID, sel.Reason)
		}
	}
}

func TestJSONOnlyKeepsProvenConsumerLanes(t *testing.T) {
	t.Parallel()
	// A specs JSON change selects only the gates whose triggers name it: the
	// Go compile lanes stay out while consumer lanes keep matching.
	changed := []string{"specs/ci-images.v1.json"}
	if sel := selectGate(goLaneGate("go-build", "go-core", "go/**"), changed, TierPrePR); sel.Selected {
		t.Errorf("json-only %v must not select go-build: %s", changed, sel.Reason)
	}
	consumer := Gate{
		ID:       "query-plan-regression",
		Blocking: true,
		Tier:     TierPrePR,
		Triggers: []string{"specs/ci-images.v1.json", "go/internal/query/**"},
		Local:    &Local{Command: "true"},
		CI:       CI{Workflow: "test.yml", Job: "go-core"},
	}
	if sel := selectGate(consumer, changed, TierPrePR); !sel.Selected {
		t.Errorf("json-only %v must keep selecting its consumer lane: %s", changed, sel.Reason)
	}
}

func TestLaneConfigStaysCodeClass(t *testing.T) {
	t.Parallel()
	// go/.golangci.yml configures the go-lint verdict through the lane
	// itself, not through any literal trigger (go-lint lists only go/** plus
	// script literals). A lone edit must keep selecting the lanes it can
	// affect, or a lint-config change would skip its own gate (#10992 P2).
	changed := []string{"go/.golangci.yml"}
	for _, gate := range []Gate{
		goLaneGate("go-lint", "go-core", "go/**", "scripts/dev/precommit-go.sh"),
		goLaneGate("go-fmt", "go-core", "go/**", "scripts/dev/precommit-go.sh"),
	} {
		if sel := selectGate(gate, changed, TierPrePR); !sel.Selected {
			t.Errorf("lane-config %v must keep selecting %q: %s", changed, gate.ID, sel.Reason)
		}
	}
}

// TestGoLaneGateIDsPinnedToRegistry pins the shell/JSON skip set against the
// committed registry: every skipped ID must exist, block, and live in the
// test.yml workflow the skip reason names. A renamed or moved lane fails here
// instead of silently leaving (or entering) the skip set.
func TestGoLaneGateIDsPinnedToRegistry(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	reg, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]Gate, len(reg.Gates))
	for _, gate := range reg.Gates {
		byID[gate.ID] = gate
	}
	if len(goLaneGateIDs) == 0 {
		t.Fatal("goLaneGateIDs names no lanes")
	}
	for id := range goLaneGateIDs {
		gate, ok := byID[id]
		if !ok {
			t.Errorf("skip-set gate %q is not in the registry", id)
			continue
		}
		if !gate.Blocking {
			t.Errorf("skip-set gate %q is not blocking", id)
		}
		if gate.CI.Workflow != "test.yml" {
			t.Errorf("skip-set gate %q lives in %q, want test.yml", id, gate.CI.Workflow)
		}
	}
}
