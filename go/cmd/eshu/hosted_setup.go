// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/spf13/cobra"
)

// hostedSetupDeps groups the injectable seams used by the orchestration so each
// staged check is unit-testable with fakes. Production wiring lives in
// runHostedSetup.
type hostedSetupDeps struct {
	// Health probes the deployed /healthz liveness endpoint.
	Health func(*APIClient) error
	// Ready probes the deployed /readyz readiness endpoint; a 401/403 here is
	// classified as an authentication failure.
	Ready func(*APIClient) error
	// FetchStatus reads the bounded pipeline status for index classification.
	FetchStatus func(*APIClient) (scanPipelineStatus, error)
	// ListTools returns the visible MCP tool surface.
	ListTools func() []mcp.ToolDefinition
	// ListRepos runs the bounded repositories query used for scope enumeration
	// and as the final useful-answer proof.
	ListRepos func(*APIClient) (repositoryListResponse, error)
}

// hostedSetupOptions captures the resolved command flags.
type hostedSetupOptions struct {
	JSON       bool
	Platform   string
	Repository string
}

func init() {
	hostedCmd := &cobra.Command{
		Use:   "hosted-setup",
		Short: "First-five-minutes connection flow for a deployed Eshu service",
		Long: `hosted-setup verifies that an assistant can connect to a deployed Eshu
service. It resolves the endpoint and bearer token from flags, environment, or
safe config, then runs ordered, individually-reported checks: /healthz, /readyz
(which also proves authentication), status/index readiness, MCP tool visibility,
and one bounded query.

It reports the specific reason a connection is not yet usable -- unavailable
auth, an empty or stale index, a missing repository scope, partial readiness, or
an unavailable MCP surface -- instead of a single generic failure. It reports
connected only when the bounded query actually returns; health alone is never
success. The raw bearer token is never printed.

Pass --platform to also emit a hosted MCP setup snippet for an assistant
client. The snippet references the ESHU_API_KEY environment variable rather than
embedding the secret.`,
		Args: cobra.NoArgs,
		RunE: runHostedSetup,
	}
	hostedCmd.Flags().Bool("json", false, "Write the hosted-setup result as a canonical JSON envelope")
	hostedCmd.Flags().String("platform", "", "Emit a hosted MCP setup snippet for this assistant client: "+strings.Join(supportedPlatformNames(), ", "))
	hostedCmd.Flags().String("repository", "", "Require this repository to be present in the indexed scope")
	addRemoteFlags(hostedCmd)
	rootCmd.AddCommand(hostedCmd)
}

// runHostedSetup is the cobra entry point. It wires production seams and
// delegates to executeHostedSetup so the orchestration stays unit-testable.
func runHostedSetup(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	platform, _ := cmd.Flags().GetString("platform")
	repository, _ := cmd.Flags().GetString("repository")
	opts := hostedSetupOptions{JSON: jsonOutput, Platform: platform, Repository: repository}

	client := apiClientFromCmd(cmd)
	deps := hostedSetupDeps{
		Health:      hostedProbe(hostedHealthzPath),
		Ready:       hostedProbe(hostedReadyzPath),
		FetchStatus: hostedFetchStatus,
		ListTools:   mcp.ReadOnlyTools,
		ListRepos:   hostedListRepositories,
	}

	result, runErr := executeHostedSetup(client, deps, opts)
	return finishHostedSetup(cmd, opts, result, runErr)
}

