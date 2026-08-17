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

// TestExecuteGatesEmptyCommandNotReportedAsRun proves a gate with an
// intentionally empty local.command (a permanently local-only gate whose
// enforcement mechanism cannot be a command at all -- prepr-stamp-verify-selftest
// is the real one) does not get an "RUN <gate>: " line with nothing after the
// colon. Before this test, localGateCommands unconditionally included the
// empty command as a step: runShellCommand executed an empty shell command,
// which always succeeds, so the output showed a RUN line that looked like
// every other real command line in the log but never ran anything -- a
// reporting false-green in the very row that exists to prevent one (#6149
// follow-up
// item 8 review, "verify before push").
func TestExecuteGatesEmptyCommandNotReportedAsRun(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	selection := cigates.Selection{
		Selected: true,
		Gate: cigates.Gate{
			ID:       "test-gate",
			Blocking: false,
			Local: &cigates.Local{
				Command:     "",
				TestCommand: "printf 'test\\n' >> trace.log",
			},
		},
	}

	var output bytes.Buffer
	if err := executeGates(&output, []cigates.Selection{selection}, repoRoot); err != nil {
		t.Fatalf("executeGates() error = %v, want nil", err)
	}

	if strings.Contains(output.String(), "RUN      test-gate: \n") {
		t.Fatalf("executeGates() reported an empty local.command as a RUN step:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "TEST     test-gate: printf 'test\\n' >> trace.log") {
		t.Fatalf("executeGates() output did not report the real test command:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "PASS     test-gate") {
		t.Fatalf("executeGates() output did not report the gate as passing:\n%s", output.String())
	}
	assertTrace(t, repoRoot, "test\n")
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
