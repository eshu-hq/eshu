// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// secretToken is a recognizable bearer value used to prove it is never printed.
const secretToken = "sk-live-SUPERSECRETVALUE-do-not-print"

// hostedServiceURL is the endpoint the fake deployed service answers on.
const hostedServiceURL = "https://eshu.example.com"

// authRejected is a transport error carrying an HTTP status, matching what the
// CLI's API client returns: it implements apierr.HTTPStatusError, so the
// classifier reads the code back out through apierr.StatusCode.
type authRejected struct{ code int }

func (e *authRejected) Error() string { return "API error: rejected" }

// HTTPStatusCode implements apierr.HTTPStatusError, mirroring the CLI's
// concrete transport error.
func (e *authRejected) HTTPStatusCode() int { return e.code }

// readyVerdict is a drained, healthy pipeline verdict so the readiness
// classifier reports ready.
func readyVerdict() scan.ReadinessVerdict {
	return scan.ReadinessVerdict{Ready: true, Reason: "pipeline healthy and drained"}
}

// repoList builds a bounded repositories answer whose entries match a selector
// by exact name, mirroring the CLI's repository-selector predicate closely
// enough for scope assertions.
func repoList(names ...string) RepositoryList {
	list := RepositoryList{Repositories: make([]Repository, 0, len(names))}
	for _, name := range names {
		list.Repositories = append(list.Repositories, Repository{
			ID:         name,
			Name:       name,
			ScopeMatch: func(selector string) bool { return selector == name },
		})
	}
	return list
}

// okDeps returns deps where every stage succeeds against a ready service with
// at least one indexed repository.
func okDeps() Deps {
	return Deps{
		Health:    func() error { return nil },
		Ready:     func() error { return nil },
		Readiness: func() (scan.ReadinessVerdict, error) { return readyVerdict(), nil },
		ListTools: mcp.ReadOnlyTools,
		ListRepos: func() (RepositoryList, error) { return repoList("acme/api"), nil },
	}
}

func baseSetupOptions() SetupOptions {
	return SetupOptions{ServiceURL: hostedServiceURL, APIKey: secretToken, Platform: "claude"}
}

func stageByName(result Result, name StageName) Stage {
	for _, s := range result.Stages {
		if s.Name == name {
			return s
		}
	}
	return Stage{}
}

// TestSetupHappyPathSucceeds proves a reachable, authenticated, ready hosted
// service with an indexed repo and a returning bounded query reports connected
// — and only because the query actually returned.
func TestSetupHappyPathSucceeds(t *testing.T) {
	t.Parallel()
	result, err := ExecuteSetup(okDeps(), baseSetupOptions())
	if err != nil {
		t.Fatalf("ExecuteSetup err = %v, want nil", err)
	}
	if !result.Connected() {
		t.Fatalf("Connected = false, want true; stages = %+v", result.Stages)
	}
	if !result.QueryAnswered {
		t.Fatal("QueryAnswered = false, want true")
	}
	for _, s := range result.Stages {
		if s.Status == StageFailed {
			t.Fatalf("stage %q failed unexpectedly: %s", s.Name, s.Detail)
		}
	}
}

// TestSetupUnresolvedEndpoint proves an unresolved endpoint fails at the first
// stage with its own category, before any probe runs.
func TestSetupUnresolvedEndpoint(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Health = func() error { t.Fatal("health probe ran with no endpoint resolved"); return nil }
	opts := baseSetupOptions()
	opts.ServiceURL = "   "
	result, err := ExecuteSetup(deps, opts)
	if err == nil {
		t.Fatal("ExecuteSetup err = nil, want an unresolved-endpoint error")
	}
	if got := stageByName(result, StageEndpoint).Category; got != FailUnresolvedEndpoint {
		t.Fatalf("category = %q, want %q", got, FailUnresolvedEndpoint)
	}
}

