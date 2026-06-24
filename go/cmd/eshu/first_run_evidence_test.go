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

// successEvidenceResult builds a fully-successful first-run result whose query
// returned a non-empty answer, used as the happy-path fixture for the report.
func successEvidenceResult() firstRunResult {
	r := newFirstRunResult("http://localhost:8080")
	r.RuntimeShape = firstRunShapeExistingAPI
	r.RepoTarget = "/work/eshu"
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r = r.addStep("detect runtime", firstRunStepOK, "reachable API")
	r = r.addStep("verify runtime", firstRunStepOK, "")
	r = r.addStep("index repository", firstRunStepOK, "reused existing indexed repository")
	r = r.addStep("wait for readiness", firstRunStepOK, "ready")
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 3 (e.g. eshu)"
	r = r.addStep("first query", firstRunStepOK, r.QuerySummary)
	r.NextSteps = firstRunNextSteps(r, firstRunRuntimeDetection{Shape: firstRunShapeExistingAPI})
	r.Truth = firstRunTruth(r, "")
	return r
}

// TestBuildFirstRunEvidenceSuccessIndexingComplete proves the success path
// derives the indexing state as "complete" from RepoIndexed (not from health)
// and records the query and readiness evidence.
func TestBuildFirstRunEvidenceSuccessIndexingComplete(t *testing.T) {
	report := buildFirstRunEvidence(successEvidenceResult(), nil)
	if report.IndexingState != evidenceIndexingComplete {
		t.Fatalf("IndexingState = %q, want complete", report.IndexingState)
	}
	if !report.QueryAnswered {
		t.Fatal("QueryAnswered = false, want true")
	}
	if report.Outcome != evidenceOutcomeSucceeded {
		t.Fatalf("Outcome = %q, want succeeded", report.Outcome)
	}
	if len(report.MissingEvidence) != 0 {
		t.Fatalf("MissingEvidence = %v, want empty on success", report.MissingEvidence)
	}
}

// TestBuildFirstRunEvidencePartialReadiness proves a partial index never reports
// as complete and is flagged as missing evidence.
func TestBuildFirstRunEvidencePartialReadiness(t *testing.T) {
	r := newFirstRunResult("http://localhost:8080")
	r.RuntimeShape = firstRunShapeLocalBinaries
	r.RepoTarget = "/work/eshu"
	r.RepoIndexed = "partial"
	r.Readiness = "degraded"
	r = r.addStep("wait for readiness", firstRunStepFailed, "scan readiness timed out: still building")
	r.NextSteps = []string{"Re-run: eshu first-run"}

	report := buildFirstRunEvidence(r, nil)
	if report.IndexingState != evidenceIndexingPartial {
		t.Fatalf("IndexingState = %q, want partial", report.IndexingState)
	}
	if report.QueryAnswered {
		t.Fatal("QueryAnswered = true, want false on partial readiness")
	}
	if len(report.MissingEvidence) == 0 {
		t.Fatal("MissingEvidence empty, want a no-answer entry on partial readiness")
	}
}

// TestBuildFirstRunEvidenceAuthFailureFailedState proves an auth failure during
// the query step yields a failed indexing state when no index was proven and
// surfaces the classified recovery steps and docs link.
func TestBuildFirstRunEvidenceAuthFailureFailedState(t *testing.T) {
	r := newFirstRunResult("http://localhost:8080")
	r.RuntimeShape = firstRunShapeExistingAPI
	r.RepoIndexed = "unknown"
	r.Readiness = "unknown"
	r = r.addStep("first query", firstRunStepFailed, "GET /api/v0/repositories: 401 unauthorized")
	r.Diagnostic = &onboardingDiagnostic{
		Class:         onboardingClassAuthMismatch,
		Summary:       "the API rejected the request with an authentication error",
		RecoverySteps: []string{"export ESHU_API_KEY=<token>"},
		DocsLink:      "docs/public/reference/http-api.md",
	}
	r.NextSteps = []string{"Re-run: eshu first-run"}

	report := buildFirstRunEvidence(r, nil)
	if report.IndexingState != evidenceIndexingFailed {
		t.Fatalf("IndexingState = %q, want failed", report.IndexingState)
	}
	if report.Diagnosis == nil {
		t.Fatal("Diagnosis = nil, want the classified auth diagnostic")
	}
	joined := strings.Join(report.NextCommands, "\n")
	if !strings.Contains(joined, "ESHU_API_KEY") {
		t.Fatalf("NextCommands = %v, want the recovery step included", report.NextCommands)
	}
	if report.DocsLinks == nil || !containsString(report.DocsLinks, "docs/public/reference/http-api.md") {
		t.Fatalf("DocsLinks = %v, want the diagnostic docs link", report.DocsLinks)
	}
}

