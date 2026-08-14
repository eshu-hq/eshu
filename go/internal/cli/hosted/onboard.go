// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/evidredact"
	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// ScopedIsolationLimitation documents, in the artifact itself, the
// hosted authorization model and the action required for tenant isolation.
// Scoped per-team tokens now exist (#1852): the API/MCP surface resolves tokens
// through an operator-managed registry (ESHU_SCOPED_TOKENS_FILE) into bounded
// repository/scope grants. The artifact references that scoped-token source so
// it never implies isolation an operator has not actually provisioned, and is
// explicit that the fallback shared token remains broad until a scoped token is
// registered for this team's repository scope.
const ScopedIsolationLimitation = "Register a scoped per-team token for the repositories listed above in the hosted " +
	"scoped-token registry (ESHU_SCOPED_TOKENS_FILE) so it reads only this team's scope; see the hosted-governance operator guide for " +
	"issuance and rotation. Until a scoped token is registered, the shared bearer token grants every holder read access to every indexed " +
	"repository — treat the shared token as a shared-service credential, not a tenant-isolated secret."

// OnboardOptions captures the resolved hosted-onboard inputs. ServiceURL and
// APIKey are the endpoint and bearer token the caller already resolved; the raw
// key never reaches the artifact, only the env var name that sources it.
type OnboardOptions struct {
	// ServiceURL is the resolved deployed endpoint base.
	ServiceURL string
	// APIKey is the resolved bearer token value.
	APIKey string
	// Team is the owning project team name; it is required and stamped into the
	// artifact so an artifact is never handed out without an owner.
	Team string
	// Platform is the optional assistant client platform for the MCP snippet.
	Platform string
	// Rules is the validated repository sync rule set to onboard.
	Rules []RepoRule
	// Repository optionally requires a specific repository to be present in the
	// indexed scope as part of the first-answer proof.
	Repository string
	// ConfirmBroad explicitly authorizes a broad, org-wide rule set. Without it,
	// a broad rule set is rejected before any connection check runs.
	ConfirmBroad bool
}

// RuleScope records how the supplied rule set was classified and
// whether broad ingestion was explicitly confirmed, so the artifact is honest
// about the ingestion blast radius.
type RuleScope struct {
	// Rules is the validated rule set, rendered as stable kind:value tokens.
	Rules []string `json:"rules"`
	// Broad reports whether the rule set was classified as org-wide/broad.
	Broad bool `json:"broad"`
	// Confirmed reports whether broad ingestion was explicitly confirmed.
	Confirmed bool `json:"confirmed"`
	// Reason explains a broad classification in operator language.
	Reason string `json:"reason,omitempty"`
}

// Connection is the redacted connection-and-readiness summary
// projected from the reused hosted-setup staged checks. It never carries a raw
// secret and never reports a returned query unless one actually returned.
type Connection struct {
	// QueryAnswered reports whether the bounded first-answer query returned.
	QueryAnswered bool `json:"query_answered"`
	// QuerySummary is the concise first-answer summary, when one returned.
	QuerySummary string `json:"query_summary,omitempty"`
	// ToolCount is the number of visible MCP tools.
	ToolCount int `json:"tool_count"`
	// Stages preserves the per-stage outcomes for an operator reading the gap.
	Stages []Stage `json:"stages"`
}

// StarterPlaybook is the structured playbook guidance embedded in
// onboarding artifacts. It mirrors the query catalog enough for a team or
// assistant to start with first-class tools without resolving the catalog.
type StarterPlaybook struct {
	PlaybookID           string   `json:"playbook_id"`
	Version              string   `json:"version"`
	PromptFamily         string   `json:"prompt_family"`
	Prompt               string   `json:"prompt"`
	Tools                []string `json:"tools"`
	ExpectedTruthClasses []string `json:"expected_truth_classes"`
}

// Artifact is the redacted, hand-to-a-team onboarding artifact. It
// is a presentation layer over the reused hosted-setup result: every endpoint is
// redacted and only the token SOURCE NAME is ever recorded, never the value, so
// the artifact is safe to share with a project team.
type Artifact struct {
	// Command identifies the artifact producer.
	Command string `json:"command"`
	// Team is the owning project team name.
	Team string `json:"team"`
	// APIURL is the redacted hosted HTTP API endpoint.
	APIURL string `json:"api_url"`
	// MCPURL is the redacted hosted MCP endpoint.
	MCPURL string `json:"mcp_url"`
	// TokenSourceName is the env var name (or secret ref) the team configures;
	// it is never the token value.
	TokenSourceName string `json:"token_source_name"`
	// RuleScope records the rule classification and broad-confirmation state.
	RuleScope RuleScope `json:"rule_scope"`
	// IndexState is the derived empty/building/stale/ready completeness label.
	IndexState string `json:"index_state"`
	// QueueStatus is a concise queue/completeness status line for the team.
	QueueStatus string `json:"queue_status"`
	// IndexedRepositories lists repositories the onboarding observed as indexed.
	IndexedRepositories []string `json:"indexed_repositories,omitempty"`
	// Connection is the redacted connection-and-readiness summary.
	Connection Connection `json:"connection"`
	// StarterPrompts are bounded first prompts referencing first-class tools.
	StarterPrompts []string `json:"starter_prompts"`
	// StarterPlaybooks are structured starter workflows from the query playbook
	// catalog, including IDs, versions, ordered tools, and expected truth classes.
	StarterPlaybooks []StarterPlaybook `json:"starter_playbooks"`
	// SetupSnippet is the optional redacted MCP client snippet.
	SetupSnippet string `json:"setup_snippet,omitempty"`
	// ScopedIsolationLimitation documents the shared-token reality so the
	// artifact never implies isolation that does not exist.
	ScopedIsolationLimitation string `json:"scoped_isolation_limitation"`
	// NextSteps are outcome-tailored follow-ups for the onboarding team.
	NextSteps []string `json:"next_steps,omitempty"`
}

