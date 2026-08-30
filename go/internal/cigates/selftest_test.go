// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSelfTestTriggers(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, []string{
		"scripts/verify-example.sh",
		"scripts/test-verify-example.sh",
	}, `
      command: "bash scripts/verify-example.sh"
      test_command: "bash scripts/test-verify-example.sh"`)

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"scripts/verify-example.sh", "scripts/test-verify-example.sh"}
	got := reg.Gates[0].SelfTestTriggers
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SelfTestTriggers = %v, want %v", got, want)
	}
}

func TestLoadRejectsEmptySelfTestTriggers(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, []string{}, `
      command: "bash scripts/verify-example.sh"
      test_command: "bash scripts/test-verify-example.sh"`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "self_test_triggers") {
		t.Fatalf("Load() error = %v, want self_test_triggers error", err)
	}
}

func TestLoadRejectsSelfTestTriggersWithoutTestCommand(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, []string{"scripts/verify-example.sh"}, `
      command: "bash scripts/verify-example.sh"`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "without a distinct test_command") {
		t.Fatalf("Load() error = %v, want missing test_command error", err)
	}
}

func TestLoadRejectsSelfTestTriggerOutsideGateTriggers(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, []string{"scripts/unwatched-test.sh"}, `
      command: "bash scripts/verify-example.sh"
      test_command: "bash scripts/test-verify-example.sh"`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must also appear in triggers") {
		t.Fatalf("Load() error = %v, want trigger-subset error", err)
	}
}

func TestLoadRejectsUnknownGateField(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, nil, `
      command: "bash scripts/verify-example.sh"`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	raw = []byte(strings.Replace(string(raw), "    local:", "    self_test_trigger: scripts/test-verify-example.sh\n    local:", 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "field self_test_trigger not found") {
		t.Fatalf("Load() error = %v, want unknown-field error", err)
	}
}

func TestGateShouldRunSelfTest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		gate    Gate
		changed []string
		want    bool
	}{
		{
			name:    "legacy declaration stays fail closed",
			gate:    Gate{Local: &Local{TestCommand: "test"}},
			changed: []string{"go/product.go"},
			want:    true,
		},
		{
			name:    "declared trigger matches",
			gate:    Gate{Local: &Local{TestCommand: "test"}, SelfTestTriggers: []string{"scripts/test-*.sh"}},
			changed: []string{"scripts/test-example.sh"},
			want:    true,
		},
		{
			name:    "product-only change skips verifier self-test",
			gate:    Gate{Local: &Local{TestCommand: "test"}, SelfTestTriggers: []string{"scripts/test-*.sh"}},
			changed: []string{"go/product.go"},
			want:    false,
		},
		{
			name:    "no self-test command",
			gate:    Gate{Local: &Local{Command: "verify"}},
			changed: []string{"scripts/test-example.sh"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.gate.ShouldRunSelfTest(tt.changed); got != tt.want {
				t.Fatalf("ShouldRunSelfTest(%v) = %v, want %v", tt.changed, got, tt.want)
			}
		})
	}
}

func writeSelfTestRegistry(t *testing.T, selfTestTriggers []string, localBlock string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "registry.yaml")
	selfTestBlock := ""
	if selfTestTriggers != nil {
		selfTestBlock = "    self_test_triggers:\n"
		if len(selfTestTriggers) == 0 {
			selfTestBlock = "    self_test_triggers: []\n"
		}
		for _, trigger := range selfTestTriggers {
			selfTestBlock += "      - " + trigger + "\n"
		}
	}
	raw := `version: v1
gates:
  - id: example
    name: Example
    category: hygiene
    tier: pre-pr
    blocking: true
    triggers:
      - go/**
      - scripts/verify-example.sh
      - scripts/test-verify-example.sh
` + selfTestBlock + `    local:` + localBlock + `
    ci:
      workflow: test.yml
      job: test
    requirements: []
    ci_only_reason: ""
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return path
}
