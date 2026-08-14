// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic leak sentinels. These are not real credentials; they exist so a
// composed string that smuggles a raw endpoint into an operator artifact fails
// loudly instead of passing a field-scoped assertion one field over.
const (
	// leakSentinelAPI stands in for a password embedded in the API base URL.
	leakSentinelAPI = "LEAKSENTINEL-1"
	// leakSentinelMCP stands in for a password embedded in the MCP endpoint.
	leakSentinelMCP = "LEAKSENTINEL-2"
	// leakSentinelTarget stands in for a private path segment in the repo target.
	leakSentinelTarget = "LEAKSENTINEL-3"
)

// composedLeakFixture builds a failed first-run result whose credential-bearing
// values reach the evidence report only through composed strings: the verify
// hint that becomes the diagnosis cause, the MCP-endpoint-is-API summary, and
// the next-step command that embeds the repo target.
//
// It deliberately routes through the production composition functions
// (firstRunStartHint, firstRunVerifySignal, classifyOnboardingFailure,
// firstRunNextSteps) rather than hand-building an onboardingDiagnostic, so the
// test proves the real path and not a fixture that mimics it.
func composedLeakFixture(t *testing.T) (firstRunResult, string, string) {
	t.Helper()

	apiBase := "http://svcuser:" + leakSentinelAPI + "@127.0.0.1:59413"
	mcpEndpoint := "http://mcpuser:" + leakSentinelMCP + "@127.0.0.1:59413/api/v0"
	repoTarget := "/home/" + leakSentinelTarget + "/work/repo"

	// The classifier resolves the MCP endpoint through the real config seam
	// (the .env file under the app home), so point the app home at a temp dir
	// and write the endpoint there rather than stubbing the resolver.
	home := t.TempDir()
	t.Setenv(appHomeEnvVar, home)
	if err := os.WriteFile(filepath.Join(home, envFileName), []byte("ESHU_MCP_URL="+mcpEndpoint+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	detection := firstRunRuntimeDetection{
		Shape:       firstRunShapeDockerCompose,
		ComposeFile: "docker-compose.yaml",
		Detail:      "compose stack detected",
	}

	result := newFirstRunResult(apiBase)
	result.RuntimeShape = detection.Shape
	result.RepoTarget = repoTarget
	result.RepoIndexed = "failed"
	result.Readiness = "not ready"

	// Reproduce the real verify-failure path: the start hint becomes the step
	// detail, the step detail becomes the verify error, and the verify error is
	// preserved as the diagnosis root cause.
	hint := firstRunStartHint(detection, apiBase, false, "docker compose up -d")
	result = result.addStep("verify runtime", firstRunStepFailed, hint)
	verifyErr := fmt.Errorf("verify runtime: %s", hint)
	result = attachFirstRunDiagnostic(result, firstRunVerifySignal(firstRunDeps{}, detection, apiBase, verifyErr))
	result.NextSteps = firstRunNextSteps(result, detection)
	result.Truth = firstRunTruth(result, "")

	if result.Diagnostic == nil {
		t.Fatal("fixture did not attach a diagnostic; the classifier path changed")
	}
	if result.Diagnostic.Class != onboardingClassMCPEndpointIsAPI {
		t.Fatalf("diagnostic class = %q, want %q; the fixture no longer exercises the composed summary",
			result.Diagnostic.Class, onboardingClassMCPEndpointIsAPI)
	}
	return result, mcpEndpoint, repoTarget
}

// assertNoSentinels fails with the offending surface named when any synthetic
// sentinel survives into a rendered artifact. It never echoes the full body, so
// a failure message cannot itself become a leak.
func assertNoSentinels(t *testing.T, surface, body string) {
	t.Helper()
	for _, sentinel := range []string{leakSentinelAPI, leakSentinelMCP, leakSentinelTarget} {
		if strings.Contains(body, sentinel) {
			t.Errorf("%s leaks %s through a composed string", surface, sentinel)
		}
	}
}

// TestEvidenceRedactsComposedStrings proves that credentials reaching the
// evidence report through composed strings — the diagnosis summary, the
// diagnosis cause, and the next-step commands — are redacted on every rendering
// surface, not merely in the endpoint fields a name-keyed redactor was aimed at.
//
// Regression guard: the existing redaction tests place a credential only in
// ServiceURL and MCPEndpoint on a result with no diagnostic and a credential-free
// repo target, so they never exercise the composed paths and pass while the
// artifact leaks.
func TestEvidenceRedactsComposedStrings(t *testing.T) {
	result, mcpEndpoint, _ := composedLeakFixture(t)
	report := buildFirstRunEvidence(result, &firstRunEvidenceInputs{MCPEndpoint: mcpEndpoint})

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	assertNoSentinels(t, "markdown artifact", md)

	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	assertNoSentinels(t, "json artifact", string(jsonBytes))

	var term strings.Builder
	renderEvidenceTerminal(&term, report)
	assertNoSentinels(t, "terminal summary", term.String())
}

// TestEvidenceRedactsComposedNextCommandTarget proves the success path, where
// firstRunNextSteps composes "eshu story <repo target>" from the raw target that
// SelectedTarget redacts one field over. This is a separate fixture because the
// story next-step is emitted only when the run succeeded; the failure fixture
// returns static compose/local next-steps and never exercises it.
func TestEvidenceRedactsComposedNextCommandTarget(t *testing.T) {
	repoTarget := "/home/" + leakSentinelTarget + "/work/repo"

	result := successEvidenceResult()
	result.RepoTarget = repoTarget
	result.NextSteps = firstRunNextSteps(result, firstRunRuntimeDetection{Shape: firstRunShapeExistingAPI})

	report := buildFirstRunEvidence(result, nil)
	if report.SelectedTarget == repoTarget {
		t.Fatal("SelectedTarget was not redacted; the fixture no longer isolates the composed path")
	}

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	assertNoSentinels(t, "markdown next commands", md)

	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	assertNoSentinels(t, "json next commands", string(jsonBytes))

	var term strings.Builder
	renderEvidenceTerminal(&term, report)
	assertNoSentinels(t, "terminal next commands", term.String())
}

// TestEvidenceArtifactOnDiskRedactsComposedStrings proves the 0600 artifact an
// operator hands to support carries no credential in its composed strings.
func TestEvidenceArtifactOnDiskRedactsComposedStrings(t *testing.T) {
	result, mcpEndpoint, _ := composedLeakFixture(t)
	report := buildFirstRunEvidence(result, &firstRunEvidenceInputs{MCPEndpoint: mcpEndpoint})

	for _, format := range []string{evidenceFormatMarkdown, evidenceFormatJSON} {
		path := filepath.Join(t.TempDir(), "evidence."+format)
		if err := writeEvidenceArtifact(report, format, path); err != nil {
			t.Fatalf("writeEvidenceArtifact(%s): %v", format, err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
		if err != nil {
			t.Fatalf("read artifact(%s): %v", format, err)
		}
		assertNoSentinels(t, "on-disk "+format+" artifact", string(data))
	}
}

// TestFirstRunReportReEmitRedactsComposedStrings proves the latent case: a saved
// `eshu first-run --json` envelope re-rendered later by `eshu first-run report`
// must not reconstitute a credential from the composed strings it stored. This
// is the worst-reachability surface because the credential sits in an artifact
// on disk and is re-emitted on demand, long after the failing run.
func TestFirstRunReportReEmitRedactsComposedStrings(t *testing.T) {
	result, _, _ := composedLeakFixture(t)
	raw, err := json.Marshal(map[string]any{
		"data":  result,
		"truth": result.Truth,
		"error": nil,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	for _, format := range []string{evidenceFormatMarkdown, evidenceFormatJSON} {
		cmd := newFirstRunReportCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewReader(raw))
		if err := cmd.Flags().Set("format", format); err != nil {
			t.Fatalf("Set(format=%s): %v", format, err)
		}
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute(format=%s): %v", format, err)
		}
		assertNoSentinels(t, "first-run report re-emit ("+format+")", out.String())
	}
}
