// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/spf13/cobra"
)

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
	onboardCmd.Flags().String("platform", "", "Emit a hosted MCP setup snippet for this assistant client: "+strings.Join(supportedPlatformNames(), ", "))
	addRemoteFlags(onboardCmd)
	rootCmd.AddCommand(onboardCmd)
}

// runHostedOnboard is the cobra entry point. It resolves flags into validated
// options, wires the production hosted-setup seams, runs the onboarding
// workflow, and renders or writes the redacted artifact.
func runHostedOnboard(cmd *cobra.Command, _ []string) error {
	opts, err := hostedOnboardOptionsFromCmd(cmd)
	if err != nil {
		return err
	}

	client := apiClientFromCmd(cmd)
	deps := hostedSetupDeps{
		Health:      hostedProbe(hostedHealthzPath),
		Ready:       hostedProbe(hostedReadyzPath),
		FetchStatus: hostedFetchStatus,
		ListTools:   mcp.ReadOnlyTools,
		ListRepos:   hostedListRepositories,
	}

	artifact, runErr := executeHostedOnboard(client, deps, opts)
	if writeErr := finishHostedOnboard(cmd, artifact, runErr); writeErr != nil {
		return writeErr
	}
	return runErr
}

// hostedOnboardOptionsFromCmd resolves and validates the command flags into
// onboarding options, parsing and compiling repository rules up front so a
// malformed rule fails before any connection attempt.
func hostedOnboardOptionsFromCmd(cmd *cobra.Command) (hostedOnboardOptions, error) {
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
	rules, err := parseHostedRepoRules(rawRules)
	if err != nil {
		return hostedOnboardOptions{}, err
	}

	return hostedOnboardOptions{
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
func finishHostedOnboard(cmd *cobra.Command, artifact hostedOnboardArtifact, runErr error) error {
	out, _ := cmd.Flags().GetString("out")
	if strings.TrimSpace(out) != "" {
		format, _ := cmd.Flags().GetString("format")
		if err := writeHostedOnboardArtifact(artifact, format, out); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote hosted onboarding artifact to %s\n", out)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		data, err := renderHostedOnboardJSON(artifact)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}
	renderHostedOnboardTerminal(cmd.OutOrStdout(), artifact, runErr)
	return nil
}

// writeHostedOnboardArtifact renders the artifact in the requested format and
// writes it with owner-only permissions, since it still carries endpoint
// hostnames an operator may not want world-readable.
func writeHostedOnboardArtifact(artifact hostedOnboardArtifact, format, path string) error {
	normalized, err := normalizeEvidenceFormat(format)
	if err != nil {
		return err
	}
	var data []byte
	if normalized == evidenceFormatJSON {
		data, err = renderHostedOnboardJSON(artifact)
	} else {
		var markdown string
		markdown, err = renderHostedOnboardMarkdown(artifact)
		data = []byte(markdown)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