// TestSetupHealthAloneIsNotSuccess proves reachable health/ready does not flip
// the result to connected when the bounded query never runs.
func TestSetupHealthAloneIsNotSuccess(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) {
		return RepositoryList{}, errors.New("query backend down")
	}
	result, err := ExecuteSetup(deps, baseSetupOptions())
	if err == nil {
		t.Fatal("expected error when bounded query fails")
	}
	if result.Connected() {
		t.Fatal("connected = true despite failing bounded query; health must not count as success")
	}
	if got := stageByName(result, StageIndexReadiness).Category; got != FailQueryFailed {
		t.Fatalf("category = %q, want %q", got, FailQueryFailed)
	}
}

// TestSetupAuthFailureCategory proves a 401/403 from the auth probe is reported
// as the auth-unavailable category, distinct from a generic failure.
func TestSetupAuthFailureCategory(t *testing.T) {
	t.Parallel()
	for _, code := range []int{401, 403} {
		deps := okDeps()
		deps.Ready = func() error { return &authRejected{code: code} }
		result, _ := ExecuteSetup(deps, baseSetupOptions())
		if got := stageByName(result, StageReadyz).Category; got != FailAuthUnavailable {
			t.Fatalf("status %d: category = %q, want %q", code, got, FailAuthUnavailable)
		}
		if result.Connected() {
			t.Fatalf("status %d: connected = true despite auth failure", code)
		}
	}
}

// TestSetupUnreachableCategory proves a non-auth probe failure is unreachable,
// not auth-unavailable, so the operator is not sent to rotate a working token.
func TestSetupUnreachableCategory(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Health = func() error { return errors.New("connection refused") }
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageHealthz).Category; got != FailUnreachable {
		t.Fatalf("category = %q, want %q", got, FailUnreachable)
	}
}

// TestSetupReadFailureWithAuthStatusStaysAuthUnavailable proves a 401 on the
// bounded read keeps the auth category instead of collapsing to query-failed.
func TestSetupReadFailureWithAuthStatusStaysAuthUnavailable(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) { return RepositoryList{}, &authRejected{code: 403} }
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageIndexReadiness).Category; got != FailAuthUnavailable {
		t.Fatalf("category = %q, want %q", got, FailAuthUnavailable)
	}
}

// TestSetupStatusReadFailureCategory proves a failing pipeline-status read is
// classified as a probe failure (unreachable), distinct from a query failure.
func TestSetupStatusReadFailureCategory(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Readiness = func() (scan.ReadinessVerdict, error) {
		return scan.ReadinessVerdict{}, errors.New("status endpoint down")
	}
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageIndexReadiness).Category; got != FailUnreachable {
		t.Fatalf("category = %q, want %q", got, FailUnreachable)
	}
}

// TestSetupEmptyIndexCategory proves a reachable, ready service with zero
// indexed repositories is reported as empty-index, not generic failure.
func TestSetupEmptyIndexCategory(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) { return RepositoryList{}, nil }
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageIndexReadiness).Category; got != FailEmptyIndex {
		t.Fatalf("category = %q, want %q", got, FailEmptyIndex)
	}
	if result.IndexState != "empty" {
		t.Fatalf("IndexState = %q, want empty", result.IndexState)
	}
	if result.Connected() {
		t.Fatal("connected = true on empty index")
	}
}

// TestSetupStaleReadinessCategory proves a degraded/stalled pipeline is
// reported as stale-readiness, distinct from empty or partial.
func TestSetupStaleReadinessCategory(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Readiness = func() (scan.ReadinessVerdict, error) {
		return scan.ReadinessVerdict{Terminal: true, Reason: "no progress"}, nil
	}
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageIndexReadiness).Category; got != FailStaleReadiness {
		t.Fatalf("category = %q, want %q", got, FailStaleReadiness)
	}
	if result.IndexState != "stale" {
		t.Fatalf("IndexState = %q, want stale", result.IndexState)
	}
}

// TestSetupPartialReadinessCategory proves an in-progress pipeline with
// outstanding queue work is reported as partial-readiness.
func TestSetupPartialReadinessCategory(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Readiness = func() (scan.ReadinessVerdict, error) {
		return scan.ReadinessVerdict{Reason: "queue still has outstanding work"}, nil
	}
	result, _ := ExecuteSetup(deps, baseSetupOptions())
	if got := stageByName(result, StageIndexReadiness).Category; got != FailPartialReadiness {
		t.Fatalf("category = %q, want %q", got, FailPartialReadiness)
	}
	if result.IndexState != "building" {
		t.Fatalf("IndexState = %q, want building", result.IndexState)
	}
}

