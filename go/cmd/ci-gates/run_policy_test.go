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
)

func TestRunChangedSelfTestsSkipsUnchangedVerifierHarness(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"go/product.go"})

	output := runPolicyCommand(t, root, registry, paths, "--self-tests", "changed")
	assertTrace(t, root, "blocking-command\nadvisory-command\n")
	if !strings.Contains(output, "SELFTEST-SKIP blocking-gate") {
		t.Fatalf("output did not explain the skipped self-test:\n%s", output)
	}
}

func TestRunChangedSelfTestsRunsWhenHarnessChanged(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"scripts/test-blocking.sh"})

	runPolicyCommand(t, root, registry, paths, "--self-tests", "changed")
	assertTrace(t, root, "blocking-command\nblocking-test\n")
}

func TestRunDefaultsToAllSelfTests(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"go/product.go"})

	runPolicyCommand(t, root, registry, paths)
	assertTrace(t, root, "blocking-command\nblocking-test\nadvisory-command\nadvisory-test\n")
}

func TestRunBlockingOnlyLeavesAdvisoryOutsideCriticalPath(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"go/product.go"})

	output := runPolicyCommand(t, root, registry, paths, "--blocking-only")
	assertTrace(t, root, "blocking-command\nblocking-test\n")
	if !strings.Contains(output, "ADVISORY-SKIP advisory-gate") {
		t.Fatalf("output did not explain the skipped advisory gate:\n%s", output)
	}
}

func TestRunWritesCommandTimingReport(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"go/product.go"})
	reportPath := filepath.Join(root, "run-report.json")

	runPolicyCommand(
		t,
		root,
		registry,
		paths,
		"--self-tests", "changed",
		"--blocking-only",
		"--report-file", reportPath,
	)

	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report struct {
		SchemaVersion string `json:"schema_version"`
		Summary       struct {
			CommandsRun      int `json:"commands_run"`
			SelfTestsSkipped int `json:"self_tests_skipped"`
			AdvisorySkipped  int `json:"advisory_skipped"`
		} `json:"summary"`
		Commands []struct {
			GateID      string `json:"gate_id"`
			Kind        string `json:"kind"`
			CommandHash string `json:"command_sha256"`
			Outcome     string `json:"outcome"`
			DurationMS  int64  `json:"duration_ms"`
			SkipReason  string `json:"skip_reason"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, raw)
	}
	if report.SchemaVersion != "v1" {
		t.Fatalf("schema_version = %q, want v1", report.SchemaVersion)
	}
	if report.Summary.CommandsRun != 1 || report.Summary.SelfTestsSkipped != 1 || report.Summary.AdvisorySkipped != 1 {
		t.Fatalf("summary = %+v, want one run and both skip classes", report.Summary)
	}
	if len(report.Commands) != 3 {
		t.Fatalf("commands = %d, want command + skipped self-test + skipped advisory", len(report.Commands))
	}
	if report.Commands[0].GateID != "blocking-gate" || report.Commands[0].Kind != "command" || report.Commands[0].Outcome != "pass" || report.Commands[0].CommandHash == "" || report.Commands[0].DurationMS < 0 {
		t.Fatalf("first command report = %+v", report.Commands[0])
	}
}

func TestRunWritesReportWhenBlockingCommandFails(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	raw, err := os.ReadFile(registry)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	raw = []byte(strings.Replace(
		string(raw),
		`printf 'blocking-command\\n' >> trace.log`,
		"command-that-does-not-exist",
		1,
	))
	if err := os.WriteFile(registry, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	paths := writePathsFile(t, root, []string{"go/product.go"})
	reportPath := filepath.Join(root, "failed-report.json")
	args := []string{
		"run",
		"--registry", registry,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--repo-root", root,
		"--report-file", reportPath,
	}
	cmd := exec.Command(buildBinary(t), args...)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("ci-gates run unexpectedly passed:\n%s", output)
	}
	raw, err = os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read failure report: %v", err)
	}
	if !strings.Contains(string(raw), `"blocking_failures": 1`) {
		t.Fatalf("failure report did not record the blocking command failure:\n%s", raw)
	}
}

func TestRunRejectsUnknownSelfTestPolicy(t *testing.T) {
	t.Parallel()
	root, registry := writeRunPolicyRegistry(t)
	paths := writePathsFile(t, root, []string{"go/product.go"})
	cmd := exec.Command(
		buildBinary(t),
		"run",
		"--registry", registry,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--repo-root", root,
		"--self-tests", "sometimes",
	)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "--self-tests must be") {
		t.Fatalf("ci-gates run error = %v, output = %s", err, output)
	}
}

func runPolicyCommand(t *testing.T, root, registry, paths string, extra ...string) string {
	t.Helper()
	args := []string{
		"run",
		"--registry", registry,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--repo-root", root,
	}
	args = append(args, extra...)
	cmd := exec.Command(buildBinary(t), args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ci-gates run error = %v:\n%s", err, output)
	}
	return string(output)
}

func writeRunPolicyRegistry(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	registry := writeRegistry(t, root, `version: v1
gates:
  - id: blocking-gate
    name: Blocking Gate
    category: hygiene
    tier: pre-pr
    blocking: true
    triggers: ["go/**", "scripts/verify-blocking.sh", "scripts/test-blocking.sh"]
    self_test_triggers: ["scripts/verify-blocking.sh", "scripts/test-blocking.sh"]
    local:
      command: "printf 'blocking-command\\n' >> trace.log"
      test_command: "printf 'blocking-test\\n' >> trace.log"
    ci:
      workflow: test.yml
      job: blocking
    requirements: []
    ci_only_reason: ""
  - id: advisory-gate
    name: Advisory Gate
    category: hygiene
    tier: pre-pr
    blocking: false
    triggers: ["go/**", "scripts/verify-advisory.sh", "scripts/test-advisory.sh"]
    self_test_triggers: ["scripts/verify-advisory.sh", "scripts/test-advisory.sh"]
    local:
      command: "printf 'advisory-command\\n' >> trace.log"
      test_command: "printf 'advisory-test\\n' >> trace.log"
    ci:
      workflow: test.yml
      job: advisory
    requirements: []
    ci_only_reason: ""
`)
	return root, registry
}
