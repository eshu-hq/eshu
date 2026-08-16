// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
)

// narrowOnboardOptions returns onboarding options that pass rule validation: an
// explicit, narrow repository set and a known team name.
func narrowOnboardOptions() OnboardOptions {
	return OnboardOptions{
		ServiceURL: hostedServiceURL,
		APIKey:     secretToken,
		Team:       "payments",
		Platform:   "claude",
		Rules:      []RepoRule{{Kind: RuleExact, Value: "acme/payments-api"}},
	}
}

// TestOnboardBroadRulesRejectedWithoutConfirm proves a whole-org glob is
// rejected before any connection check runs, unless broad ingestion is
// explicitly confirmed. This is the core safety acceptance criterion.
func TestOnboardBroadRulesRejectedWithoutConfirm(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Rules = []RepoRule{{Kind: RulePattern, Value: "acme/*"}}

	deps := okDeps()
	deps.Health = func() error { t.Fatal("connection checks ran on a rejected broad rule set"); return nil }
	deps.ListRepos = func() (RepositoryList, error) {
		t.Fatal("bounded query ran on a rejected broad rule set")
		return RepositoryList{}, nil
	}

	artifact, err := ExecuteOnboard(deps, opts)
	if err == nil {
		t.Fatal("ExecuteOnboard() err = nil, want broad-ingestion rejection")
	}
	if !strings.Contains(err.Error(), "confirm-broad") {
		t.Fatalf("error %q does not mention the confirm-broad escape hatch", err.Error())
	}
	if artifact.Connection.QueryAnswered {
		t.Fatal("connection checks ran despite a rejected broad rule set")
	}
	if !artifact.RuleScope.Broad {
		t.Fatal("artifact must record that the rejected rule set was broad")
	}
	if artifact.RuleScope.Confirmed {
		t.Fatal("artifact must not record a confirmation that was never given")
	}
	if artifact.QueueStatus == "" || !strings.Contains(artifact.QueueStatus, "rejected") {
		t.Fatalf("QueueStatus = %q, want an explicit not-checked explanation", artifact.QueueStatus)
	}
}

// TestOnboardEveryBroadShapeIsRefused proves each accidental-org-ingestion shape
// reaches the refusal path through the onboarding entry point, not just through
// the rule classifier in isolation.
func TestOnboardEveryBroadShapeIsRefused(t *testing.T) {
	t.Parallel()
	for _, rules := range [][]RepoRule{
		nil,
		{{Kind: RulePattern, Value: "acme/*"}},
		{{Kind: RulePattern, Value: "*"}},
		{{Kind: RulePattern, Value: ".*"}},
		{{Kind: RulePattern, Value: "^acme/.*$"}},
		{{Kind: RuleExact, Value: "acme/api"}, {Kind: RulePattern, Value: "acme/.*"}},
	} {
		opts := narrowOnboardOptions()
		opts.Rules = rules
		artifact, err := ExecuteOnboard(okDeps(), opts)
		if err == nil {
			t.Fatalf("rules %v: err = nil, want refusal", rules)
		}
		if !artifact.RuleScope.Broad {
			t.Fatalf("rules %v: artifact does not record the broad classification", rules)
		}
		if artifact.Connection.QueryAnswered {
			t.Fatalf("rules %v: connection checks ran on a refused rule set", rules)
		}
	}
}

// TestOnboardBroadRulesAllowedWithConfirm proves the explicit confirm flag
// is the documented escape hatch: a broad rule set proceeds to connection checks
// when confirmed, and the artifact records the confirmation.
func TestOnboardBroadRulesAllowedWithConfirm(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Rules = []RepoRule{{Kind: RulePattern, Value: "acme/*"}}
	opts.ConfirmBroad = true

	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v, want nil with --confirm-broad", err)
	}
	if !artifact.RuleScope.Broad {
		t.Fatal("artifact must still record that the rule set was broad")
	}
	if !artifact.RuleScope.Confirmed {
		t.Fatal("artifact must record that broad ingestion was explicitly confirmed")
	}
}

// TestOnboardNarrowRulesProceed proves a narrow, explicit repository set
// passes validation and reaches a connected onboarding artifact.
func TestOnboardNarrowRulesProceed(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v, want nil for narrow rules", err)
	}
	if artifact.RuleScope.Broad {
		t.Fatal("narrow rule set classified as broad")
	}
	if !artifact.Connection.QueryAnswered {
		t.Fatal("narrow onboarding did not reach a returned bounded query")
	}
}

// TestOnboardRequiresTeamName proves onboarding rejects an empty team name
// so an artifact is never handed out without an owning team.
func TestOnboardRequiresTeamName(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.Team = "   "
	if _, err := ExecuteOnboard(okDeps(), opts); err == nil {
		t.Fatal("ExecuteOnboard() err = nil, want error for empty team name")
	}
}

// TestOnboardArtifactOutputFields proves the artifact carries every field
// the acceptance criteria require: API URL, MCP URL, token source name, indexed
// repos, queue/completeness status, and starter prompts.
func TestOnboardArtifactOutputFields(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
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
	assertStarterPlaybook(t, artifact.StarterPlaybooks)
	if strings.TrimSpace(artifact.ScopedIsolationLimitation) == "" {
		t.Fatal("artifact must document the scoped-token isolation limitation")
	}
}

