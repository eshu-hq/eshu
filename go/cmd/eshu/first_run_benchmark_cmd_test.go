// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const completeBenchmarkJSON = `{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://localhost:8080",
    "repo_indexed": "complete",
    "repo_target": "/ws/demo",
    "readiness": "indexing complete",
    "query_answered": true,
    "query_summary": "repositories query returned 1 (e.g. demo)",
    "steps": []
  },
  "truth": {"level": "runtime", "freshness": "current", "completeness": "complete", "backend": "nornicdb"},
  "error": null
}`

const healthOnlyBenchmarkJSON = `{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://localhost:8080",
    "repo_indexed": "complete",
    "readiness": "indexing complete",
    "query_answered": false,
    "steps": [
      {"name": "wait for readiness", "status": "ok", "detail": "indexing complete"}
    ]
  },
  "truth": {"level": "runtime", "freshness": "current", "completeness": "complete", "backend": "nornicdb"},
  "error": null
}`

// TestFirstRunBenchmarkCommandIsRegistered proves the subcommand is wired in.
func TestFirstRunBenchmarkCommandIsRegistered(t *testing.T) {
	t.Parallel()

	cmd, _, err := rootCmd.Find([]string{"first-run-benchmark"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run-benchmark) error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "first-run-benchmark" {
		t.Fatalf("first-run-benchmark command not found: %#v", cmd)
	}
}

// runBenchmarkCommand scores an envelope file and returns the verdict plus the
// command error (non-nil means the benchmark FAILED).
func runBenchmarkCommand(t *testing.T, envelopeJSON string, extraArgs ...string) (benchmarkVerdict, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.json")
	if err := os.WriteFile(path, []byte(envelopeJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cmd := newFirstRunBenchmarkCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	args := append([]string{"--envelope", path, "--json"}, extraArgs...)
	cmd.SetArgs(args)
	runErr := cmd.Execute()

	var verdict benchmarkVerdict
	if decodeErr := json.Unmarshal(out.Bytes(), &verdict); decodeErr != nil {
		t.Fatalf("json.Unmarshal verdict error = %v; output=%s", decodeErr, out.String())
	}
	return verdict, out.String(), runErr
}

// TestFirstRunBenchmarkCommandPassesOnCompleteEnvelope proves the command exits
// zero and reports PASS for a complete proof.
func TestFirstRunBenchmarkCommandPassesOnCompleteEnvelope(t *testing.T) {
	verdict, _, runErr := runBenchmarkCommand(t, completeBenchmarkJSON)
	if runErr != nil {
		t.Fatalf("command error = %v, want nil for a complete envelope", runErr)
	}
	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false, want true; reasons: %v", verdict.failureReasons())
	}
}

// TestFirstRunBenchmarkCommandFailsOnHealthOnlyEnvelope is the command-level
// enforcement of the health-only-rejection invariant: a healthy/ready run with
// no returned query must exit non-zero.
func TestFirstRunBenchmarkCommandFailsOnHealthOnlyEnvelope(t *testing.T) {
	verdict, _, runErr := runBenchmarkCommand(t, healthOnlyBenchmarkJSON)
	if runErr == nil {
		t.Fatal("command error = nil, want non-zero exit for a health-only envelope")
	}
	if verdict.Pass {
		t.Fatal("verdict.Pass = true, want false for a health-only envelope")
	}
	if !strings.Contains(runErr.Error(), "benchmark FAILED") {
		t.Fatalf("error = %q, want it to mention benchmark FAILED", runErr.Error())
	}
}
