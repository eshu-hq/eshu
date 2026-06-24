// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"strings"
	"testing"
)

// rootCauseSentinel is a distinctive substring asserted in every classified
// diagnostic so the tests prove the underlying error text is never swallowed.
const rootCauseSentinel = "ROOT-CAUSE-EVIDENCE-7f3a"

// assertDiagnostic checks the shared invariants every classified diagnostic
// must satisfy: the expected class, a stable summary fragment, a non-empty
// recovery step, a docs link, and the preserved root-cause evidence.
func assertDiagnostic(t *testing.T, got onboardingDiagnostic, ok bool, wantClass onboardingFailureClass, summaryFragment string) {
	t.Helper()
	if !ok {
		t.Fatalf("classifyOnboardingFailure() ok = false, want a %s diagnostic", wantClass)
	}
	if got.Class != wantClass {
		t.Fatalf("Class = %q, want %q", got.Class, wantClass)
	}
	if !strings.Contains(got.Summary, summaryFragment) {
		t.Fatalf("Summary = %q, want fragment %q", got.Summary, summaryFragment)
	}
	if len(got.RecoverySteps) == 0 {
		t.Fatalf("RecoverySteps empty for class %s", wantClass)
	}
	if strings.TrimSpace(got.DocsLink) == "" {
		t.Fatalf("DocsLink empty for class %s", wantClass)
	}
	rendered := got.String()
	if !strings.Contains(rendered, rootCauseSentinel) {
		t.Fatalf("rendered diagnostic = %q, want preserved root cause %q", rendered, rootCauseSentinel)
	}
}

func rootCauseErr() error {
	return errors.New(rootCauseSentinel + ": transport dial tcp 127.0.0.1:8080: connection refused")
}

// TestClassifyDockerCannotSeeRepoPaths covers the Docker-path-visibility class.
func TestClassifyDockerCannotSeeRepoPaths(t *testing.T) {
	signal := onboardingSignal{
		Step:           onboardingStepIndex,
		Shape:          firstRunShapeDockerCompose,
		Underlying:     errors.New(rootCauseSentinel + ": bootstrap mount /repos not visible inside container"),
		RepoPathDenied: true,
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassDockerRepoPaths, "Docker cannot see")
}

// TestClassifyComposeServicesUnhealthy covers compose-not-running / unhealthy.
func TestClassifyComposeServicesUnhealthy(t *testing.T) {
	signal := onboardingSignal{
		Step:            onboardingStepVerify,
		Shape:           firstRunShapeDockerCompose,
		Underlying:      rootCauseErr(),
		RuntimeDetail:   "API not reachable",
		RuntimeFailed:   true,
		ComposeDetected: true,
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassComposeUnhealthy, "Compose services")
}

// TestClassifyBinariesMissing covers the missing-helper-binary class.
func TestClassifyBinariesMissing(t *testing.T) {
	signal := onboardingSignal{
		Step:            onboardingStepVerify,
		Shape:           firstRunShapeUnknown,
		Underlying:      errors.New(rootCauseSentinel + ": eshu-api not found in PATH"),
		RuntimeFailed:   true,
		MissingBinaries: []string{"eshu-api", "eshu-bootstrap-index"},
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassBinariesMissing, "helper binaries")
	if !strings.Contains(got.Summary, "eshu-api") {
		t.Fatalf("Summary = %q, want missing binary name", got.Summary)
	}
}

// TestClassifyAuthMismatch covers an API auth/token mismatch (HTTP 401/403).
func TestClassifyAuthMismatch(t *testing.T) {
	for _, code := range []int{401, 403} {
		signal := onboardingSignal{
			Step:       onboardingStepQuery,
			Shape:      firstRunShapeExistingAPI,
			Underlying: &apiHTTPError{StatusCode: code, Body: rootCauseSentinel + ": unauthorized"},
		}
		got, ok := classifyOnboardingFailure(signal)
		assertDiagnostic(t, got, ok, onboardingClassAuthMismatch, "auth")
	}
}

// TestClassifyMCPEndpointPointsAtAPI covers the MCP-vs-API misconfiguration.
func TestClassifyMCPEndpointPointsAtAPI(t *testing.T) {
	signal := onboardingSignal{
		Step:        onboardingStepVerify,
		Shape:       firstRunShapeExistingAPI,
		Underlying:  errors.New(rootCauseSentinel + ": mcp client got 404 on /api/v0/repositories"),
		MCPEndpoint: "http://localhost:8080/api/v0/repositories",
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassMCPEndpointIsAPI, "MCP endpoint")
}

// TestClassifyIndexingNotReady covers health-green-but-indexing-not-done.
func TestClassifyIndexingNotReady(t *testing.T) {
	signal := onboardingSignal{
		Step:       onboardingStepReadiness,
		Shape:      firstRunShapeExistingAPI,
		Underlying: errors.New(rootCauseSentinel + ": scan readiness timed out: queue still has outstanding work"),
		Readiness:  scanReadinessVerdict{Reason: "queue still has outstanding work"},
		Queue:      scanQueue{Outstanding: 12, Pending: 12},
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassIndexingNotReady, "indexing is still")
}

// TestClassifyQueueFailedWork covers failed/retrying/dead-letter queue work.
func TestClassifyQueueFailedWork(t *testing.T) {
	signal := onboardingSignal{
		Step:       onboardingStepReadiness,
		Shape:      firstRunShapeExistingAPI,
		Underlying: errors.New(rootCauseSentinel + ": queue has dead-letter work"),
		Readiness:  scanReadinessVerdict{Terminal: true, Reason: "queue has dead-letter work"},
		Queue:      scanQueue{DeadLetter: 3, Failed: 1},
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassQueueFailedWork, "Queue has")
	if !strings.Contains(got.Summary, "dead-letter") {
		t.Fatalf("Summary = %q, want dead-letter detail", got.Summary)
	}
}