// TestSetupMissingRepoScopeCategory proves that when a repository scope is
// requested but not present in the indexed set, the missing-repo-scope category
// is reported (distinct from empty-index).
func TestSetupMissingRepoScopeCategory(t *testing.T) {
	t.Parallel()
	opts := baseSetupOptions()
	opts.Repository = "acme/missing"
	result, _ := ExecuteSetup(okDeps(), opts)
	if got := stageByName(result, StageQuery).Category; got != FailMissingRepoScope {
		t.Fatalf("category = %q, want %q", got, FailMissingRepoScope)
	}
	if result.Connected() {
		t.Fatal("connected = true when requested repository scope is missing")
	}
}

// TestSetupPresentRepoScopeConnects proves the same requested-scope path reports
// connected when the repository IS in the indexed set, so the missing-scope test
// above is not passing for an unrelated reason.
func TestSetupPresentRepoScopeConnects(t *testing.T) {
	t.Parallel()
	opts := baseSetupOptions()
	opts.Repository = "acme/api"
	result, err := ExecuteSetup(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteSetup err = %v, want nil", err)
	}
	if !result.Connected() {
		t.Fatal("connected = false when the requested repository is indexed")
	}
}

// TestSetupNilScopeMatchReportsMissing proves an entry with no selector
// predicate never silently satisfies a requested scope.
func TestSetupNilScopeMatchReportsMissing(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) {
		return RepositoryList{Repositories: []Repository{{ID: "acme/api", Name: "acme/api"}}}, nil
	}
	opts := baseSetupOptions()
	opts.Repository = "acme/api"
	result, _ := ExecuteSetup(deps, opts)
	if got := stageByName(result, StageQuery).Category; got != FailMissingRepoScope {
		t.Fatalf("category = %q, want %q", got, FailMissingRepoScope)
	}
}

// TestSetupMCPUnavailableCategory proves an empty MCP tool surface is reported
// as mcp-unavailable, whether the seam returns nothing or is not wired at all.
func TestSetupMCPUnavailableCategory(t *testing.T) {
	t.Parallel()
	for name, list := range map[string]func() []mcp.ToolDefinition{
		"empty tool surface": func() []mcp.ToolDefinition { return nil },
		"unwired seam":       nil,
	} {
		deps := okDeps()
		deps.ListTools = list
		result, _ := ExecuteSetup(deps, baseSetupOptions())
		if got := stageByName(result, StageMCPTools).Category; got != FailMCPUnavailable {
			t.Fatalf("%s: category = %q, want %q", name, got, FailMCPUnavailable)
		}
		if result.Connected() {
			t.Fatalf("%s: connected = true when MCP tools are unavailable", name)
		}
	}
}

// TestSetupNeverPrintsToken proves neither the human render nor the JSON
// envelope leaks the raw bearer token value.
func TestSetupNeverPrintsToken(t *testing.T) {
	t.Parallel()
	result, _ := ExecuteSetup(okDeps(), baseSetupOptions())

	var human bytes.Buffer
	RenderSetupHuman(&human, result, nil)
	if strings.Contains(human.String(), secretToken) {
		t.Fatal("human output leaked the raw bearer token")
	}

	var jsonBuf bytes.Buffer
	if err := json.NewEncoder(&jsonBuf).Encode(SetupEnvelope(result, nil)); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	if strings.Contains(jsonBuf.String(), secretToken) {
		t.Fatal("JSON envelope leaked the raw bearer token")
	}
}

