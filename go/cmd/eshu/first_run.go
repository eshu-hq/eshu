// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

func init() {
	firstRunCmd := &cobra.Command{
		Use:   "first-run [path]",
		Short: "Guided path from a checkout to one indexed repo and one answer",
		Long: `first-run walks the smallest truthful path from an Eshu checkout or
installed binary to a single indexed repository, a readiness proof, and one
bounded API-backed answer.

It detects the runtime shape (reachable API, local binaries, or Docker
Compose), verifies the runtime is usable, indexes the target repository (or
reuses an already-indexed one), waits for indexing completeness rather than
process health, and runs one bounded query before reporting success.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runFirstRun,
	}
	addFirstRunFlags(firstRunCmd)
	addRemoteFlags(firstRunCmd)
	firstRunCmd.AddCommand(newFirstRunReportCmd())
	rootCmd.AddCommand(firstRunCmd)
}

// addFirstRunFlags registers the first-run specific flags.
func addFirstRunFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the first-run result as a canonical JSON envelope")
	cmd.Flags().Bool("no-start", false, "Never attempt to start a runtime; only verify and report")
	cmd.Flags().Duration("timeout", 15*time.Minute, "Maximum time to spend proving indexing readiness")
	cmd.Flags().Duration("poll-interval", 3*time.Second, "Readiness polling interval")
	cmd.Flags().Bool("report", false, "Print a redacted first-run evidence summary after the run")
	cmd.Flags().String("report-format", "md", "Evidence artifact format for --report-out: md or json")
	cmd.Flags().String("report-out", "", "Write a redacted first-run evidence artifact to this path")
}

// runFirstRun is the cobra entry point. It resolves every piece of process
// state the orchestration needs -- flags, the API client, the scan runtime,
// the selector matcher, and the config-backed MCP endpoint -- and delegates to
// firstrun.Execute so the orchestration stays unit-testable outside package
// main.
func runFirstRun(cmd *cobra.Command, args []string) error {
	opts, err := firstRunOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	client := apiClientFromCmd(cmd)

	root, rootErr := scan.ResolveTarget(opts.Path, "")
	deps := firstrun.Deps{
		Probe:              defaultFirstRunRuntimeProbe(),
		FetchStatus:        scan.FetchPipelineStatus,
		ListRepos:          firstRunListRepositories,
		RunScan:            scan.Execute,
		ReposDir:           scan.ReposDir,
		ScanRuntime:        scanRuntimeFor(client),
		MatchesSelector:    firstRunSelectorMatches,
		ResolveMCPEndpoint: resolveFirstRunMCPEndpoint,
	}
	if rootErr != nil {
		deps.WorkspaceError = rootErr
	} else {
		deps.WorkspaceRoot = root.Root
	}

	result, runErr := firstrun.Execute(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), client, client.BaseURL, deps, opts)
	return finishFirstRun(cmd, opts, result, runErr)
}

// firstRunOptionsFromCommand parses and validates flags.
func firstRunOptionsFromCommand(cmd *cobra.Command, args []string) (firstrun.Options, error) {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")
	noStart, _ := cmd.Flags().GetBool("no-start")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	profile, _ := cmd.Flags().GetString("profile")
	report, _ := cmd.Flags().GetBool("report")
	reportFormat, _ := cmd.Flags().GetString("report-format")
	reportOut, _ := cmd.Flags().GetString("report-out")
	if timeout <= 0 {
		return firstrun.Options{}, fmt.Errorf("timeout must be greater than zero")
	}
	if pollInterval <= 0 {
		return firstrun.Options{}, fmt.Errorf("poll-interval must be greater than zero")
	}
	if reportOut != "" {
		if _, err := firstrun.NormalizeEvidenceFormat(reportFormat); err != nil {
			return firstrun.Options{}, err
		}
	}
	return firstrun.Options{
		Path:         path,
		JSON:         jsonOutput,
		NoStart:      noStart,
		Timeout:      timeout,
		PollInterval: pollInterval,
		Profile:      profile,
		Report:       report,
		ReportFormat: reportFormat,
		ReportOut:    reportOut,
	}, nil
}

// defaultFirstRunRuntimeProbe returns the production probe backed by a bounded
// HTTP client, exec.LookPath, and os.Stat.
func defaultFirstRunRuntimeProbe() firstrun.RuntimeProbe {
	return firstrun.RuntimeProbe{
		APIHealthy: firstrun.APIHealthy,
		LookPath:   exec.LookPath,
		FileExists: pathExists,
	}
}

// firstRunListRepositories is the default repositories query seam. It owns the
// wire decode because the repository-list response type is shared by thirteen
// command families in this package, and it copies the entries into the
// firstrun package's plain-value model at the boundary.
func firstRunListRepositories(client scan.Client) (firstrun.RepositoryList, error) {
	var response repositoryListResponse
	if err := client.Get(firstrun.QueryEndpoint, &response); err != nil {
		return firstrun.RepositoryList{}, err
	}
	list := firstrun.RepositoryList{}
	for _, repo := range response.Repositories {
		list.Repositories = append(list.Repositories, firstrun.Repository{
			ID:        repo.ID,
			Name:      repo.Name,
			Path:      repo.Path,
			LocalPath: repo.LocalPath,
			RepoSlug:  repo.RepoSlug,
		})
	}
	return list, nil
}

// firstRunSelectorMatches adapts the shared repository selector matcher to the
// firstrun package's plain-value repository model.
func firstRunSelectorMatches(repo firstrun.Repository, selector string) bool {
	return repositorySelectorMatches(repositorySelectorEntry{
		ID:        repo.ID,
		Name:      repo.Name,
		Path:      repo.Path,
		LocalPath: repo.LocalPath,
		RepoSlug:  repo.RepoSlug,
	}, selector)
}

// finishFirstRun renders the result as JSON or human text and returns runErr so
// the exit code reflects the truthful outcome. When an evidence report was
// requested it also emits a redacted summary and/or writes a redacted artifact;
// a report failure is reported but never masks the run's own outcome.
func finishFirstRun(cmd *cobra.Command, opts firstrun.Options, result firstrun.Result, runErr error) error {
	if result.Truth == nil {
		result.Truth = firstrun.Truth(result, opts.Profile)
	}
	if opts.JSON {
		envelope := map[string]any{
			"data":  result,
			"truth": result.Truth,
			"error": nil,
		}
		if runErr != nil {
			envelope["error"] = map[string]any{"message": runErr.Error()}
		}
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		emitFirstRunEvidence(cmd, opts, result)
		return runErr
	}
	firstrun.RenderHuman(cmd.OutOrStdout(), result, runErr)
	emitFirstRunEvidence(cmd, opts, result)
	return runErr
}

// emitFirstRunEvidence prints the redacted evidence summary and/or writes the
// redacted artifact when requested. With JSON output the summary goes to stderr
// so the canonical envelope on stdout stays parseable. Any error is reported on
// stderr without overriding the run's truthful exit code.
func emitFirstRunEvidence(cmd *cobra.Command, opts firstrun.Options, result firstrun.Result) {
	if !opts.Report && opts.ReportOut == "" {
		return
	}
	report := firstrun.BuildEvidence(result, &firstrun.EvidenceInputs{
		MCPEndpoint: resolveFirstRunMCPEndpoint(),
		Profile:     opts.Profile,
	})
	if opts.Report {
		summaryOut := cmd.OutOrStdout()
		if opts.JSON {
			summaryOut = cmd.ErrOrStderr()
		}
		firstrun.RenderEvidenceTerminal(summaryOut, report)
	}
	if opts.ReportOut != "" {
		if err := firstrun.WriteEvidenceArtifact(report, opts.ReportFormat, opts.ReportOut); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evidence artifact: %v\n", err)
			return
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote first-run evidence to %s\n", opts.ReportOut)
	}
}

// resolveFirstRunMCPEndpoint reads a configured MCP endpoint from the
// environment or config so the API-vs-MCP heuristic can flag a misrouted URL.
// An empty result means no endpoint is configured and the heuristic is skipped.
func resolveFirstRunMCPEndpoint() string {
	if value := strings.TrimSpace(resolveConfigValue("ESHU_MCP_URL", "")); value != "" {
		return value
	}
	return strings.TrimSpace(resolveConfigValue("ESHU_MCP_ENDPOINT", ""))
}
