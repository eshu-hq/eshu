// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// TestExecuteFirstRunAttachesComposeDiagnostic proves a compose-down failure
// produces a classified diagnostic on the result while preserving the
// underlying runtime error text.
func TestExecuteFirstRunAttachesComposeDiagnostic(t *testing.T) {
	deps := Deps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{"/ws/docker-compose.yaml": true}),
		FetchStatus:   func(scan.Client) (scan.PipelineStatus, error) { return scan.PipelineStatus{}, nil },
		ListRepos:     func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want compose-down failure")
	}
	if result.Diagnostic == nil {
		t.Fatal("result.Diagnostic = nil, want a compose_unhealthy diagnostic")
	}
	if result.Diagnostic.Class != ClassComposeUnhealthy {
		t.Fatalf("Diagnostic.Class = %q, want compose_unhealthy", result.Diagnostic.Class)
	}
	// Root cause from the verify step must be preserved on the diagnostic.
	if !strings.Contains(result.Diagnostic.rootCause(), "docker compose up") {
		t.Fatalf("rootCause = %q, want preserved verify hint", result.Diagnostic.rootCause())
	}
}

// TestExecuteFirstRunAttachsBinariesDiagnostic proves a missing-binaries +
// no-runtime failure is classified as binaries_missing.
func TestExecuteFirstRunAttachsBinariesDiagnostic(t *testing.T) {
	deps := Deps{
		Probe:         fakeFirstRunProbe(false, map[string]bool{}, map[string]bool{}),
		FetchStatus:   func(scan.Client) (scan.PipelineStatus, error) { return scan.PipelineStatus{}, nil },
		ListRepos:     func(scan.Client) (RepositoryList, error) { return RepositoryList{}, nil },
		WorkspaceRoot: "/ws",
	}
	result, _ := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if result.Diagnostic == nil {
		t.Fatal("result.Diagnostic = nil, want a binaries_missing diagnostic")
	}
	if result.Diagnostic.Class != ClassBinariesMissing {
		t.Fatalf("Diagnostic.Class = %q, want binaries_missing", result.Diagnostic.Class)
	}
}

// TestExecuteFirstRunAttachesQueueDiagnostic proves a readiness failure caused
// by dead-letter queue work is classified as queue_failed_work and preserves
// the underlying readiness error.
func TestExecuteFirstRunAttachesQueueDiagnostic(t *testing.T) {
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{
				Health: scan.Health{State: "healthy"},
				Queue:  scan.Queue{DeadLetter: 4},
			}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			return RepositoryList{}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			return scan.Result{
				Status:       "partial",
				StatusReport: scan.PipelineStatus{Queue: scan.Queue{DeadLetter: 4}},
			}, errors.New("scan readiness: queue has dead-letter work")
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err == nil {
		t.Fatal("Execute() error = nil, want readiness failure")
	}
	if result.Diagnostic == nil {
		t.Fatal("result.Diagnostic = nil, want a queue_failed_work diagnostic")
	}
	if result.Diagnostic.Class != ClassQueueFailedWork {
		t.Fatalf("Diagnostic.Class = %q, want queue_failed_work", result.Diagnostic.Class)
	}
	if !strings.Contains(result.Diagnostic.rootCause(), "dead-letter") {
		t.Fatalf("rootCause = %q, want preserved readiness error", result.Diagnostic.rootCause())
	}
}

// TestExecuteFirstRunAttachesNoRepositoriesDiagnostic proves a query that
// returns zero repositories on a successful run still records the empty-index
// diagnostic for guidance, without marking the run failed.
func TestExecuteFirstRunAttachesNoRepositoriesDiagnostic(t *testing.T) {
	var scanCalled bool
	deps := Deps{
		Probe: fakeFirstRunProbe(true, map[string]bool{"eshu-bootstrap-index": true, "eshu-api": true}, map[string]bool{}),
		FetchStatus: func(scan.Client) (scan.PipelineStatus, error) {
			return scan.PipelineStatus{Health: scan.Health{State: "progressing"}}, nil
		},
		ListRepos: func(scan.Client) (RepositoryList, error) {
			return RepositoryList{}, nil
		},
		RunScan: func(context.Context, io.Writer, io.Writer, scan.Runtime, scan.Options, bool) (scan.Result, error) {
			scanCalled = true
			return scan.Result{Status: "ready"}, nil
		},
		ReposDir:      fakeReposDir,
		WorkspaceRoot: "/ws",
	}
	result, err := Execute(context.Background(), io.Discard, io.Discard, nilClient, testServiceURL, deps, baseFirstRunOptions())
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !scanCalled {
		t.Fatal("scan was not called")
	}
	if result.Diagnostic == nil || result.Diagnostic.Class != ClassNoRepositories {
		t.Fatalf("Diagnostic = %+v, want no_repositories", result.Diagnostic)
	}
}

// TestFirstRunResultJSONPreservesDiagnosisCause proves the JSON envelope carries
// the classified diagnosis and the preserved root-cause string, so machine
// consumers also see the underlying error rather than only the recovery text.
func TestFirstRunResultJSONPreservesDiagnosisCause(t *testing.T) {
	result := NewResult("http://localhost:8080")
	result.Diagnostic = &Diagnostic{
		Class:         ClassAuthMismatch,
		Summary:       "API auth/token mismatch",
		RecoverySteps: []string{"export ESHU_API_KEY=..."},
		DocsLink:      "docs/public/reference/http-api.md",
		Underlying:    errors.New("API error 401: unauthorized-token-xyz"),
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded struct {
		Diagnosis struct {
			Class string `json:"class"`
			Cause string `json:"cause"`
		} `json:"diagnosis"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.Diagnosis.Class != string(ClassAuthMismatch) {
		t.Fatalf("diagnosis.class = %q, want auth_mismatch", decoded.Diagnosis.Class)
	}
	if !strings.Contains(decoded.Diagnosis.Cause, "unauthorized-token-xyz") {
		t.Fatalf("diagnosis.cause = %q, want preserved root cause", decoded.Diagnosis.Cause)
	}
}

// TestRenderFirstRunHumanIncludesDiagnostic proves the human renderer prints the
// classified diagnostic block, the recovery steps, the docs link, and the
// preserved root-cause error together.
func TestRenderFirstRunHumanIncludesDiagnostic(t *testing.T) {
	result := NewResult("http://localhost:8080")
	result.Diagnostic = &Diagnostic{
		Class:         ClassComposeUnhealthy,
		Summary:       "Compose services are not running or are unhealthy",
		RecoverySteps: []string{"docker compose up -d"},
		DocsLink:      "docs/public/run-locally/docker-compose.md",
		Underlying:    errors.New("verify runtime: API not reachable"),
	}
	var buf bytes.Buffer
	RenderHuman(&buf, result, errors.New("verify runtime: API not reachable"))
	out := buf.String()
	for _, want := range []string{
		"Diagnosis",
		"Compose services are not running",
		"docker compose up -d",
		"docs/public/run-locally/docker-compose.md",
		"API not reachable",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q\n%s", want, out)
		}
	}
}