// ExecuteOnboard validates the rule set, runs the reused hosted-setup
// staged checks, and projects a redacted onboarding artifact. A broad rule set
// is rejected before any connection check runs unless ConfirmBroad is set. The
// returned error is the truthful connection outcome (nil only when the bounded
// query returned); the artifact is always populated and safe to share, even on a
// rejection or an incomplete connection, so a team always gets actionable,
// redacted guidance.
func ExecuteOnboard(deps Deps, opts OnboardOptions) (Artifact, error) {
	team := strings.TrimSpace(opts.Team)
	if team == "" {
		return Artifact{}, errors.New("onboard: team name is required (set --team)")
	}

	verdict := ClassifyRepoRules(opts.Rules)
	scope := RuleScope{
		Rules:     renderRuleTokens(opts.Rules),
		Broad:     verdict.Broad,
		Confirmed: opts.ConfirmBroad,
		Reason:    verdict.Reason,
	}

	if verdict.Broad && !opts.ConfirmBroad {
		artifact := newRejectedArtifact(team, opts, scope)
		artifact.NextSteps = []string{
			"Narrow the rule set to explicit repositories (--repo owner/name) or a scoped prefix (--repo-pattern '^org/team-').",
			"If org-wide ingestion is truly intended, re-run with --confirm-broad.",
		}
		return artifact, fmt.Errorf("onboard: %s; re-run with --confirm-broad to ingest the whole org intentionally", verdict.Reason)
	}

	setupOpts := SetupOptions{
		ServiceURL: opts.ServiceURL,
		APIKey:     opts.APIKey,
		Platform:   opts.Platform,
		Repository: opts.Repository,
	}
	result, runErr := ExecuteSetup(deps, setupOpts)

	artifact := buildArtifact(team, opts, scope, result)
	return artifact, runErr
}

// newRejectedArtifact builds a minimal redacted artifact for the cases
// (such as a rejected broad rule set) where the staged checks never ran. It
// still carries the redacted endpoints, token source name, starter prompts, and
// the scoped-isolation limitation so the artifact is always safe and useful.
func newRejectedArtifact(team string, opts OnboardOptions, scope RuleScope) Artifact {
	return Artifact{
		Command:                   "hosted-onboard",
		Team:                      team,
		APIURL:                    evidredact.Endpoint(opts.ServiceURL),
		MCPURL:                    mcpEndpoint(opts.ServiceURL),
		TokenSourceName:           tokenSourceName(opts.APIKey),
		RuleScope:                 scope,
		IndexState:                "unknown",
		QueueStatus:               "not checked: rule set rejected before connection checks",
		StarterPrompts:            StarterPrompts(),
		StarterPlaybooks:          StarterPlaybooks(),
		ScopedIsolationLimitation: ScopedIsolationLimitation,
	}
}

// buildArtifact projects a reused hosted-setup result into the
// redacted onboarding artifact. It reads only fields the staged checks already
// computed and redacts every endpoint, so no secret can leak through this layer.
//
// The client snippet is re-rendered from the REDACTED endpoint rather than
// copied from result.SetupHint. hosted-setup renders its snippet from the raw
// endpoint, which is correct for the operator who owns the credential; this
// artifact is handed to a project team, and a snippet built from the raw
// endpoint carried any embedded userinfo straight past the api_url/mcp_url
// redaction. Re-rendering keeps the snippet's URL identical to the artifact's
// own mcp_url.
func buildArtifact(team string, opts OnboardOptions, scope RuleScope, result Result) Artifact {
	artifact := Artifact{
		Command:         "hosted-onboard",
		Team:            team,
		APIURL:          evidredact.Endpoint(result.ServiceURL),
		MCPURL:          mcpEndpoint(result.ServiceURL),
		TokenSourceName: tokenSourceNameFromRef(result.TokenRef),
		RuleScope:       scope,
		IndexState:      result.IndexState,
		QueueStatus:     queueStatus(result),
		Connection: Connection{
			QueryAnswered: result.QueryAnswered,
			QuerySummary:  result.QuerySummary,
			ToolCount:     result.ToolCount,
			Stages:        result.Stages,
		},
		StarterPrompts:            StarterPrompts(),
		StarterPlaybooks:          StarterPlaybooks(),
		SetupSnippet:              setupHint(opts.Platform, evidredact.Endpoint(result.ServiceURL), opts.APIKey),
		ScopedIsolationLimitation: ScopedIsolationLimitation,
		NextSteps:                 result.NextSteps,
	}
	if result.QueryAnswered {
		artifact.IndexedRepositories = indexedRepos(result)
	}
	return artifact
}

