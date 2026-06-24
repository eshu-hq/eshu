// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// secretToken is a recognizable bearer value used to prove it is never printed.
const secretToken = "sk-live-SUPERSECRETVALUE-do-not-print"

// hostedClientWithKey builds a client carrying a hosted endpoint and a secret
// API key so token-redaction assertions have a real value to look for.
func hostedClientWithKey() *APIClient {
	return &APIClient{BaseURL: "https://eshu.example.com", APIKey: secretToken}
}

// readyStatus returns a drained, healthy pipeline status with one completed
// generation so the readiness classifier reports ready.
func readyStatus() scanPipelineStatus {
	return scanPipelineStatus{
		Health:            scanHealth{State: "healthy"},
		GenerationHistory: scanGenerationHistory{Completed: 1},
	}
}

// okHostedDeps returns deps where every stage succeeds against a ready service
// with at least one indexed repository.
func okHostedDeps() hostedSetupDeps {
	return hostedSetupDeps{
		Health:      func(*APIClient) error { return nil },
		Ready:       func(*APIClient) error { return nil },
		FetchStatus: func(*APIClient) (scanPipelineStatus, error) { return readyStatus(), nil },
		ListTools:   mcp.ReadOnlyTools,
		ListRepos: func(*APIClient) (repositoryListResponse, error) {
			return repositoryListResponse{Repositories: []repositorySelectorEntry{{Name: "acme/api"}}}, nil
		},
	}
}

func baseHostedOptions() hostedSetupOptions {
	return hostedSetupOptions{Platform: "claude"}
}

func stageByCategory(result hostedSetupResult, name hostedStageName) hostedSetupStage {
	for _, s := range result.Stages {
		if s.Name == name {
			return s
		}
	}
	return hostedSetupStage{}
}

// TestHostedSetupHappyPathSucceeds proves a reachable, authenticated, ready
// hosted service with an indexed repo and a returning bounded query reports
// connected — and only because the query actually returned.
func TestHostedSetupHappyPathSucceeds(t *testing.T) {
	t.Parallel()
	result, err := executeHostedSetup(hostedClientWithKey(), okHostedDeps(), baseHostedOptions())
	if err != nil {
		t.Fatalf("executeHostedSetup err = %v, want nil", err)
	}
	if !result.connected() {
		t.Fatalf("connected = false, want true; stages = %+v", result.Stages)
	}
	if !result.QueryAnswered {
		t.Fatal("QueryAnswered = false, want true")
	}
	for _, s := range result.Stages {
		if s.Status == hostedStageFailed {
			t.Fatalf("stage %q failed unexpectedly: %s", s.Name, s.Detail)
		}
	}
}

// TestHostedSetupHealthAloneIsNotSuccess proves reachable health/ready does not
// flip the result to connected when the bounded query never runs.
func TestHostedSetupHealthAloneIsNotSuccess(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.ListRepos = func(*APIClient) (repositoryListResponse, error) {
		return repositoryListResponse{}, errors.New("query backend down")
	}
	result, err := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	if err == nil {
		t.Fatal("expected error when bounded query fails")
	}
	if result.connected() {
		t.Fatal("connected = true despite failing bounded query; health must not count as success")
	}
	stage := stageByCategory(result, hostedStageIndexReadiness)
	if stage.Category != hostedFailQueryFailed {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailQueryFailed)
	}
}

// TestHostedSetupAuthFailureCategory proves a 401/403 from the auth probe is
// reported as the auth-unavailable category, distinct from a generic failure.
func TestHostedSetupAuthFailureCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.Ready = func(*APIClient) error {
		return &apiHTTPError{StatusCode: 401, Body: "unauthorized"}
	}
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	stage := stageByCategory(result, hostedStageReadyz)
	if stage.Category != hostedFailAuthUnavailable {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailAuthUnavailable)
	}
	if result.connected() {
		t.Fatal("connected = true despite auth failure")
	}
}

// TestHostedSetupEmptyIndexCategory proves a reachable, ready service with zero
// indexed repositories is reported as empty-index, not generic failure.
func TestHostedSetupEmptyIndexCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.FetchStatus = func(*APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{Health: scanHealth{State: "healthy"}}, nil
	}
	deps.ListRepos = func(*APIClient) (repositoryListResponse, error) {
		return repositoryListResponse{}, nil
	}
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	stage := stageByCategory(result, hostedStageIndexReadiness)
	if stage.Category != hostedFailEmptyIndex {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailEmptyIndex)
	}
	if result.connected() {
		t.Fatal("connected = true on empty index")
	}
}

// TestHostedSetupStaleReadinessCategory proves a degraded/stalled pipeline is
// reported as stale-readiness, distinct from empty or partial.
func TestHostedSetupStaleReadinessCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.FetchStatus = func(*APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{
			Health:            scanHealth{State: "stalled", Reasons: []string{"no progress"}},
			GenerationHistory: scanGenerationHistory{Completed: 1},
		}, nil
	}
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	stage := stageByCategory(result, hostedStageIndexReadiness)
	if stage.Category != hostedFailStaleReadiness {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailStaleReadiness)
	}
}