// executeHostedSetup runs the ordered, individually-reported checks and returns
// the canonical result. It never reports connected unless the final bounded
// query actually returned an answer. Each failure is recorded with its specific
// category so the operator sees exactly which stage and why.
func executeHostedSetup(client *APIClient, deps hostedSetupDeps, opts hostedSetupOptions) (hostedSetupResult, error) {
	tokenRef := hostedTokenReference(opts.Platform, client.APIKey)
	result := newHostedSetupResult(client.BaseURL, tokenRef, strings.TrimSpace(opts.Platform), strings.TrimSpace(opts.Repository))
	result.SetupHint = hostedSetupHint(opts.Platform, client)

	// Stage 1: endpoint and auth resolved.
	if strings.TrimSpace(client.BaseURL) == "" {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageEndpoint, Status: hostedStageFailed,
			Detail:   "no deployed endpoint resolved; set --service-url or ESHU_SERVICE_URL",
			Category: hostedFailUnresolvedEndpoint,
		})
		return finalizeHostedSetup(result), errors.New("resolve endpoint: no deployed endpoint configured")
	}
	authDetail := "endpoint resolved; no bearer token supplied (set ESHU_API_KEY for an authenticated service)"
	if strings.TrimSpace(client.APIKey) != "" {
		authDetail = "endpoint and bearer token resolved (" + tokenRef + ")"
	}
	result = result.addStage(hostedSetupStage{Name: hostedStageEndpoint, Status: hostedStageOK, Detail: authDetail})

	// Stage 2: /healthz reachable.
	if err := deps.Health(client); err != nil {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageHealthz, Status: hostedStageFailed,
			Detail: err.Error(), Category: classifyProbeError(err),
		})
		return finalizeHostedSetup(result), fmt.Errorf("healthz: %w", err)
	}
	result = result.addStage(hostedSetupStage{Name: hostedStageHealthz, Status: hostedStageOK, Detail: "service answered /healthz"})

	// Stage 3: /readyz reachable (also proves authentication).
	if err := deps.Ready(client); err != nil {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageReadyz, Status: hostedStageFailed,
			Detail: err.Error(), Category: classifyProbeError(err),
		})
		return finalizeHostedSetup(result), fmt.Errorf("readyz: %w", err)
	}
	result = result.addStage(hostedSetupStage{Name: hostedStageReadyz, Status: hostedStageOK, Detail: "service answered /readyz; auth accepted"})

	// Stage 4: status/index readiness. The bounded repositories query doubles as
	// the indexed-scope source so the classifier can distinguish empty vs partial
	// vs stale.
	repos, reposErr := deps.ListRepos(client)
	if reposErr != nil {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageIndexReadiness, Status: hostedStageFailed,
			Detail: reposErr.Error(), Category: classifyReadError(reposErr),
		})
		return finalizeHostedSetup(result), fmt.Errorf("index readiness: %w", reposErr)
	}
	status, statusErr := deps.FetchStatus(client)
	if statusErr != nil {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageIndexReadiness, Status: hostedStageFailed,
			Detail: statusErr.Error(), Category: classifyProbeError(statusErr),
		})
		return finalizeHostedSetup(result), fmt.Errorf("index readiness: %w", statusErr)
	}
	category, detail, ready := classifyIndexReadiness(status, len(repos.Repositories))
	result.IndexState = hostedIndexStateLabel(category, ready)
	if !ready {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageIndexReadiness, Status: hostedStageWarn,
			Detail: detail, Category: category,
		})
		return finalizeHostedSetup(result), fmt.Errorf("index readiness: %s", category)
	}
	result = result.addStage(hostedSetupStage{Name: hostedStageIndexReadiness, Status: hostedStageOK, Detail: detail})

	// Stage 5: MCP tools visible.
	tools := hostedVisibleTools(deps.ListTools)
	result.ToolCount = len(tools)
	if len(tools) == 0 {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageMCPTools, Status: hostedStageFailed,
			Detail: "MCP tool surface is empty", Category: hostedFailMCPUnavailable,
		})
		return finalizeHostedSetup(result), errors.New("mcp tools: " + string(hostedFailMCPUnavailable))
	}
	result = result.addStage(hostedSetupStage{Name: hostedStageMCPTools, Status: hostedStageOK, Detail: fmt.Sprintf("%d tools visible", len(tools))})

	// Stage 6: one bounded query is the only stage whose success proves a useful
	// answer is reachable. A requested-but-missing repository scope is reported
	// distinctly from an empty result.
	if !repositoryScopePresent(repos, opts.Repository) {
		result = result.addStage(hostedSetupStage{
			Name: hostedStageQuery, Status: hostedStageFailed,
			Detail:   fmt.Sprintf("repository %q not present in indexed scope", strings.TrimSpace(opts.Repository)),
			Category: hostedFailMissingRepoScope,
		})
		return finalizeHostedSetup(result), fmt.Errorf("first query: %s", hostedFailMissingRepoScope)
	}
	result.QueryAnswered = true
	result.QuerySummary = hostedQuerySummary(repos)
	result = result.addStage(hostedSetupStage{Name: hostedStageQuery, Status: hostedStageOK, Detail: result.QuerySummary})
	return finalizeHostedSetup(result), nil
}

