// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAuditScripts_ReportsEvidenceWithoutCallingUnreferencedScriptsOrphans(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	writeAuditFixture(t, repoRoot, "scripts/gate.sh", "#!/usr/bin/env bash\nsource scripts/helper.sh\ncommon=$REPO_ROOT/scripts/computed.sh\n")
	writeAuditFixture(t, repoRoot, "scripts/helper.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/computed.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/gate-test.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/manual.sh", "#!/usr/bin/env bash\n# Usage: scripts/manual.sh\n")
	writeAuditFixture(t, repoRoot, "scripts/trigger-only.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/near-match.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/url-only.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/doc-only.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "docs/runbook.md", "Run `./scripts/doc-only.sh`, not `scripts/near-match.sh.bak` or `https://host/scripts/url-only.sh`.\n")
	writeAuditFixture(t, repoRoot, ".github/workflows/test.yml", `jobs:
  test:
    defaults:
      run:
        working-directory: go
    steps:
      - run: bash ../scripts/gate.sh
`)
	gitAuditFixture(t, repoRoot)

	reg := &Registry{Gates: []Gate{{
		ID:       "example-gate",
		Triggers: []string{"scripts/gate.sh", "scripts/helper.sh", "scripts/trigger-only.sh"},
		Local: &Local{
			Command:     "cd go && bash ../scripts/gate.sh",
			TestCommand: "bash scripts/gate-test.sh",
		},
		CI: CI{Workflow: "test.yml", Job: "test"},
	}}}

	got, err := AuditScripts(repoRoot, reg)
	if err != nil {
		t.Fatalf("AuditScripts() error = %v", err)
	}
	byPath := make(map[string]ScriptAudit, len(got))
	for _, entry := range got {
		byPath[entry.Path] = entry
	}

	gate := byPath["scripts/gate.sh"]
	if gate.Status != ScriptStatusGateEntrypoint {
		t.Errorf("gate status = %q, want %q", gate.Status, ScriptStatusGateEntrypoint)
	}
	wantCommand := []GateCommandEvidence{{GateID: "example-gate", Field: "local.command"}}
	if !reflect.DeepEqual(gate.GateCommands, wantCommand) {
		t.Errorf("gate commands = %v, want %v", gate.GateCommands, wantCommand)
	}
	wantRun := []WorkflowRunEvidence{{Workflow: ".github/workflows/test.yml", Job: "test"}}
	if !reflect.DeepEqual(gate.WorkflowRuns, wantRun) {
		t.Errorf("gate workflow runs = %v, want %v", gate.WorkflowRuns, wantRun)
	}
	testHarness := byPath["scripts/gate-test.sh"]
	wantTestCommand := []GateCommandEvidence{{GateID: "example-gate", Field: "local.test_command"}}
	if !reflect.DeepEqual(testHarness.GateCommands, wantTestCommand) {
		t.Errorf("test commands = %v, want %v", testHarness.GateCommands, wantTestCommand)
	}

	helper := byPath["scripts/helper.sh"]
	if helper.Status != ScriptStatusReferenced {
		t.Errorf("helper status = %q, want %q", helper.Status, ScriptStatusReferenced)
	}
	if !reflect.DeepEqual(helper.GateTriggers, []string{"example-gate"}) {
		t.Errorf("helper gate triggers = %v, want [example-gate]", helper.GateTriggers)
	}
	wantHelperRef := []ScriptReference{{Source: "scripts/gate.sh", Kind: ReferenceLiteralSource}}
	if !reflect.DeepEqual(helper.References, wantHelperRef) {
		t.Errorf("helper references = %v, want %v", helper.References, wantHelperRef)
	}
	computed := byPath["scripts/computed.sh"]
	wantComputedRef := []ScriptReference{{Source: "scripts/gate.sh", Kind: ReferenceLiteralMention}}
	if !reflect.DeepEqual(computed.References, wantComputedRef) {
		t.Errorf("computed references = %v, want %v", computed.References, wantComputedRef)
	}

	docOnly := byPath["scripts/doc-only.sh"]
	if docOnly.Status != ScriptStatusReferenced {
		t.Errorf("doc-only status = %q, want %q", docOnly.Status, ScriptStatusReferenced)
	}
	wantDocRef := []ScriptReference{{Source: "docs/runbook.md", Kind: ReferenceLiteralMention}}
	if !reflect.DeepEqual(docOnly.References, wantDocRef) {
		t.Errorf("doc-only references = %v, want %v", docOnly.References, wantDocRef)
	}

	manual := byPath["scripts/manual.sh"]
	if manual.Status != ScriptStatusUnreferenced {
		t.Errorf("manual status = %q, want %q", manual.Status, ScriptStatusUnreferenced)
	}
	if len(manual.References) != 0 {
		t.Errorf("manual references = %v, want none", manual.References)
	}
	triggerOnly := byPath["scripts/trigger-only.sh"]
	if triggerOnly.Status != ScriptStatusUnreferenced {
		t.Errorf("trigger-only status = %q, want %q", triggerOnly.Status, ScriptStatusUnreferenced)
	}
	if !reflect.DeepEqual(triggerOnly.GateTriggers, []string{"example-gate"}) {
		t.Errorf("trigger-only gate triggers = %v, want [example-gate]", triggerOnly.GateTriggers)
	}
	nearMatch := byPath["scripts/near-match.sh"]
	if nearMatch.Status != ScriptStatusUnreferenced {
		t.Errorf("near-match status = %q, want %q", nearMatch.Status, ScriptStatusUnreferenced)
	}
	urlOnly := byPath["scripts/url-only.sh"]
	if urlOnly.Status != ScriptStatusUnreferenced {
		t.Errorf("URL-only status = %q, want %q", urlOnly.Status, ScriptStatusUnreferenced)
	}
}

func TestAuditScripts_IgnoresTrackedFilesDeletedFromWorkingTree(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	writeAuditFixture(t, repoRoot, "scripts/present.sh", "#!/usr/bin/env bash\n")
	writeAuditFixture(t, repoRoot, "scripts/deleted.sh", "#!/usr/bin/env bash\n")
	gitAuditFixture(t, repoRoot)
	if err := os.Remove(filepath.Join(repoRoot, "scripts", "deleted.sh")); err != nil {
		t.Fatal(err)
	}

	got, err := AuditScripts(repoRoot, &Registry{})
	if err != nil {
		t.Fatalf("AuditScripts() error = %v", err)
	}
	if len(got) != 1 || got[0].Path != "scripts/present.sh" {
		t.Fatalf("audits = %+v, want only scripts/present.sh", got)
	}
}

func writeAuditFixture(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitAuditFixture(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