func assertStarterPlaybook(t *testing.T, playbooks []StarterPlaybook) {
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

// TestOnboardTokenSourceNameIsReferenceNotValue proves the artifact only
// ever exposes the token SOURCE NAME, never the raw secret, across the model,
// the JSON artifact, and the Markdown artifact.
func TestOnboardTokenSourceNameIsReferenceNotValue(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}

	if artifact.TokenSourceName != mcpsetup.APIKeyEnvVar {
		t.Fatalf("TokenSourceName = %q, want the env var name %q (never the value)", artifact.TokenSourceName, mcpsetup.APIKeyEnvVar)
	}

	jsonBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("marshal artifact: %v", err)
	}
	if strings.Contains(string(jsonBytes), secretToken) {
		t.Fatal("JSON artifact leaked the raw bearer token value")
	}

	markdown, err := RenderArtifactMarkdown(artifact)
	if err != nil {
		t.Fatalf("RenderArtifactMarkdown: %v", err)
	}
	if strings.Contains(markdown, secretToken) {
		t.Fatal("Markdown artifact leaked the raw bearer token value")
	}
	if !strings.Contains(markdown, mcpsetup.APIKeyEnvVar) {
		t.Fatal("Markdown artifact must name the token source env var")
	}
}

// TestOnboardTokenSourceNameWithoutToken proves an unresolved token is reported
// as the env var to set rather than an empty or invented source.
func TestOnboardTokenSourceNameWithoutToken(t *testing.T) {
	t.Parallel()
	opts := narrowOnboardOptions()
	opts.APIKey = ""
	artifact, err := ExecuteOnboard(okDeps(), opts)
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	if want := mcpsetup.APIKeyEnvVar + " (unset)"; artifact.TokenSourceName != want {
		t.Fatalf("TokenSourceName = %q, want %q", artifact.TokenSourceName, want)
	}

	opts.Rules = []RepoRule{{Kind: RulePattern, Value: "acme/*"}}
	rejected, err := ExecuteOnboard(okDeps(), opts)
	if err == nil {
		t.Fatal("expected the broad rule set to be refused")
	}
	if want := mcpsetup.APIKeyEnvVar + " (unset)"; rejected.TokenSourceName != want {
		t.Fatalf("refused artifact TokenSourceName = %q, want %q", rejected.TokenSourceName, want)
	}
}

// TestOnboardIncompleteConnectionStillSafeArtifact proves that when the
// hosted service is not yet ready, onboarding still produces a redacted artifact
// describing the gap rather than failing without guidance.
func TestOnboardIncompleteConnectionStillSafeArtifact(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.ListRepos = func() (RepositoryList, error) { return RepositoryList{}, nil }
	artifact, err := ExecuteOnboard(deps, narrowOnboardOptions())
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
	if len(artifact.IndexedRepositories) != 0 {
		t.Fatal("artifact lists indexed repositories without a returned query")
	}
}

// TestOnboardMarkdownNamesPlaybookIDs proves the shareable Markdown
// artifact gives teams concrete playbook IDs rather than generic prompt prose.
func TestOnboardMarkdownNamesPlaybookIDs(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	markdown, err := RenderArtifactMarkdown(artifact)
	if err != nil {
		t.Fatalf("RenderArtifactMarkdown: %v", err)
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

// TestOnboardArtifactRoundTripsJSON proves the persisted artifact decodes back
// into the same shape a downstream consumer reads.
func TestOnboardArtifactRoundTripsJSON(t *testing.T) {
	t.Parallel()
	artifact, err := ExecuteOnboard(okDeps(), narrowOnboardOptions())
	if err != nil {
		t.Fatalf("ExecuteOnboard() err = %v", err)
	}
	data, err := RenderArtifactJSON(artifact)
	if err != nil {
		t.Fatalf("RenderArtifactJSON: %v", err)
	}
	var decoded Artifact
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	if decoded.Command != "hosted-onboard" || decoded.Team != artifact.Team {
		t.Fatalf("decoded = %+v, want command hosted-onboard and team %q", decoded, artifact.Team)
	}
	if decoded.RuleScope.Broad != artifact.RuleScope.Broad || len(decoded.RuleScope.Rules) != len(artifact.RuleScope.Rules) {
		t.Fatalf("decoded rule scope = %+v, want %+v", decoded.RuleScope, artifact.RuleScope)
	}
}

// TestOnboardQueueStatusPerIndexState proves each index state gets its own
// operator-facing queue line instead of one generic sentence.
func TestOnboardQueueStatusPerIndexState(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, state := range []string{"ready", "building", "stale", "empty", "unknown"} {
		line := queueStatus(Result{IndexState: state})
		if strings.TrimSpace(line) == "" {
			t.Fatalf("index state %q has no queue status line", state)
		}
		if seen[line] {
			t.Fatalf("index state %q reuses another state's queue status line: %q", state, line)
		}
		seen[line] = true
	}
}

// TestOnboardPropagatesConnectionFailure proves a failing probe still yields a
// populated artifact and the truthful error, so the team gets guidance either
// way.
func TestOnboardPropagatesConnectionFailure(t *testing.T) {
	t.Parallel()
	deps := okDeps()
	deps.Health = func() error { return errors.New("connection refused") }
	artifact, err := ExecuteOnboard(deps, narrowOnboardOptions())
	if err == nil {
		t.Fatal("ExecuteOnboard() err = nil, want the probe failure")
	}
	if artifact.Command != "hosted-onboard" {
		t.Fatalf("artifact Command = %q, want hosted-onboard", artifact.Command)
	}
	if len(artifact.NextSteps) == 0 {
		t.Fatal("failed onboarding must still carry next steps")
	}
}
