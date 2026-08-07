// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestExecuteGatesRunsTestCommandAfterCommand(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := selectedGate(
		"printf 'command\\n' >> trace.log",
		"printf 'test\\n' >> trace.log",
		true,
	)

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err != nil {
		t.Fatalf("executeGates() error = %v, want nil", err)
	}

	assertTrace(t, repoRoot, "command\ntest\n")
	if !strings.Contains(output.String(), "TEST     test-gate: printf 'test\\n' >> trace.log") {
		t.Fatalf("executeGates() output did not report test command:\n%s", output.String())
	}
}

func TestExecuteGatesTestCommandFailureFailsBlockingGate(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := selectedGate("true", "exit 17", true)

	var output bytes.Buffer
	err := executeGates(&output, []cigates.Selection{selection}, repoRoot)
	if err == nil {
		t.Fatal("executeGates() error = nil, want blocking test-command failure")
	}
	if !strings.Contains(output.String(), "FAIL     test-gate (blocking test_command)") {
		t.Fatalf("executeGates() output did not attribute test-command failure:\n%s", output.String())
	}
}

func TestExecuteGatesStillRunsTestCommandAfterCommandFailure(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := selectedGate(
		"printf 'command\\n' >> trace.log; exit 9",
		"printf 'test\\n' >> trace.log",
		true,
	)

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err == nil {
		t.Fatal("executeGates() error = nil, want blocking command failure")
	}

	assertTrace(t, repoRoot, "command\ntest\n")
}

func TestExecuteGatesRunsIdenticalCommandOnce(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	command := "printf 'once\\n' >> trace.log"
	selection := selectedGate(command, command, true)

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err != nil {
		t.Fatalf("executeGates() error = %v, want nil", err)
	}

	assertTrace(t, repoRoot, "once\n")
}

func TestExecuteGatesAdvisoryTestCommandFailureDoesNotFailRun(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := selectedGate("true", "exit 23", false)

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err != nil {
		t.Fatalf("executeGates() error = %v, want nil for advisory failure", err)
	}
	if !strings.Contains(output.String(), "FAIL     test-gate (advisory test_command)") {
		t.Fatalf("executeGates() output did not attribute advisory test-command failure:\n%s", output.String())
	}
}

func TestRunSubcommandExecutesTestCommand(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	repoRoot := t.TempDir()
	registry := writeRegistry(t, repoRoot, `version: v1
gates:
  - id: test-command-gate
    name: Test Command Gate
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local:
      command: "printf 'command\\n' >> trace.log"
      test_command: "printf 'test\\n' >> trace.log"
    ci:
      workflow: test.yml
      job: test
    requirements: []
    ci_only_reason: ""
`)
	paths := writePathsFile(t, repoRoot, []string{"go/example.go"})

	cmd := exec.Command(
		bin,
		"run",
		"--registry", registry,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--repo-root", repoRoot,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ci-gates run error = %v:\n%s", err, output)
	}

	assertTrace(t, repoRoot, "command\ntest\n")
}

func selectedGate(command, testCommand string, blocking bool) cigates.Selection {
	return cigates.Selection{
		Selected: true,
		Gate: cigates.Gate{
			ID:       "test-gate",
			Blocking: blocking,
			Local: &cigates.Local{
				Command:     command,
				TestCommand: testCommand,
			},
		},
	}
}

func assertTrace(t *testing.T, repoRoot, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(repoRoot, "trace.log"))
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if string(got) != want {
		t.Fatalf("trace = %q, want %q", got, want)
	}
}