// TestBuildFirstRunEvidenceMissingRepoEmptyIndex proves a successful query that
// returned zero repositories reports as a query that answered but flags the
// missing repository as evidence to collect.
func TestBuildFirstRunEvidenceMissingRepoEmptyIndex(t *testing.T) {
	r := newFirstRunResult("http://localhost:8080")
	r.RuntimeShape = firstRunShapeExistingAPI
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 0 repositories"
	r = r.addStep("first query", firstRunStepOK, r.QuerySummary)
	r.Diagnostic = &onboardingDiagnostic{
		Class:         onboardingClassNoRepositories,
		Summary:       "no repositories match the configured selector",
		RecoverySteps: []string{"eshu scan <path>"},
		DocsLink:      "docs/public/reference/local-testing.md",
	}

	report := buildFirstRunEvidence(r, nil)
	if !report.QueryAnswered {
		t.Fatal("QueryAnswered = false, want true for an empty but valid answer")
	}
	if len(report.IndexedRepositories) != 0 {
		t.Fatalf("IndexedRepositories = %v, want empty", report.IndexedRepositories)
	}
	if len(report.MissingEvidence) == 0 {
		t.Fatal("MissingEvidence empty, want a no-repositories entry")
	}
}

// TestEvidenceRedactsTokenInServiceURL proves a credential embedded in the
// service URL never appears verbatim in the report model or its renderings.
func TestEvidenceRedactsTokenInServiceURL(t *testing.T) {
	const secret = "supersecrettoken1234567890"
	r := newFirstRunResult("https://user:" + secret + "@hosted.example.com/api")
	r.RuntimeShape = firstRunShapeExistingAPI
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 1 (e.g. eshu)"

	report := buildFirstRunEvidence(r, nil)
	if strings.Contains(report.ServiceEndpoint, secret) {
		t.Fatalf("ServiceEndpoint = %q leaks the secret", report.ServiceEndpoint)
	}

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	if strings.Contains(md, secret) {
		t.Fatal("markdown artifact leaks the embedded credential")
	}

	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	if strings.Contains(string(jsonBytes), secret) {
		t.Fatal("json artifact leaks the embedded credential")
	}

	var term strings.Builder
	renderEvidenceTerminal(&term, report)
	if strings.Contains(term.String(), secret) {
		t.Fatal("terminal summary leaks the embedded credential")
	}
}

// TestEvidenceRedactsTokenInMCPEndpoint proves a token embedded in the MCP
// endpoint is redacted across every rendering surface.
func TestEvidenceRedactsTokenInMCPEndpoint(t *testing.T) {
	const secret = "mcptokenABCDEFGHIJKLMNOP"
	r := successEvidenceResult()
	report := buildFirstRunEvidence(r, &firstRunEvidenceInputs{
		MCPEndpoint: "https://x:" + secret + "@mcp.example.com/mcp",
	})
	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	for name, body := range map[string]string{"markdown": md, "json": string(jsonBytes)} {
		if strings.Contains(body, secret) {
			t.Fatalf("%s artifact leaks the MCP endpoint credential", name)
		}
	}
}