// TestRecoveryStepsReferenceRealCommands guards against recovery hints that
// point at non-existent CLI surfaces. `eshu scan` has no `--status` flag; the
// readiness surface is `eshu index-status` and dead-letter work is inspected
// with `eshu admin facts dead-letter`.
func TestRecoveryStepsReferenceRealCommands(t *testing.T) {
	queueSignal := onboardingSignal{
		Step:       onboardingStepReadiness,
		Underlying: rootCauseErr(),
		Readiness:  scanReadinessVerdict{Terminal: true, Reason: "queue has dead-letter work"},
		Queue:      scanQueue{DeadLetter: 3, Failed: 1},
	}
	buildingSignal := onboardingSignal{
		Step:       onboardingStepReadiness,
		Underlying: rootCauseErr(),
		Readiness:  scanReadinessVerdict{Reason: "queue still has outstanding work"},
		Queue:      scanQueue{Outstanding: 12, Pending: 12},
	}

	for _, tc := range []struct {
		name     string
		signal   onboardingSignal
		wantStep string
	}{
		{"queue-failed", queueSignal, "eshu admin facts dead-letter"},
		{"indexing-building", buildingSignal, "eshu index-status"},
	} {
		got, ok := classifyOnboardingFailure(tc.signal)
		if !ok {
			t.Fatalf("%s: classifyOnboardingFailure() ok = false", tc.name)
		}
		joined := strings.Join(got.RecoverySteps, "\n")
		if strings.Contains(joined, "scan --status") {
			t.Fatalf("%s: recovery steps reference non-existent `eshu scan --status`: %q", tc.name, joined)
		}
		if !strings.Contains(joined, tc.wantStep) {
			t.Fatalf("%s: recovery steps = %q, want a step referencing %q", tc.name, joined, tc.wantStep)
		}
	}
}

// TestClassifyNoRepositoriesMatch covers an empty repository-selector result.
func TestClassifyNoRepositoriesMatch(t *testing.T) {
	signal := onboardingSignal{
		Step:          onboardingStepQuery,
		Shape:         firstRunShapeExistingAPI,
		Underlying:    errors.New(rootCauseSentinel + ": no matching repository"),
		EmptyRepoList: true,
		Selector:      "my-service",
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassNoRepositories, "No repositories match")
}

// TestClassifyAssistantToolsNotVisible covers config-exists-but-tools-hidden.
func TestClassifyAssistantToolsNotVisible(t *testing.T) {
	signal := onboardingSignal{
		Step:                  onboardingStepVerify,
		Shape:                 firstRunShapeExistingAPI,
		Underlying:            errors.New(rootCauseSentinel + ": assistant lists 0 eshu tools"),
		AssistantConfigured:   true,
		AssistantToolsVisible: false,
	}
	got, ok := classifyOnboardingFailure(signal)
	assertDiagnostic(t, got, ok, onboardingClassAssistantToolsHidden, "tools are not visible")
}

// TestClassifyUnknownReturnsFalse proves the classifier does not invent a
// diagnostic when no signal matches, so the raw root-cause error survives.
func TestClassifyUnknownReturnsFalse(t *testing.T) {
	signal := onboardingSignal{
		Step:       onboardingStepQuery,
		Shape:      firstRunShapeExistingAPI,
		Underlying: errors.New("some entirely novel failure"),
	}
	if _, ok := classifyOnboardingFailure(signal); ok {
		t.Fatal("classifyOnboardingFailure() ok = true, want false for an unmatched signal")
	}
}

// TestMCPEndpointLooksLikeAPI proves the heuristic flags API-shaped URLs and
// accepts genuine MCP endpoints.
func TestMCPEndpointLooksLikeAPI(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		apiBase  string
		want     bool
	}{
		{"api v0 path", "http://localhost:8080/api/v0/repositories", "http://localhost:8080", true},
		{"bare api base", "http://localhost:8080", "http://localhost:8080", true},
		{"api base trailing slash", "http://localhost:8080/", "http://localhost:8080", true},
		{"genuine mcp message path", "http://localhost:8081/mcp/message", "http://localhost:8080", false},
		{"mcp path on same host", "http://localhost:8080/mcp/message", "http://localhost:8080", false},
		{"empty endpoint", "", "http://localhost:8080", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpEndpointLooksLikeAPI(tc.endpoint, tc.apiBase); got != tc.want {
				t.Fatalf("mcpEndpointLooksLikeAPI(%q, %q) = %v, want %v", tc.endpoint, tc.apiBase, got, tc.want)
			}
		})
	}
}

// TestDiagnosticStringPreservesRootCause proves the rendered form always carries
// the recovery guidance, the docs link, and the underlying error together.
func TestDiagnosticStringPreservesRootCause(t *testing.T) {
	d := onboardingDiagnostic{
		Class:         onboardingClassAuthMismatch,
		Summary:       "API auth/token mismatch",
		RecoverySteps: []string{"export ESHU_API_KEY=..."},
		DocsLink:      "docs/public/reference/http-api.md",
		Underlying:    rootCauseErr(),
	}
	rendered := d.String()
	for _, want := range []string{"API auth/token mismatch", "export ESHU_API_KEY", "docs/public/reference/http-api.md", rootCauseSentinel} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered = %q, want substring %q", rendered, want)
		}
	}
}
