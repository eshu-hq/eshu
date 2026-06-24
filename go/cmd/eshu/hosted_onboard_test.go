// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// narrowOnboardOptions returns onboarding options that pass rule validation: an
// explicit, narrow repository set and a known team name.
func narrowOnboardOptions() hostedOnboardOptions {
	return hostedOnboardOptions{
		Team:     "payments",
		Platform: "claude",
		Rules:    []hostedRepoRule{{Kind: repoRuleExact, Value: "acme/payments-api"}},
	}
}

// TestHostedOnboardBroadRulesRejectedWithoutConfirm proves a whole-org glob is
// rejected before any connection check runs, unless broad ingestion is
// explicitly confirmed. This is the core safety acceptance criterion.
func TestHostedOnboardBroadRulesRejectedWithoutConfirm(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Rules = []hostedRepoRule{{Kind: repoRulePattern, Value: "acme/*"}}

	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), opts)
	if err == nil {
		t.Fatal("executeHostedOnboard() err = nil, want broad-ingestion rejection")
	}
	if !strings.Contains(err.Error(), "confirm-broad") {
		t.Fatalf("error %q does not mention the confirm-broad escape hatch", err.Error())
	}
	if artifact.Connection.QueryAnswered {
		t.Fatal("connection checks ran despite a rejected broad rule set")
	}
	if artifact.RuleScope.Broad != true {
		t.Fatal("artifact must record that the rejected rule set was broad")
	}
}

// TestHostedOnboardBroadRulesAllowedWithConfirm proves the explicit confirm flag
// is the documented escape hatch: a broad rule set proceeds to connection checks
// when confirmed, and the artifact records the confirmation.
func TestHostedOnboardBroadRulesAllowedWithConfirm(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Rules = []hostedRepoRule{{Kind: repoRulePattern, Value: "acme/*"}}
	opts.ConfirmBroad = true

	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), opts)
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v, want nil with --confirm-broad", err)
	}
	if !artifact.RuleScope.Broad {
		t.Fatal("artifact must still record that the rule set was broad")
	}
	if !artifact.RuleScope.Confirmed {
		t.Fatal("artifact must record that broad ingestion was explicitly confirmed")
	}
}

// TestHostedOnboardNarrowRulesProceed proves a narrow, explicit repository set
// passes validation and reaches a connected onboarding artifact.
func TestHostedOnboardNarrowRulesProceed(t *testing.T) {
	t.Parallel()
	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v, want nil for narrow rules", err)
	}
	if artifact.RuleScope.Broad {
		t.Fatal("narrow rule set classified as broad")
	}
	if !artifact.Connection.QueryAnswered {
		t.Fatal("narrow onboarding did not reach a returned bounded query")
	}
}

// TestHostedOnboardRequiresTeamName proves onboarding rejects an empty team name
// so an artifact is never handed out without an owning team.
func TestHostedOnboardRequiresTeamName(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Team = "   "
	if _, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), opts); err == nil {
		t.Fatal("executeHostedOnboard() err = nil, want error for empty team name")
	}
}

// TestHostedOnboardArtifactOutputFields proves the artifact carries every field
// the acceptance criteria require: API URL, MCP URL, token source name, indexed
// repos, queue/completeness status, and starter prompts.
func TestHostedOnboardArtifactOutputFields(t *testing.T) {
	t.Parallel()
	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v", err)
	}
	if strings.TrimSpace(artifact.APIURL) == "" {
		t.Fatal("artifact missing API URL")
	}
	if strings.TrimSpace(artifact.MCPURL) == "" {
		t.Fatal("artifact missing MCP URL")
	}
	if strings.TrimSpace(artifact.TokenSourceName) == "" {
		t.Fatal("artifact missing token source name")
	}
	if len(artifact.IndexedRepositories) == 0 {
		t.Fatal("artifact missing indexed repositories")
	}
	if strings.TrimSpace(artifact.IndexState) == "" {
		t.Fatal("artifact missing index/completeness state")
	}
	if len(artifact.StarterPrompts) == 0 {
		t.Fatal("artifact missing starter prompts")
	}
	if len(artifact.StarterPlaybooks) == 0 {
		t.Fatal("artifact missing starter playbooks")
	}
	assertHostedStarterPlaybook(t, artifact.StarterPlaybooks)
	if strings.TrimSpace(artifact.ScopedIsolationLimitation) == "" {
		t.Fatal("artifact must document the scoped-token isolation limitation")
	}
}