// TestEvidenceJSONRoundTrips proves the JSON artifact is well-formed and carries
// the load-bearing indexing-state and outcome fields.
func TestEvidenceJSONRoundTrips(t *testing.T) {
	report := buildFirstRunEvidence(successEvidenceResult(), nil)
	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["indexing_state"] != string(evidenceIndexingComplete) {
		t.Fatalf("indexing_state = %v, want complete", decoded["indexing_state"])
	}
	if decoded["outcome"] != string(evidenceOutcomeSucceeded) {
		t.Fatalf("outcome = %v, want succeeded", decoded["outcome"])
	}
}

// TestNormalizeEvidenceFormat proves the accepted format spellings and that an
// unknown format is rejected.
func TestNormalizeEvidenceFormat(t *testing.T) {
	for _, in := range []string{"", "md", "markdown", "MD", "json", "JSON"} {
		if _, err := normalizeEvidenceFormat(in); err != nil {
			t.Fatalf("normalizeEvidenceFormat(%q) error = %v, want nil", in, err)
		}
	}
	if _, err := normalizeEvidenceFormat("yaml"); err == nil {
		t.Fatal("normalizeEvidenceFormat(yaml) error = nil, want unsupported error")
	}
}

// TestFirstRunReportSubcommandRendersEnvelope proves `first-run report` rebuilds
// the evidence report from a saved --json envelope and redacts secrets, without
// re-running any step.
func TestFirstRunReportSubcommandRendersEnvelope(t *testing.T) {
	const secret = "envelopesecrettoken1234567890"
	result := successEvidenceResult()
	result.ServiceURL = "https://user:" + secret + "@hosted.example.com/api"
	envelope := map[string]any{
		"data":  result,
		"truth": result.Truth,
		"error": nil,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	cmd := newFirstRunReportCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewReader(raw))
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("Set(format): %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatal("report subcommand output leaks the embedded credential")
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal rendered json: %v; out=%s", err, out.String())
	}
	if decoded["indexing_state"] != string(evidenceIndexingComplete) {
		t.Fatalf("indexing_state = %v, want complete", decoded["indexing_state"])
	}
}

// TestFirstRunReportSubcommandRejectsEmptyEnvelope proves a non-envelope input
// is rejected rather than silently producing an empty report.
func TestFirstRunReportSubcommandRejectsEmptyEnvelope(t *testing.T) {
	cmd := newFirstRunReportCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(`{"truth":{}}`))
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want missing data block error")
	}
}

// TestWriteEvidenceArtifactRedactsOnDisk proves the on-disk artifact never
// contains an embedded credential and is written with owner-only permissions.
func TestWriteEvidenceArtifactRedactsOnDisk(t *testing.T) {
	const secret = "disksecrettoken1234567890"
	r := successEvidenceResult()
	r.ServiceURL = "https://user:" + secret + "@hosted.example.com/api"
	report := buildFirstRunEvidence(r, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.md")
	if err := writeEvidenceArtifact(report, "md", path); err != nil {
		t.Fatalf("writeEvidenceArtifact: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("on-disk artifact leaks the embedded credential")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("artifact perm = %o, want 600", perm)
	}
}

// TestFirstRunReportSubcommandRegistered proves the report subcommand is wired
// under first-run with its flags.
func TestFirstRunReportSubcommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"first-run", "report"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run report) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "report" {
		t.Fatalf("command = %#v, want report", cmd)
	}
	for _, name := range []string{"from", "format", "out"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("report flag %q missing", name)
		}
	}
}

// TestFirstRunReportFlagsRegistered proves the evidence flags exist on first-run.
func TestFirstRunReportFlagsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"first-run"})
	if err != nil {
		t.Fatalf("rootCmd.Find(first-run) error = %v", err)
	}
	for _, name := range []string{"report", "report-format", "report-out"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("first-run flag %q missing", name)
		}
	}
}

// containsString reports whether the slice contains the target value.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
