// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/evidredact"
)

// successEvidenceResult builds a fully-successful first-run result whose query
// returned a non-empty answer, used as the happy-path fixture for the report.
func successEvidenceResult() Result {
	r := NewResult("http://localhost:8080")
	r.RuntimeShape = ShapeExistingAPI
	r.RepoTarget = "/work/eshu"
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r = r.addStep("detect runtime", StepOK, "reachable API")
	r = r.addStep("verify runtime", StepOK, "")
	r = r.addStep("index repository", StepOK, "reused existing indexed repository")
	r = r.addStep("wait for readiness", StepOK, "ready")
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 3 (e.g. eshu)"
	r = r.addStep("first query", StepOK, r.QuerySummary)
	r.NextSteps = firstRunNextSteps(r, firstRunRuntimeDetection{Shape: ShapeExistingAPI})
	r.Truth = Truth(r, "")
	return r
}

// TestBuildFirstRunEvidenceSuccessIndexingComplete proves the success path
// derives the indexing state as "complete" from RepoIndexed (not from health)
// and records the query and readiness evidence.
func TestBuildFirstRunEvidenceSuccessIndexingComplete(t *testing.T) {
	report := BuildEvidence(successEvidenceResult(), nil)
	if report.IndexingState != EvidenceIndexingComplete {
		t.Fatalf("IndexingState = %q, want complete", report.IndexingState)
	}
	if !report.QueryAnswered {
		t.Fatal("QueryAnswered = false, want true")
	}
	if report.Outcome != EvidenceOutcomeSucceeded {
		t.Fatalf("Outcome = %q, want succeeded", report.Outcome)
	}
	if len(report.MissingEvidence) != 0 {
		t.Fatalf("MissingEvidence = %v, want empty on success", report.MissingEvidence)
	}
}

// TestBuildFirstRunEvidencePartialReadiness proves a partial index never reports
// as complete and is flagged as missing evidence.
func TestBuildFirstRunEvidencePartialReadiness(t *testing.T) {
	r := NewResult("http://localhost:8080")
	r.RuntimeShape = ShapeLocalBinaries
	r.RepoTarget = "/work/eshu"
	r.RepoIndexed = "partial"
	r.Readiness = "degraded"
	r = r.addStep("wait for readiness", StepFailed, "scan readiness timed out: still building")
	r.NextSteps = []string{"Re-run: eshu first-run"}

	report := BuildEvidence(r, nil)
	if report.IndexingState != EvidenceIndexingPartial {
		t.Fatalf("IndexingState = %q, want partial", report.IndexingState)
	}
	if report.QueryAnswered {
		t.Fatal("QueryAnswered = true, want false on partial readiness")
	}
	if len(report.MissingEvidence) == 0 {
		t.Fatal("MissingEvidence empty, want a no-answer entry on partial readiness")
	}
}

// authMismatchDiagnostic returns the auth-mismatch entry of the production
// classification table, so a test asserting on its recovery steps is asserting
// on the strings Eshu actually ships.
func authMismatchDiagnostic(t *testing.T) *Diagnostic {
	t.Helper()
	for _, rule := range onboardingRules() {
		if d := rule.build(onboardingSignal{}); d.Class == ClassAuthMismatch {
			return &d
		}
	}
	t.Fatalf("no %q rule in the classification table", ClassAuthMismatch)
	return nil
}

// TestBuildFirstRunEvidenceAuthFailureFailedState proves an auth failure during
// the query step yields a failed indexing state when no index was proven and
// surfaces the classified recovery steps and docs link.
func TestBuildFirstRunEvidenceAuthFailureFailedState(t *testing.T) {
	r := NewResult("http://localhost:8080")
	r.RuntimeShape = ShapeExistingAPI
	r.RepoIndexed = "unknown"
	r.Readiness = "unknown"
	r = r.addStep("first query", StepFailed, "GET /api/v0/repositories: 401 unauthorized")
	// The diagnostic comes from the production classification table, not from a
	// literal written here. The assertions below are about what the free-text
	// credential scan does to the REAL recovery steps, so a hand-written fixture
	// would let the shipped string drift into a shape the scan destroys while
	// this test stayed green.
	r.Diagnostic = authMismatchDiagnostic(t)
	r.Diagnostic.DocsLink = "docs/public/reference/http-api.md"
	r.NextSteps = []string{"Re-run: eshu first-run"}

	report := BuildEvidence(r, nil)
	if report.IndexingState != EvidenceIndexingFailed {
		t.Fatalf("IndexingState = %q, want failed", report.IndexingState)
	}
	if report.Diagnosis == nil {
		t.Fatal("Diagnosis = nil, want the classified auth diagnostic")
	}
	// The recovery step reaches the artifact READABLE, naming the variable an
	// operator has to set.
	//
	// That is not free. Every free-form field goes through a structural scan
	// that removes any credential-shaped "key=value" pair it finds — name and
	// all — and a "token:" header takes the rest of its line with it. The scan
	// is deliberately blind to whether a value is real, so a placeholder is
	// removed exactly like a live key. Written as
	// "Set a matching token: export ESHU_API_KEY=<server token>" this step
	// reached the artifact as "Set a matching [redacted]", which does not tell
	// a maintainer which variable to set.
	//
	// The fix was to phrase the instruction without the pair, NOT to exempt the
	// field from the scan. An exemption list has to be re-decided every time a
	// field changes where its bytes come from, and a field whose provenance
	// quietly changed is the exact shape of the leak the scan closes.
	//
	// So this asserts the variable name survives. If a future edit reintroduces
	// the pair shape, the name disappears and this goes red — which is the
	// whole point of asserting on the name rather than on "export".
	joined := strings.Join(report.NextCommands, "\n")
	if !strings.Contains(joined, "ESHU_API_KEY") {
		t.Fatalf("NextCommands = %v, want the recovery step to still name the variable to set", report.NextCommands)
	}
	if strings.Contains(joined, evidredact.FreeTextRemovalMarker) {
		t.Fatalf("NextCommands = %v, want no recovery step shortened by the credential scan", report.NextCommands)
	}
	if !strings.Contains(joined, "Re-run: eshu first-run") {
		t.Fatalf("NextCommands = %v, want the run's own next step carried through intact", report.NextCommands)
	}
	if report.DocsLinks == nil || !containsString(report.DocsLinks, "docs/public/reference/http-api.md") {
		t.Fatalf("DocsLinks = %v, want the diagnostic docs link", report.DocsLinks)
	}
}

