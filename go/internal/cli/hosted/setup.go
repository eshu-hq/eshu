// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// SetupOptions captures the resolved hosted-setup inputs. ServiceURL and APIKey
// are the endpoint and bearer token the caller already resolved from flags,
// environment, or config; the raw key is used only to derive a redacted
// reference and the client snippet, and is never recorded in the result.
type SetupOptions struct {
	// ServiceURL is the resolved deployed endpoint base.
	ServiceURL string
	// APIKey is the resolved bearer token value.
	APIKey string
	// Platform is the optional assistant client platform for the MCP snippet.
	Platform string
	// Repository optionally requires a specific repository to be present in the
	// indexed scope before the flow reports connected.
	Repository string
}

// ExecuteSetup runs the ordered, individually-reported checks and returns
// the canonical result. It never reports connected unless the final bounded
// query actually returned an answer. Each failure is recorded with its specific
// category so the operator sees exactly which stage and why.
func ExecuteSetup(deps Deps, opts SetupOptions) (Result, error) {
	tokenRef := tokenReference(opts.Platform, opts.APIKey)
	result := newResult(opts.ServiceURL, tokenRef, strings.TrimSpace(opts.Platform), strings.TrimSpace(opts.Repository))
	result.SetupHint = setupHint(opts.Platform, opts.ServiceURL, opts.APIKey)

	// Stage 1: endpoint and auth resolved.
	if strings.TrimSpace(opts.ServiceURL) == "" {
		result = result.addStage(Stage{
			Name: StageEndpoint, Status: StageFailed,
			Detail:   "no deployed endpoint resolved; set --service-url or ESHU_SERVICE_URL",
			Category: FailUnresolvedEndpoint,
		})
		return finalize(result), errors.New("resolve endpoint: no deployed endpoint configured")
	}
	authDetail := "endpoint resolved; no bearer token supplied (set ESHU_API_KEY for an authenticated service)"
	if strings.TrimSpace(opts.APIKey) != "" {
		authDetail = "endpoint and bearer token resolved (" + tokenRef + ")"
	}
	result = result.addStage(Stage{Name: StageEndpoint, Status: StageOK, Detail: authDetail})

	// Stage 2: /healthz reachable.
	if err := deps.Health(); err != nil {
		result = result.addStage(Stage{
			Name: StageHealthz, Status: StageFailed,
			Detail: err.Error(), Category: classifyProbeError(err),
		})
		return finalize(result), fmt.Errorf("healthz: %w", err)
	}
	result = result.addStage(Stage{Name: StageHealthz, Status: StageOK, Detail: "service answered /healthz"})

	// Stage 3: /readyz reachable (also proves authentication).
	if err := deps.Ready(); err != nil {
		result = result.addStage(Stage{
			Name: StageReadyz, Status: StageFailed,
			Detail: err.Error(), Category: classifyProbeError(err),
		})
		return finalize(result), fmt.Errorf("readyz: %w", err)
	}
	result = result.addStage(Stage{Name: StageReadyz, Status: StageOK, Detail: "service answered /readyz; auth accepted"})

	// Stage 4: status/index readiness. The bounded repositories query doubles as
	// the indexed-scope source so the classifier can distinguish empty vs partial
	// vs stale.
	repos, reposErr := deps.ListRepos()
	if reposErr != nil {
		result = result.addStage(Stage{
			Name: StageIndexReadiness, Status: StageFailed,
			Detail: reposErr.Error(), Category: classifyReadError(reposErr),
		})
		return finalize(result), fmt.Errorf("index readiness: %w", reposErr)
	}
	verdict, verdictErr := deps.Readiness()
	if verdictErr != nil {
		result = result.addStage(Stage{
			Name: StageIndexReadiness, Status: StageFailed,
			Detail: verdictErr.Error(), Category: classifyProbeError(verdictErr),
		})
		return finalize(result), fmt.Errorf("index readiness: %w", verdictErr)
	}
	category, detail, ready := ClassifyIndexReadiness(verdict, len(repos.Repositories))
	result.IndexState = indexStateLabel(category, ready)
	if !ready {
		result = result.addStage(Stage{
			Name: StageIndexReadiness, Status: StageWarn,
			Detail: detail, Category: category,
		})
		return finalize(result), fmt.Errorf("index readiness: %s", category)
	}
	result = result.addStage(Stage{Name: StageIndexReadiness, Status: StageOK, Detail: detail})

	// Stage 5: MCP tools visible.
	tools := visibleTools(deps.ListTools)
	result.ToolCount = len(tools)
	if len(tools) == 0 {
		result = result.addStage(Stage{
			Name: StageMCPTools, Status: StageFailed,
			Detail: "MCP tool surface is empty", Category: FailMCPUnavailable,
		})
		return finalize(result), errors.New("mcp tools: " + string(FailMCPUnavailable))
	}
	result = result.addStage(Stage{Name: StageMCPTools, Status: StageOK, Detail: fmt.Sprintf("%d tools visible", len(tools))})

	// Stage 6: one bounded query is the only stage whose success proves a useful
	// answer is reachable. A requested-but-missing repository scope is reported
	// distinctly from an empty result.
	if !scopePresent(repos, opts.Repository) {
		result = result.addStage(Stage{
			Name: StageQuery, Status: StageFailed,
			Detail:   fmt.Sprintf("repository %q not present in indexed scope", strings.TrimSpace(opts.Repository)),
			Category: FailMissingRepoScope,
		})
		return finalize(result), fmt.Errorf("first query: %s", FailMissingRepoScope)
	}
	result.QueryAnswered = true
	result.QuerySummary = querySummary(repos)
	result = result.addStage(Stage{Name: StageQuery, Status: StageOK, Detail: result.QuerySummary})
	return finalize(result), nil
}

