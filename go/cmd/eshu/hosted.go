// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/hosted"
	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
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

// hostedRepositoryList runs the bounded repositories query and projects it
// into the minimal view the hosted checks read. Each entry keeps a selector predicate
// bound to reposelector's matching rules, so a --repository scope check
// resolves paths and symlinks the same way every other command does.
func hostedRepositoryList(client *APIClient) (hosted.RepositoryList, error) {
	var response reposelector.ListResponse
	if err := client.Get(hosted.ReposPath, &response); err != nil {
		return hosted.RepositoryList{}, err
	}
	list := hosted.RepositoryList{Repositories: make([]hosted.Repository, 0, len(response.Repositories))}
	for _, repo := range response.Repositories {
		list.Repositories = append(list.Repositories, hosted.Repository{
			ID:         repo.ID,
			Name:       repo.Name,
			ScopeMatch: func(selector string) bool { return reposelector.Matches(repo, selector) },
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

func init() {
	onboardCmd := &cobra.Command{
		Use:   "hosted-onboard",
		Short: "Onboard a project team and repository set onto a hosted Eshu service",
		Long: `hosted-onboard captures a hosted onboarding workflow for a shared-service
team: a team name, a narrow repository sync rule set, the client endpoint, the
token SOURCE NAME (never the value), the initial indexing state, and a first
MCP/API answer.

It validates that the repository rules are narrow. A whole-org glob such as
'org/*' is rejected as accidental org-wide ingestion unless you pass
--confirm-broad. It then reuses the hosted-setup staged connection checks
(/healthz, /readyz, index readiness, MCP tools, one bounded query) and emits a
redacted onboarding artifact safe to hand to the project team.

The artifact records the API URL, MCP URL, token source name, indexed
repositories, queue/completeness status, starter prompts, and structured starter
playbooks with IDs, ordered tools, and expected truth classes. It never embeds a
bearer token value and references the scoped per-team token the operator should
register for this team's repository scope, noting that the fallback shared token
stays broad until that scoped token is provisioned.`,
		Args: cobra.NoArgs,
		RunE: runHostedOnboard,
	}
	onboardCmd.Flags().String("team", "", "Owning project team name (required)")
	onboardCmd.Flags().StringArray("repo", nil, "Repository to onboard by exact owner/name (repeatable)")
	onboardCmd.Flags().StringArray("repo-pattern", nil, "Repository selector regex, e.g. '^org/team-' (repeatable)")
	onboardCmd.Flags().Bool("confirm-broad", false, "Explicitly confirm a broad, org-wide repository rule set")
	onboardCmd.Flags().String("require-repository", "", "Require this repository to be present in the indexed scope")
	onboardCmd.Flags().Bool("json", false, "Write the onboarding artifact as JSON instead of the terminal summary")
	onboardCmd.Flags().String("out", "", "Write the redacted onboarding artifact to this path")
	onboardCmd.Flags().String("format", "md", "Artifact format for --out: md or json")
	onboardCmd.Flags().String("platform", "", "Emit a hosted MCP setup snippet for this assistant client: "+strings.Join(mcpsetup.SupportedPlatformNames(), ", "))
	addRemoteFlags(onboardCmd)
	rootCmd.AddCommand(onboardCmd)
}

// runHostedOnboard is the cobra entry point. It resolves flags into validated
// options, wires the production hosted-setup seams, runs the onboarding
// workflow, and renders or writes the redacted artifact.
func runHostedOnboard(cmd *cobra.Command, _ []string) error {
	client := apiClientFromCmd(cmd)
	opts, err := hostedOnboardOptionsFromCmd(cmd, client)
	if err != nil {
		return err
	}

	artifact, runErr := hosted.ExecuteOnboard(hostedSetupDeps(client), opts)
	if writeErr := finishHostedOnboard(cmd, artifact, runErr); writeErr != nil {
		return writeErr
	}
	return runErr
}

// hostedOnboardOptionsFromCmd resolves and validates the command flags into
// onboarding options, parsing and compiling repository rules up front so a
// malformed rule fails before any connection attempt.
func hostedOnboardOptionsFromCmd(cmd *cobra.Command, client *APIClient) (hosted.OnboardOptions, error) {
	team, _ := cmd.Flags().GetString("team")
	repos, _ := cmd.Flags().GetStringArray("repo")
	patterns, _ := cmd.Flags().GetStringArray("repo-pattern")
	confirmBroad, _ := cmd.Flags().GetBool("confirm-broad")
	requireRepo, _ := cmd.Flags().GetString("require-repository")
	platform, _ := cmd.Flags().GetString("platform")

	rawRules := make([]string, 0, len(repos)+len(patterns))
	for _, repo := range repos {
		rawRules = append(rawRules, "repo:"+repo)
	}
	for _, pattern := range patterns {
		rawRules = append(rawRules, "pattern:"+pattern)
	}
	rules, err := hosted.ParseRepoRules(rawRules)
	if err != nil {
		return hosted.OnboardOptions{}, err
	}

	return hosted.OnboardOptions{
		ServiceURL:   client.BaseURL,
		APIKey:       client.APIKey,
		Team:         team,
		Platform:     platform,
		Rules:        rules,
		Repository:   requireRepo,
		ConfirmBroad: confirmBroad,
	}, nil
}

// finishHostedOnboard renders the artifact to the terminal or as JSON, and
// optionally writes a redacted artifact file. It returns only a write/encoding
// error; the truthful connection outcome is returned by the caller so the exit
// code reflects whether the bounded query actually returned.
func finishHostedOnboard(cmd *cobra.Command, artifact hosted.Artifact, runErr error) error {
	out, _ := cmd.Flags().GetString("out")
	if strings.TrimSpace(out) != "" {
		format, _ := cmd.Flags().GetString("format")
		if err := hosted.WriteArtifact(artifact, format, out); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote hosted onboarding artifact to %s\n", out)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		data, err := hosted.RenderArtifactJSON(artifact)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}
	hosted.RenderArtifactTerminal(cmd.OutOrStdout(), artifact, runErr)
	return nil
}