// TestBuildFirstRunEvidenceMissingRepoEmptyIndex proves a successful query that
// returned zero repositories reports as a query that answered but flags the
// missing repository as evidence to collect.
func TestBuildFirstRunEvidenceMissingRepoEmptyIndex(t *testing.T) {
	r := NewResult("http://localhost:8080")
	r.RuntimeShape = ShapeExistingAPI
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 0 repositories"
	r = r.addStep("first query", StepOK, r.QuerySummary)
	r.Diagnostic = &Diagnostic{
		Class:         ClassNoRepositories,
		Summary:       "no repositories match the configured selector",
		RecoverySteps: []string{"eshu scan <path>"},
		DocsLink:      "docs/public/reference/local-testing.md",
	}

	report := BuildEvidence(r, nil)
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
	r := NewResult("https://user:" + secret + "@hosted.example.com/api")
	r.RuntimeShape = ShapeExistingAPI
	r.RepoIndexed = "complete"
	r.Readiness = "ready"
	r.QueryAnswered = true
	r.QuerySummary = "repositories query returned 1 (e.g. eshu)"

	report := BuildEvidence(r, nil)
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
	RenderEvidenceTerminal(&term, report)
	if strings.Contains(term.String(), secret) {
		t.Fatal("terminal summary leaks the embedded credential")
	}
}

// TestEvidenceRedactsTokenInMCPEndpoint proves a token embedded in the MCP
// endpoint is redacted across every rendering surface.
func TestEvidenceRedactsTokenInMCPEndpoint(t *testing.T) {
	const secret = "mcptokenABCDEFGHIJKLMNOP"
	r := successEvidenceResult()
	report := BuildEvidence(r, &EvidenceInputs{
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
	report := BuildEvidence(successEvidenceResult(), nil)
	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["indexing_state"] != string(EvidenceIndexingComplete) {
		t.Fatalf("indexing_state = %v, want complete", decoded["indexing_state"])
	}
	if decoded["outcome"] != string(EvidenceOutcomeSucceeded) {
		t.Fatalf("outcome = %v, want succeeded", decoded["outcome"])
	}
}

// TestNormalizeEvidenceFormat proves the accepted format spellings and that an
// unknown format is rejected.
func TestNormalizeEvidenceFormat(t *testing.T) {
	for _, in := range []string{"", "md", "markdown", "MD", "json", "JSON"} {
		if _, err := NormalizeEvidenceFormat(in); err != nil {
			t.Fatalf("NormalizeEvidenceFormat(%q) error = %v, want nil", in, err)
		}
	}
	if _, err := NormalizeEvidenceFormat("yaml"); err == nil {
		t.Fatal("NormalizeEvidenceFormat(yaml) error = nil, want unsupported error")
	}
}

// TestWriteEvidenceArtifactRedactsOnDisk proves the on-disk artifact never
// contains an embedded credential and is written with owner-only permissions.
func TestWriteEvidenceArtifactRedactsOnDisk(t *testing.T) {
	const secret = "disksecrettoken1234567890"
	r := successEvidenceResult()
	r.ServiceURL = "https://user:" + secret + "@hosted.example.com/api"
	report := BuildEvidence(r, nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.md")
	if err := WriteEvidenceArtifact(report, "md", path); err != nil {
		t.Fatalf("WriteEvidenceArtifact: %v", err)
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

// containsString reports whether the slice contains the target value.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