// TestHostedSetupPartialReadinessCategory proves an in-progress pipeline with
// outstanding queue work is reported as partial-readiness.
func TestHostedSetupPartialReadinessCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.FetchStatus = func(*APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{
			Health:            scanHealth{State: "healthy"},
			Queue:             scanQueue{Outstanding: 3, Pending: 3},
			GenerationHistory: scanGenerationHistory{Completed: 1},
		}, nil
	}
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	stage := stageByCategory(result, hostedStageIndexReadiness)
	if stage.Category != hostedFailPartialReadiness {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailPartialReadiness)
	}
}

// TestHostedSetupMissingRepoScopeCategory proves that when a repository scope is
// requested but not present in the indexed set, the missing-repo-scope category
// is reported (distinct from empty-index).
func TestHostedSetupMissingRepoScopeCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.ListRepos = func(*APIClient) (repositoryListResponse, error) {
		return repositoryListResponse{Repositories: []repositorySelectorEntry{{Name: "acme/api"}}}, nil
	}
	opts := baseHostedOptions()
	opts.Repository = "acme/missing"
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, opts)
	stage := stageByCategory(result, hostedStageQuery)
	if stage.Category != hostedFailMissingRepoScope {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailMissingRepoScope)
	}
	if result.connected() {
		t.Fatal("connected = true when requested repository scope is missing")
	}
}

// TestHostedSetupMCPUnavailableCategory proves an empty MCP tool surface is
// reported as mcp-unavailable.
func TestHostedSetupMCPUnavailableCategory(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.ListTools = func() []mcp.ToolDefinition { return nil }
	result, _ := executeHostedSetup(hostedClientWithKey(), deps, baseHostedOptions())
	stage := stageByCategory(result, hostedStageMCPTools)
	if stage.Category != hostedFailMCPUnavailable {
		t.Fatalf("category = %q, want %q", stage.Category, hostedFailMCPUnavailable)
	}
	if result.connected() {
		t.Fatal("connected = true when MCP tools are unavailable")
	}
}

// TestHostedSetupNeverPrintsToken proves neither the human render nor the JSON
// envelope leaks the raw bearer token value.
func TestHostedSetupNeverPrintsToken(t *testing.T) {
	t.Parallel()
	result, _ := executeHostedSetup(hostedClientWithKey(), okHostedDeps(), baseHostedOptions())

	var human bytes.Buffer
	renderHostedSetupHuman(&human, result, nil)
	if strings.Contains(human.String(), secretToken) {
		t.Fatal("human output leaked the raw bearer token")
	}

	var jsonBuf bytes.Buffer
	envelope := map[string]any{"data": result, "truth": nil, "error": nil}
	if err := json.NewEncoder(&jsonBuf).Encode(envelope); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	if strings.Contains(jsonBuf.String(), secretToken) {
		t.Fatal("JSON envelope leaked the raw bearer token")
	}
}

// TestHostedSetupHostedSnippetGenerated proves the report carries a hosted MCP
// snippet produced via the #1767 snippet helpers and that the snippet uses the
// env-var reference rather than the raw secret.
func TestHostedSetupHostedSnippetGenerated(t *testing.T) {
	t.Parallel()
	result, _ := executeHostedSetup(hostedClientWithKey(), okHostedDeps(), baseHostedOptions())
	if strings.TrimSpace(result.SetupHint) == "" {
		t.Fatal("SetupHint is empty, want a hosted MCP snippet")
	}
	if !strings.Contains(result.SetupHint, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("SetupHint does not reference %s env var: %q", apiKeyEnvVar, result.SetupHint)
	}
	if strings.Contains(result.SetupHint, secretToken) {
		t.Fatal("SetupHint leaked the raw bearer token")
	}
}

// TestClassifyIndexReadinessVariants proves the classifier distinguishes empty,
// building, partial, stale, and ready without collapsing them.
func TestClassifyIndexReadinessVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   scanPipelineStatus
		repos    int
		wantCat  hostedFailCategory
		wantDone bool
	}{
		{
			name:     "ready",
			status:   readyStatus(),
			repos:    1,
			wantCat:  hostedFailNone,
			wantDone: true,
		},
		{
			name:     "empty index",
			status:   scanPipelineStatus{Health: scanHealth{State: "healthy"}},
			repos:    0,
			wantCat:  hostedFailEmptyIndex,
			wantDone: false,
		},
		{
			name:     "partial outstanding work",
			status:   scanPipelineStatus{Health: scanHealth{State: "healthy"}, Queue: scanQueue{Outstanding: 2, Pending: 2}, GenerationHistory: scanGenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hostedFailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "building no generation yet",
			status:   scanPipelineStatus{Health: scanHealth{State: "healthy"}},
			repos:    1,
			wantCat:  hostedFailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "stale degraded",
			status:   scanPipelineStatus{Health: scanHealth{State: "degraded", Reasons: []string{"backend slow"}}, GenerationHistory: scanGenerationHistory{Completed: 1}},
			repos:    1,
			wantCat:  hostedFailStaleReadiness,
			wantDone: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cat, _, done := classifyIndexReadiness(tc.status, tc.repos)
			if cat != tc.wantCat {
				t.Fatalf("category = %q, want %q", cat, tc.wantCat)
			}
			if done != tc.wantDone {
				t.Fatalf("done = %v, want %v", done, tc.wantDone)
			}
		})
	}
}
