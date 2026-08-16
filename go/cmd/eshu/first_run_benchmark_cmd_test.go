// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
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
func runBenchmarkCommand(t *testing.T, envelopeJSON string, extraArgs ...string) (firstrunbench.Verdict, string, error) {
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

	var verdict firstrunbench.Verdict
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
		t.Fatalf("verdict.Pass = false, want true; reasons: %v", verdict.FailureReasons())
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

// TestFirstRunEnvelopeMatchesBenchmarkMirror pins the package-main envelope
// (consumed by the first-run-evidence family) and the firstrunbench mirror to
// the same wire contract. A fully populated canonical envelope must decode
// through the mirror with every benchmark-visible field intact, and a payload
// one decoder rejects must be rejected by both. If this test fails, a field
// was added or retyped on one side only — fix the drifted side, not the test.
func TestFirstRunEnvelopeMatchesBenchmarkMirror(t *testing.T) {
	t.Parallel()

	canonical := firstRunEnvelope{
		Data: firstRunResult{
			Command:       "first-run",
			RuntimeShape:  firstRunShapeDockerCompose,
			ServiceURL:    "http://localhost:8080",
			RepoIndexed:   "complete",
			RepoTarget:    "/ws/demo",
			Readiness:     "indexing complete",
			QueryAnswered: true,
			QuerySummary:  "repositories query returned 1 (e.g. demo)",
			Steps: []firstRunStep{
				{Name: "first query", Status: firstRunStepFailed, Detail: "query timed out"},
			},
			NextSteps: []string{"Re-run: eshu first-run"},
			Diagnostic: &onboardingDiagnostic{
				Class:         onboardingFailureClass("api_unreachable"),
				Summary:       "no reachable API",
				RecoverySteps: []string{"start the API"},
				DocsLink:      "docs/public/run-locally/docker-compose.md",
			},
		},
		Truth: map[string]any{"freshness": "current", "completeness": "complete"},
		Error: &firstRunEnvelopeError{Message: "verify runtime: no reachable API"},
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("json.Marshal(canonical) error = %v", err)
	}

	mirror, err := firstrunbench.ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("firstrunbench.ParseEnvelope error = %v", err)
	}

	want := firstrunbench.Envelope{
		Data: firstrunbench.Result{
			Command:       "first-run",
			RuntimeShape:  string(firstRunShapeDockerCompose),
			ServiceURL:    "http://localhost:8080",
			RepoIndexed:   "complete",
			RepoTarget:    "/ws/demo",
			Readiness:     "indexing complete",
			QueryAnswered: true,
			QuerySummary:  "repositories query returned 1 (e.g. demo)",
			Steps: []firstrunbench.Step{
				{Name: "first query", Status: firstrunbench.StepFailed, Detail: "query timed out"},
			},
			NextSteps: []string{"Re-run: eshu first-run"},
			Diagnostic: &firstrunbench.Diagnostic{
				Class:         "api_unreachable",
				Summary:       "no reachable API",
				RecoverySteps: []string{"start the API"},
				DocsLink:      "docs/public/run-locally/docker-compose.md",
			},
		},
		Truth: map[string]any{"freshness": "current", "completeness": "complete"},
		Error: &firstrunbench.EnvelopeError{Message: "verify runtime: no reachable API"},
	}
	if !reflect.DeepEqual(mirror, want) {
		t.Fatalf("mirror decode drifted from the canonical envelope:\n got %#v\nwant %#v", mirror, want)
	}

	malformed := `{"data":{"command":"first-run","steps":"broken"},"truth":{},"error":null}`
	if _, err := parseFirstRunEnvelope([]byte(malformed)); err == nil {
		t.Fatal("parseFirstRunEnvelope(malformed) error = nil, want decode failure")
	}
	if _, err := firstrunbench.ParseEnvelope([]byte(malformed)); err == nil {
		t.Fatal("firstrunbench.ParseEnvelope(malformed) error = nil, want decode failure")
	}
}

// TestFirstRunEnvelopeFieldSetsMatchBenchmarkMirror compares the JSON tag and
// field-kind sets of the canonical structs against the firstrunbench mirror.
// The round-trip test above only exercises fields it populates, so it cannot
// catch a field added, removed, or retagged on one side alone; this guard can.
func TestFirstRunEnvelopeFieldSetsMatchBenchmarkMirror(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		name      string
		canonical any
		mirror    any
	}{
		{"envelope", firstRunEnvelope{}, firstrunbench.Envelope{}},
		{"result", firstRunResult{}, firstrunbench.Result{}},
		{"step", firstRunStep{}, firstrunbench.Step{}},
		{"diagnostic", onboardingDiagnostic{}, firstrunbench.Diagnostic{}},
	}
	for _, pair := range pairs {
		got := jsonFieldKinds(t, pair.mirror)
		want := jsonFieldKinds(t, pair.canonical)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: mirror JSON fields = %v, canonical = %v; change both sides together", pair.name, got, want)
		}
	}
}

// jsonFieldKinds maps each wire-visible JSON tag of v's struct type to the
// field's reflect.Kind. Fields tagged `json:"-"` are not on the wire and are
// excluded on purpose (firstRunResult.Truth, onboardingDiagnostic.Underlying).
func jsonFieldKinds(t *testing.T, v any) map[string]reflect.Kind {
	t.Helper()
	fields := map[string]reflect.Kind{}
	typ := reflect.TypeOf(v)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "-" || tag == "" {
			continue
		}
		fields[tag] = field.Type.Kind()
	}
	return fields
}