// queueStatus renders a concise queue/completeness status line for
// the team from the readiness-derived index state. The staged checks already
// classified completeness, so this never recomputes readiness.
func queueStatus(result Result) string {
	switch result.IndexState {
	case "ready":
		return "indexing complete and drained; queries are trustworthy"
	case "building":
		return "indexing in progress; queue still draining, answers may be partial"
	case "stale":
		return "pipeline degraded or carrying failed/dead-letter work; index truth is not current"
	case "empty":
		return "no repository indexed yet; nothing to query"
	default:
		return "index completeness unknown"
	}
}

// indexedRepos extracts the indexed repository summary the staged
// query observed. The hosted-setup result records the first-query summary rather
// than a full list, so the artifact reports that summary as the indexed-scope
// evidence the team can re-run.
func indexedRepos(result Result) []string {
	summary := strings.TrimSpace(result.QuerySummary)
	if summary == "" {
		return nil
	}
	return []string{summary}
}

// renderRuleTokens renders rules as stable "kind:value" tokens for the artifact.
func renderRuleTokens(rules []RepoRule) []string {
	if len(rules) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(rules))
	for _, rule := range rules {
		tokens = append(tokens, rule.String())
	}
	return tokens
}

// mcpEndpoint derives the redacted hosted MCP endpoint from the resolved
// API base, matching the hosted MCP snippet transport path (<base>/mcp/message).
// The endpoint is redacted so any embedded credentials never survive.
func mcpEndpoint(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		return ""
	}
	return evidredact.Endpoint(trimmed + "/mcp/message")
}

// tokenSourceName returns the token SOURCE NAME a team configures, never
// the value. When a token is resolved the source is the ESHU_API_KEY env var;
// when none is resolved the team is told which env var to set.
func tokenSourceName(apiKey string) string {
	if strings.TrimSpace(apiKey) == "" {
		return mcpsetup.APIKeyEnvVar + " (unset)"
	}
	return mcpsetup.APIKeyEnvVar
}

// tokenSourceNameFromRef derives the token source name from the redacted
// token reference the staged checks recorded. An empty reference means no token
// was resolved, so the team is told which env var to set; otherwise the source
// is the ESHU_API_KEY env var. The redacted reference value itself is never
// surfaced as the source name.
func tokenSourceNameFromRef(tokenRef string) string {
	if strings.TrimSpace(tokenRef) == "" {
		return mcpsetup.APIKeyEnvVar + " (unset)"
	}
	return mcpsetup.APIKeyEnvVar
}

// StarterPrompts returns a small, bounded set of starter prompts for an
// onboarding team. They are sourced from the query playbook catalog (the single
// source of truth for first-class starter workflows) so the prompts always name
// real first-class tools; a stable fallback is used only if the catalog is
// empty.
func StarterPrompts() []string {
	playbooks := StarterPlaybooks()
	prompts := make([]string, 0, len(playbooks))
	for _, playbook := range playbooks {
		prompt := strings.TrimSpace(playbook.Prompt)
		if prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return []string{
			"Build the service story for <service> and cite the source, manifest, and runtime evidence.",
			"Investigate how <repository> handles <topic> and read the source behind the top evidence.",
			"Trace the deployment chain for <workload> in <environment>.",
		}
	}
	return prompts
}

// StarterPlaybooks projects the query playbook catalog into the artifact's
// starter-workflow shape: playbook ID, version, prompt family, ordered tools,
// and the truth class each step is expected to return.
func StarterPlaybooks() []StarterPlaybook {
	catalog := query.PlaybookCatalog()
	playbooks := make([]StarterPlaybook, 0, len(catalog))
	for _, playbook := range catalog {
		prompt := strings.TrimSpace(playbook.Description)
		if prompt == "" {
			prompt = strings.TrimSpace(playbook.Name)
		}
		tools := make([]string, 0, len(playbook.Steps))
		truthClasses := make([]string, 0, len(playbook.Steps))
		for _, step := range playbook.Steps {
			tools = append(tools, step.Tool)
			truthClasses = append(truthClasses, string(step.ExpectedTruth))
		}
		playbooks = append(playbooks, StarterPlaybook{
			PlaybookID:           playbook.ID,
			Version:              playbook.Version,
			PromptFamily:         playbook.PromptFamily,
			Prompt:               prompt,
			Tools:                tools,
			ExpectedTruthClasses: truthClasses,
		})
	}
	return playbooks
}