func assertHostedStarterPlaybook(t *testing.T, playbooks []hostedOnboardStarterPlaybook) {
	t.Helper()
	for _, playbook := range playbooks {
		if playbook.PlaybookID != "service_story_citation" {
			continue
		}
		if playbook.Version != "1.0.0" {
			t.Fatalf("service_story_citation version = %q, want 1.0.0", playbook.Version)
		}
		if playbook.PromptFamily != "service.story" {
			t.Fatalf("service_story_citation prompt family = %q, want service.story", playbook.PromptFamily)
		}
		if got, want := strings.Join(playbook.Tools, " -> "), "get_service_story -> build_evidence_citation_packet"; got != want {
			t.Fatalf("service_story_citation tools = %q, want %q", got, want)
		}
		if got, want := strings.Join(playbook.ExpectedTruthClasses, ","), "deterministic,code_hint"; got != want {
			t.Fatalf("service_story_citation truth classes = %q, want %q", got, want)
		}
		return
	}
	t.Fatal("starter playbooks missing service_story_citation")
}

// TestHostedOnboardTokenSourceNameIsReferenceNotValue proves the artifact only
// ever exposes the token SOURCE NAME, never the raw secret, across the model,
// the JSON artifact, and the Markdown artifact.
func TestHostedOnboardTokenSourceNameIsReferenceNotValue(t *testing.T) {
	t.Parallel()
	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v", err)
	}

	if artifact.TokenSourceName != apiKeyEnvVar {
		t.Fatalf("TokenSourceName = %q, want the env var name %q (never the value)", artifact.TokenSourceName, apiKeyEnvVar)
	}

	jsonBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	if strings.Contains(string(jsonBytes), secretToken) {
		t.Fatal("JSON artifact leaked the raw bearer token value")
	}

	markdown, err := renderHostedOnboardMarkdown(artifact)
	if err != nil {
		t.Fatalf("renderHostedOnboardMarkdown: %v", err)
	}
	if strings.Contains(markdown, secretToken) {
		t.Fatal("Markdown artifact leaked the raw bearer token value")
	}
	if !strings.Contains(markdown, apiKeyEnvVar) {
		t.Fatal("Markdown artifact must name the token source env var")
	}
}

// TestHostedOnboardArtifactRedactsEndpointCredentials proves any embedded
// userinfo in the resolved endpoint is redacted before it lands in the artifact.
func TestHostedOnboardArtifactRedactsEndpointCredentials(t *testing.T) {
	t.Parallel()
	client := &APIClient{BaseURL: "https://user:s3cr3t@eshu.example.com", APIKey: secretToken}
	artifact, err := executeHostedOnboard(client, okHostedDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v", err)
	}
	if strings.Contains(artifact.APIURL, "s3cr3t") {
		t.Fatalf("API URL leaked embedded credentials: %q", artifact.APIURL)
	}
	if strings.Contains(artifact.MCPURL, "s3cr3t") {
		t.Fatalf("MCP URL leaked embedded credentials: %q", artifact.MCPURL)
	}
}

// TestHostedOnboardIncompleteConnectionStillSafeArtifact proves that when the
// hosted service is not yet ready, onboarding still produces a redacted artifact
// describing the gap rather than failing without guidance.
func TestHostedOnboardIncompleteConnectionStillSafeArtifact(t *testing.T) {
	t.Parallel()
	deps := okHostedDeps()
	deps.FetchStatus = func(*APIClient) (scanPipelineStatus, error) {
		return scanPipelineStatus{Health: scanHealth{State: "healthy"}}, nil
	}
	deps.ListRepos = func(*APIClient) (repositoryListResponse, error) {
		return repositoryListResponse{}, nil
	}
	artifact, err := executeHostedOnboard(hostedClientWithKey(), deps, narrowOnboardOptions())
	if err == nil {
		t.Fatal("expected a non-nil error when the index is empty")
	}
	if artifact.Connection.QueryAnswered {
		t.Fatal("artifact reports a returned query on an empty index")
	}
	if strings.TrimSpace(artifact.TokenSourceName) == "" {
		t.Fatal("incomplete artifact must still carry the token source name")
	}
	if len(artifact.StarterPrompts) == 0 {
		t.Fatal("incomplete artifact must still carry starter prompts")
	}
	if len(artifact.StarterPlaybooks) == 0 {
		t.Fatal("incomplete artifact must still carry starter playbooks")
	}
}

// TestHostedOnboardMarkdownNamesPlaybookIDs proves the shareable Markdown
// artifact gives teams concrete playbook IDs rather than generic prompt prose.
func TestHostedOnboardMarkdownNamesPlaybookIDs(t *testing.T) {
	t.Parallel()
	artifact, err := executeHostedOnboard(hostedClientWithKey(), okHostedDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("executeHostedOnboard() err = %v", err)
	}
	markdown, err := renderHostedOnboardMarkdown(artifact)
	if err != nil {
		t.Fatalf("renderHostedOnboardMarkdown: %v", err)
	}
	for _, want := range []string{
		"`service_story_citation@1.0.0`",
		"`get_service_story -> build_evidence_citation_packet`",
		"`deterministic, code_hint`",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("Markdown artifact missing %q:\n%s", want, markdown)
		}
	}
}