// visibleTools resolves the MCP tool surface, tolerating a nil seam.
func visibleTools(list func() []mcp.ToolDefinition) []mcp.ToolDefinition {
	if list == nil {
		return nil
	}
	return list()
}

// querySummary renders a concise summary of the bounded query answer. An
// empty repository list is a valid, truthful answer once readiness has already
// confirmed at least one indexed repository.
func querySummary(repos RepositoryList) string {
	count := len(repos.Repositories)
	if count == 0 {
		return "repositories query returned 0 repositories"
	}
	first := strings.TrimSpace(repos.Repositories[0].Name)
	if first == "" {
		first = strings.TrimSpace(repos.Repositories[0].ID)
	}
	return fmt.Sprintf("repositories query returned %d (e.g. %s)", count, first)
}

// indexStateLabel maps a readiness category to a human index-state label.
func indexStateLabel(category FailCategory, ready bool) string {
	if ready {
		return "ready"
	}
	switch category {
	case FailEmptyIndex:
		return "empty"
	case FailStaleReadiness:
		return "stale"
	case FailPartialReadiness:
		return "building"
	default:
		return "unknown"
	}
}

// finalize attaches outcome-tailored next steps to the result.
func finalize(result Result) Result {
	result.NextSteps = nextSteps(result)
	return result
}

// nextSteps builds actionable follow-ups tailored to the first failing
// stage so an operator can fix the right thing without reading every page.
func nextSteps(result Result) []string {
	if result.Connected() {
		steps := []string{
			"Ask a deeper question through the assistant once the MCP client is configured.",
		}
		if strings.TrimSpace(result.Platform) != "" {
			steps = append(steps, "Apply the printed MCP snippet to your assistant client config.")
		} else {
			steps = append(steps, "Generate an MCP client snippet: eshu hosted-setup --platform claude")
		}
		return steps
	}
	stage, ok := result.firstFailure()
	if !ok {
		return []string{"Re-run: eshu hosted-setup"}
	}
	switch stage.Category {
	case FailUnresolvedEndpoint:
		return []string{"Set the deployed endpoint: export ESHU_SERVICE_URL=https://your-eshu-host", "Re-run: eshu hosted-setup"}
	case FailAuthUnavailable:
		// No "key=value" pair and no credential-named word before a ":", for
		// the reason recorded on the auth-mismatch rule in
		// go/cmd/eshu/diagnostics_classify.go: a step written that way is
		// removed whole from any artifact that runs the free-text credential
		// scan, taking the variable name with it. This set is not scrubbed
		// today; it is phrased this way so it stays readable if it ever is.
		return []string{"Put a valid API token into the ESHU_API_KEY environment variable", "Re-run: eshu hosted-setup"}
	case FailUnreachable:
		return []string{"Confirm the deployed service URL and network reachability.", "Re-run: eshu hosted-setup"}
	case FailEmptyIndex:
		return []string{"Index a repository on the deployed service, then re-run: eshu hosted-setup"}
	case FailPartialReadiness:
		return []string{"Wait for indexing to drain on the deployed service, then re-run: eshu hosted-setup"}
	case FailStaleReadiness:
		return []string{"Inspect the deployed pipeline status; clear failed or dead-letter work, then re-run."}
	case FailMissingRepoScope:
		return []string{"Confirm the repository is indexed and your token's scope includes it.", "List visible repositories with a deployed query, then re-run."}
	case FailMCPUnavailable:
		return []string{"Confirm the deployed MCP server is reachable and the token is authorized."}
	default:
		return []string{"Re-run: eshu hosted-setup"}
	}
}

// tokenReference returns a display-safe reference for the resolved bearer
// token, never the raw value. It reuses the #1767 mcpsetup.TokenReference/
// mcpsetup.RedactToken helpers: when a platform supports env-var references it
// returns ${ESHU_API_KEY}; otherwise it returns a masked placeholder so the
// operator can recognize which key is configured without exposing it. `eshu
// hosted-setup` connects using this resolved ESHU_API_KEY (the shared admin/dev
// credential, issue #1767) regardless of the deployment's F-8 auth posture, so
// this always references mcpsetup.APIKeyEnvVar -- it is showing the credential
// the command actually used to connect, not re-deriving a posture.
func tokenReference(platform, apiKey string) string {
	if strings.TrimSpace(apiKey) == "" {
		return ""
	}
	if p, err := mcpsetup.ResolvePlatform(platform); err == nil {
		return mcpsetup.TokenReference(p, mcpsetup.APIKeyEnvVar, apiKey)
	}
	return mcpsetup.RedactToken(apiKey)
}

// setupHint renders a hosted MCP setup snippet for the requested platform
// by delegating to the #1767 mcpsetup.RenderSetupSnippet helper. It returns an
// empty string when no platform was requested or the platform is unknown. The
// snippet is pinned to shared-key posture: this command authenticates via the
// resolved ESHU_API_KEY (see tokenReference), so the hint it shows must
// match the credential it actually used, independent of the deployment's F-8
// auto-detected auth posture.
func setupHint(platform, serviceURL, apiKey string) string {
	if strings.TrimSpace(platform) == "" {
		return ""
	}
	p, err := mcpsetup.ResolvePlatform(platform)
	if err != nil {
		return ""
	}
	req := mcpsetup.SetupRequest{
		Mode:       mcpsetup.ModeHostedHTTP,
		ServiceURL: serviceURL,
		APIKey:     apiKey,
		Posture:    mcpsetup.PostureSharedKey,
	}
	hint, err := mcpsetup.RenderSetupSnippet(p, req)
	if err != nil {
		return ""
	}
	return hint
}
