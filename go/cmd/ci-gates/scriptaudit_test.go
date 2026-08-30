// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestAuditScriptsSubcommandJSONReportsTrackedEvidence(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	root := t.TempDir()
	writeAuditCommandFixture(t, root, "scripts/gate.sh", "#!/usr/bin/env bash\nsource scripts/helper.sh\n")
	writeAuditCommandFixture(t, root, "scripts/gate-test.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, "scripts/helper.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, "scripts/manual.sh", "#!/usr/bin/env bash\n# Usage: ./scripts/manual.sh\n")
	writeAuditCommandFixture(t, root, "scripts/trigger-only.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, "scripts/untracked.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, ".github/workflows/test.yml", `jobs:
  verify:
    defaults:
      run:
        working-directory: go
    steps:
      - run: bash ../scripts/gate.sh
`)
	registry := writeAuditCommandFixture(t, root, "specs/ci-gates.v1.yaml", `version: v1
gates:
  - id: example-gate
    name: Example gate
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["scripts/gate.sh", "scripts/helper.sh", "scripts/trigger-only.sh"]
    local:
      command: "cd go && bash ../scripts/gate.sh"
      test_command: "bash scripts/gate-test.sh"
    ci:
      workflow: test.yml
      job: verify
    requirements: []
`)
	runGitForAuditCommand(t, root, "init", "-q")
	runGitForAuditCommand(t, root, "add", ".")
	runGitForAuditCommand(t, root, "reset", "-q", "scripts/untracked.sh")

	cmd := exec.Command(bin, "audit-scripts", "--registry", registry, "--repo-root", root, "--json")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("audit-scripts failed: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("audit-scripts failed: %v", err)
	}
	var report scriptAuditJSONOutput
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if report.SchemaVersion != "v1" {
		t.Errorf("schema version = %q, want v1", report.SchemaVersion)
	}
	if report.Summary.TrackedShellScripts != 5 || report.Summary.GateEntrypoints != 2 ||
		report.Summary.Referenced != 1 || report.Summary.Unreferenced != 2 {
		t.Errorf("summary = %+v, want tracked=5 gate=2 referenced=1 unreferenced=2", report.Summary)
	}
	if len(report.Scripts) != 5 {
		t.Fatalf("scripts = %d, want 5: %s", len(report.Scripts), out)
	}
	if report.Scripts[0].Path != "scripts/gate-test.sh" || report.Scripts[3].Path != "scripts/manual.sh" ||
		report.Scripts[4].Path != "scripts/trigger-only.sh" {
		t.Errorf("scripts are not path-sorted: %+v", report.Scripts)
	}
	manual := report.Scripts[3]
	if manual.Status != cigates.ScriptStatusUnreferenced {
		t.Errorf("manual status = %q, want %q", manual.Status, cigates.ScriptStatusUnreferenced)
	}
	if strings.Contains(string(out), "scripts/untracked.sh") {
		t.Errorf("untracked script leaked into report: %s", out)
	}
}

func TestAuditScriptsSubcommandTextFiltersRowsButKeepsFullTotals(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	root := t.TempDir()
	writeAuditCommandFixture(t, root, "scripts/gate.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, "scripts/manual.sh", "#!/usr/bin/env bash\n")
	writeAuditCommandFixture(t, root, ".github/workflows/test.yml", "jobs: {}\n")
	registry := writeAuditCommandFixture(t, root, "specs/ci-gates.v1.yaml", `version: v1
gates:
  - id: example-gate
    name: Example gate
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["scripts/gate.sh"]
    local:
      command: "bash scripts/gate.sh"
    ci:
      workflow: test.yml
      job: verify
    requirements: []
`)
	runGitForAuditCommand(t, root, "init", "-q")
	runGitForAuditCommand(t, root, "add", ".")

	cmd := exec.Command(bin, "audit-scripts", "--registry", registry, "--repo-root", root, "--unreferenced-only")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("audit-scripts failed: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("audit-scripts failed: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "ADVISORY: unreferenced means") {
		t.Errorf("text output lacks advisory banner: %s", text)
	}
	if !strings.Contains(text, "unreferenced\tscripts/manual.sh") {
		t.Errorf("text output lacks unreferenced script: %s", text)
	}
	if strings.Contains(text, "gate-entrypoint\tscripts/gate.sh") {
		t.Errorf("filtered text output includes gate entrypoint: %s", text)
	}
	if !strings.Contains(text, "TOTAL tracked=2 gate-entrypoint=1 referenced=0 unreferenced=1") {
		t.Errorf("text output lacks full-inventory totals: %s", text)
	}
}

func writeAuditCommandFixture(t *testing.T, root, path, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func runGitForAuditCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
