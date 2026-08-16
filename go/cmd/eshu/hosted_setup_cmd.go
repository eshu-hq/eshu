// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/hosted"
	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/spf13/cobra"
)

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
	hostedCmd.Flags().String("platform", "", "Emit a hosted MCP setup snippet for this assistant client: "+strings.Join(mcpsetup.SupportedPlatformNames(), ", "))
	hostedCmd.Flags().String("repository", "", "Require this repository to be present in the indexed scope")
	addRemoteFlags(hostedCmd)
	rootCmd.AddCommand(hostedCmd)
}

// runHostedSetup is the cobra entry point. It resolves flags into options, wires
// the production transport seams, and delegates the staged checks to
// hosted.ExecuteSetup so the orchestration stays testable outside the binary.
func runHostedSetup(cmd *cobra.Command, _ []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	platform, _ := cmd.Flags().GetString("platform")
	repository, _ := cmd.Flags().GetString("repository")

	client := apiClientFromCmd(cmd)
	opts := hosted.SetupOptions{
		ServiceURL: client.BaseURL,
		APIKey:     client.APIKey,
		Platform:   platform,
		Repository: repository,
	}

	result, runErr := hosted.ExecuteSetup(hostedSetupDeps(client), opts)
	return finishHostedSetup(cmd, jsonOutput, result, runErr)
}

// hostedSetupDeps wires the production transport seams the staged checks call.
// Each seam is bound to one resolved client so the hosted package never holds an
// HTTP client of its own.
func hostedSetupDeps(client *APIClient) hosted.Deps {
	return hosted.Deps{
		Health:    func() error { return hostedProbe(client, hosted.HealthzPath) },
		Ready:     func() error { return hostedProbe(client, hosted.ReadyzPath) },
		Readiness: func() (scan.ReadinessVerdict, error) { return hostedReadinessVerdict(client) },
		ListTools: mcp.ReadOnlyTools,
		ListRepos: func() (hosted.RepositoryList, error) { return hostedRepositoryList(client) },
	}
}

// hostedProbe issues a GET against path and returns nil when the deployed
// service answers without an error status.
func hostedProbe(client *APIClient, path string) error {
	if err := client.Get(path, nil); err != nil {
		return fmt.Errorf("%s probe %s failed: %w", path, client.BaseURL, err)
	}
	return nil
}

// hostedReadinessVerdict reads the bounded pipeline status from the deployed
// service and evaluates it with scan.EvaluateReadiness, the same readiness
// contract the local scan flow uses, so hosted and local agree on what
// "drained" means.
func hostedReadinessVerdict(client *APIClient) (scan.ReadinessVerdict, error) {
	var status scan.PipelineStatus
	if err := client.Get(hosted.StatusPath, &status); err != nil {
		return scan.ReadinessVerdict{}, err
	}
	return scan.EvaluateReadiness(status), nil
}

// hostedRepositoryList runs the bounded repositories query and projects it into
// the minimal view the hosted checks read. Each entry keeps a selector predicate
// bound to this CLI's repository-selector rules, so a --repository scope check
// resolves paths and symlinks the same way every other command does.
func hostedRepositoryList(client *APIClient) (hosted.RepositoryList, error) {
	var response repositoryListResponse
	if err := client.Get(hosted.ReposPath, &response); err != nil {
		return hosted.RepositoryList{}, err
	}
	list := hosted.RepositoryList{Repositories: make([]hosted.Repository, 0, len(response.Repositories))}
	for _, repo := range response.Repositories {
		list.Repositories = append(list.Repositories, hosted.Repository{
			ID:         repo.ID,
			Name:       repo.Name,
			ScopeMatch: func(selector string) bool { return repositorySelectorMatches(repo, selector) },
		})
	}
	return list, nil
}

// finishHostedSetup renders the result as JSON or human text and returns runErr
// so the exit code reflects the truthful outcome.
func finishHostedSetup(cmd *cobra.Command, jsonOutput bool, result hosted.Result, runErr error) error {
	if jsonOutput {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), hosted.SetupEnvelope(result, runErr)); writeErr != nil {
			return writeErr
		}
		return runErr
	}
	hosted.RenderSetupHuman(cmd.OutOrStdout(), result, runErr)
	return runErr
}