// TestSetupEnvelopeCarriesRunError proves the JSON envelope reports the truthful
// failure message rather than an empty error field.
func TestSetupEnvelopeCarriesRunError(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) { return RepositoryList{}, nil }
	result, runErr := ExecuteSetup(deps, baseSetupOptions())
	envelope := SetupEnvelope(result, runErr)
	errField, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope error field = %#v, want a message map", envelope["error"])
	}
	if msg, _ := errField["message"].(string); !strings.Contains(msg, string(FailEmptyIndex)) {
		t.Fatalf("envelope error message = %q, want the empty-index category", msg)
	}
	if SetupEnvelope(result, nil)["error"] != nil {
		t.Fatal("envelope error field is populated with no run error")
	}
}

// TestSetupHostedSnippetGenerated proves the report carries a hosted MCP
// snippet produced via the #1767 snippet helpers and that the snippet uses the
// env-var reference rather than the raw secret.
func TestSetupHostedSnippetGenerated(t *testing.T) {
	t.Parallel()
	result, _ := ExecuteSetup(okDeps(), baseSetupOptions())
	if strings.TrimSpace(result.SetupHint) == "" {
		t.Fatal("SetupHint is empty, want a hosted MCP snippet")
	}
	if !strings.Contains(result.SetupHint, "${"+mcpsetup.APIKeyEnvVar+"}") {
		t.Fatalf("SetupHint does not reference %s env var: %q", mcpsetup.APIKeyEnvVar, result.SetupHint)
	}
	if strings.Contains(result.SetupHint, secretToken) {
		t.Fatal("SetupHint leaked the raw bearer token")
	}
}

// TestSetupUnknownPlatformSkipsSnippet proves an unrecognized --platform yields
// no snippet and a masked token reference instead of an error or a raw value.
func TestSetupUnknownPlatformSkipsSnippet(t *testing.T) {
	t.Parallel()
	opts := baseSetupOptions()
	opts.Platform = "not-a-client"
	result, err := ExecuteSetup(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteSetup err = %v, want nil", err)
	}
	if result.SetupHint != "" {
		t.Fatalf("SetupHint = %q, want empty for an unknown platform", result.SetupHint)
	}
	if strings.Contains(result.TokenRef, secretToken) {
		t.Fatalf("TokenRef leaked the raw bearer token: %q", result.TokenRef)
	}
}

// TestClassifyIndexReadinessVariants proves the classifier distinguishes empty,
// building, partial, stale, and ready without collapsing them.
func TestClassifyIndexReadinessVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		verdict  scan.ReadinessVerdict
		repos    int
		wantCat  FailCategory
		wantDone bool
	}{
		{
			name:     "ready",
			verdict:  readyVerdict(),
			repos:    1,
			wantCat:  FailNone,
			wantDone: true,
		},
		{
			name:     "empty index",
			verdict:  readyVerdict(),
			repos:    0,
			wantCat:  FailEmptyIndex,
			wantDone: false,
		},
		{
			name:     "partial outstanding work",
			verdict:  scan.ReadinessVerdict{Reason: "queue still has outstanding work"},
			repos:    1,
			wantCat:  FailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "building no generation yet",
			verdict:  scan.ReadinessVerdict{Reason: "no completed or active generation observed"},
			repos:    1,
			wantCat:  FailPartialReadiness,
			wantDone: false,
		},
		{
			name:     "building with nothing indexed",
			verdict:  scan.ReadinessVerdict{Reason: "queue still has outstanding work"},
			repos:    0,
			wantCat:  FailEmptyIndex,
			wantDone: false,
		},
		{
			name:     "stale degraded",
			verdict:  scan.ReadinessVerdict{Terminal: true, Reason: "backend slow"},
			repos:    1,
			wantCat:  FailStaleReadiness,
			wantDone: false,
		},
		{
			name:     "stale with no reason still explains itself",
			verdict:  scan.ReadinessVerdict{Terminal: true},
			repos:    1,
			wantCat:  FailStaleReadiness,
			wantDone: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cat, detail, done := ClassifyIndexReadiness(tc.verdict, tc.repos)
			if cat != tc.wantCat {
				t.Fatalf("category = %q, want %q", cat, tc.wantCat)
			}
			if done != tc.wantDone {
				t.Fatalf("done = %v, want %v", done, tc.wantDone)
			}
			if strings.TrimSpace(detail) == "" {
				t.Fatal("classification must carry a human detail")
			}
		})
	}
}