// hostedVisibleTools resolves the MCP tool surface, tolerating a nil seam.
func hostedVisibleTools(list func() []mcp.ToolDefinition) []mcp.ToolDefinition {
	if list == nil {
		return nil
	}
	return list()
}

// hostedQuerySummary renders a concise summary of the bounded query answer. An
// empty repository list is a valid, truthful answer once readiness has already
// confirmed at least one indexed repository.
func hostedQuerySummary(repos repositoryListResponse) string {
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

// hostedIndexStateLabel maps a readiness category to a human index-state label.
func hostedIndexStateLabel(category hostedFailCategory, ready bool) string {
	if ready {
		return "ready"
	}
	switch category {
	case hostedFailEmptyIndex:
		return "empty"
	case hostedFailStaleReadiness:
		return "stale"
	case hostedFailPartialReadiness:
		return "building"
	default:
		return "unknown"
	}
}

// finalizeHostedSetup attaches outcome-tailored next steps to the result.
func finalizeHostedSetup(result hostedSetupResult) hostedSetupResult {
	result.NextSteps = hostedNextSteps(result)
	return result
}

// hostedNextSteps builds actionable follow-ups tailored to the first failing
// stage so an operator can fix the right thing without reading every page.
func hostedNextSteps(result hostedSetupResult) []string {
	if result.connected() {
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
	case hostedFailUnresolvedEndpoint:
		return []string{"Set the deployed endpoint: export ESHU_SERVICE_URL=https://your-eshu-host", "Re-run: eshu hosted-setup"}
	case hostedFailAuthUnavailable:
		return []string{"Set a valid token: export ESHU_API_KEY=...", "Re-run: eshu hosted-setup"}
	case hostedFailUnreachable:
		return []string{"Confirm the deployed service URL and network reachability.", "Re-run: eshu hosted-setup"}
	case hostedFailEmptyIndex:
		return []string{"Index a repository on the deployed service, then re-run: eshu hosted-setup"}
	case hostedFailPartialReadiness:
		return []string{"Wait for indexing to drain on the deployed service, then re-run: eshu hosted-setup"}
	case hostedFailStaleReadiness:
		return []string{"Inspect the deployed pipeline status; clear failed or dead-letter work, then re-run."}
	case hostedFailMissingRepoScope:
		return []string{"Confirm the repository is indexed and your token's scope includes it.", "List visible repositories with a deployed query, then re-run."}
	case hostedFailMCPUnavailable:
		return []string{"Confirm the deployed MCP server is reachable and the token is authorized."}
	default:
		return []string{"Re-run: eshu hosted-setup"}
	}
}

// finishHostedSetup renders the result as JSON or human text and returns runErr
// so the exit code reflects the truthful outcome.
func finishHostedSetup(cmd *cobra.Command, opts hostedSetupOptions, result hostedSetupResult, runErr error) error {
	if opts.JSON {
		envelope := map[string]any{
			"data":  result,
			"truth": nil,
			"error": nil,
		}
		if runErr != nil {
			envelope["error"] = map[string]any{"message": runErr.Error()}
		}
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return runErr
	}
	renderHostedSetupHuman(cmd.OutOrStdout(), result, runErr)
	return runErr
}
