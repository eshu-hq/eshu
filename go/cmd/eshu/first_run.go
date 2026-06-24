// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// firstRunQueryEndpoint is the bounded, API-backed query the command runs to
// prove a useful answer is reachable. Listing repositories with a small limit
// is the smallest truthful end-to-end probe.
const firstRunQueryEndpoint = "/api/v0/repositories?limit=5"

// firstRunDeps groups the injectable seams used by the orchestration so each
// step is unit-testable with fakes. Production wiring lives in runFirstRun.
type firstRunDeps struct {
	Probe          firstRunRuntimeProbe
	FetchStatus    func(client *APIClient) (scanPipelineStatus, error)
	ListRepos      func(client *APIClient) (repositoryListResponse, error)
	RunScan        func(ctx context.Context, stdout, stderr io.Writer, client *APIClient, opts scanOptions, announce bool) (scanResult, error)
	ReposDir       func(root string) (string, error)
	WorkspaceRoot  string
	WorkspaceError error
}

// firstRunOptions captures the resolved command flags.
type firstRunOptions struct {
	Path         string
	JSON         bool
	NoStart      bool
	Timeout      time.Duration
	PollInterval time.Duration
	Profile      string
	// Report enables the terminal evidence summary in addition to the normal
	// human or JSON output.
	Report bool
	// ReportFormat selects the artifact format ("md" or "json") for ReportOut.
	ReportFormat string
	// ReportOut is the optional path the redacted evidence artifact is written
	// to. An empty value writes no artifact.
	ReportOut string
}

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

// runFirstRun is the cobra entry point. It wires production seams and delegates
// to executeFirstRun so the orchestration stays unit-testable.
func runFirstRun(cmd *cobra.Command, args []string) error {
	opts, err := firstRunOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	client := apiClientFromCmd(cmd)

	root, rootErr := resolveScanTarget(opts.Path, "")
	deps := firstRunDeps{
		Probe:       defaultFirstRunRuntimeProbe(),
		FetchStatus: scanFetchPipelineStatus,
		ListRepos:   firstRunListRepositories,
		RunScan:     executeScan,
		ReposDir:    scanReposDir,
	}
	if rootErr != nil {
		deps.WorkspaceError = rootErr
	} else {
		deps.WorkspaceRoot = root.Root
	}

	result, runErr := executeFirstRun(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), client, deps, opts)
	return finishFirstRun(cmd, opts, result, runErr)
}

// firstRunOptionsFromCommand parses and validates flags.
func firstRunOptionsFromCommand(cmd *cobra.Command, args []string) (firstRunOptions, error) {
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
		return firstRunOptions{}, fmt.Errorf("timeout must be greater than zero")
	}
	if pollInterval <= 0 {
		return firstRunOptions{}, fmt.Errorf("poll-interval must be greater than zero")
	}
	if reportOut != "" {
		if _, err := normalizeEvidenceFormat(reportFormat); err != nil {
			return firstRunOptions{}, err
		}
	}
	return firstRunOptions{
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

// executeFirstRun runs the ordered, individually-testable steps and returns the
// canonical result. It never reports success unless the final bounded query
// actually returned an answer.
func executeFirstRun(
	parentCtx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	client *APIClient,
	deps firstRunDeps,
	opts firstRunOptions,
) (firstRunResult, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	result := newFirstRunResult(client.BaseURL)
	result.RepoTarget = strings.TrimSpace(deps.WorkspaceRoot)

	// Step 1: detect runtime shape.
	detection := detectFirstRunRuntime(deps.Probe, client.BaseURL, deps.WorkspaceRoot)
	result.RuntimeShape = detection.Shape
	result = result.addStep("detect runtime", firstRunStepOK, detection.Detail)

	// Step 2: verify the runtime is usable (no destructive auto-start).
	verifyStep := verifyFirstRunRuntime(deps.Probe, detection, client.BaseURL, opts.NoStart)
	result = result.addStep(verifyStep.Name, verifyStep.Status, verifyStep.Detail)
	if verifyStep.Status == firstRunStepFailed {
		verifyErr := fmt.Errorf("verify runtime: %s", verifyStep.Detail)
		result = attachFirstRunDiagnostic(result, firstRunVerifySignal(deps, detection, client.BaseURL, verifyErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, verifyErr
	}

	// Step 3: index the target repository (or reuse an existing index).
	indexed, runErr := ensureFirstRunIndexed(parentCtx, stdout, stderr, client, deps, opts)
	result.RepoIndexed = indexed.Completeness
	result.Readiness = indexed.Readiness
	result = result.addStep("index repository", indexed.Status, indexed.Detail)
	if runErr != nil {
		result = result.addStep("wait for readiness", firstRunStepFailed, runErr.Error())
		result = attachFirstRunDiagnostic(result, firstRunReadinessSignal(deps, client, detection, indexed, runErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, runErr
	}
	result = result.addStep("wait for readiness", firstRunStepOK, indexed.Readiness)

	// Step 4: run one bounded API-backed query as the truthful end proof.
	answer, queryErr := runFirstRunQuery(deps, client)
	if queryErr != nil {
		result = result.addStep("first query", firstRunStepFailed, queryErr.Error())
		result = attachFirstRunDiagnostic(result, firstRunQuerySignal(queryErr))
		result.NextSteps = firstRunNextSteps(result, detection)
		return result, fmt.Errorf("first query: %w", queryErr)
	}
	result.QueryAnswered = true
	result.QuerySummary = answer
	result = result.addStep("first query", firstRunStepOK, answer)
	// A successful query that found zero repositories is truthful success, but the
	// operator has nothing to query yet. Attach the empty-index advisory so the
	// next action is clear without marking the run failed.
	if isEmptyRepositoriesAnswer(answer) {
		result = attachFirstRunDiagnostic(result, firstRunEmptyRepoSignal())
	}
	result.NextSteps = firstRunNextSteps(result, detection)
	return result, nil
}

// firstRunIndexOutcome captures the readiness and completeness of the index
// step in a form the summary can render truthfully.
type firstRunIndexOutcome struct {
	Status       firstRunStepStatus
	Detail       string
	Completeness string
	Readiness    string
}

// firstRunListRepositories is the default repositories query seam.
func firstRunListRepositories(client *APIClient) (repositoryListResponse, error) {
	var response repositoryListResponse
	if err := client.Get(firstRunQueryEndpoint, &response); err != nil {
		return repositoryListResponse{}, err
	}
	return response, nil
}

// runFirstRunQuery runs the bounded repositories query and returns a concise
// human summary of the answer. An error is returned only when the query did not
// return; an empty repository list is a valid, truthful answer.
func runFirstRunQuery(deps firstRunDeps, client *APIClient) (string, error) {
	if deps.ListRepos == nil {
		return "", errors.New("repositories query seam is not configured")
	}
	response, err := deps.ListRepos(client)
	if err != nil {
		return "", err
	}
	count := len(response.Repositories)
	if count == 0 {
		return "repositories query returned 0 repositories", nil
	}
	first := strings.TrimSpace(response.Repositories[0].Name)
	if first == "" {
		first = strings.TrimSpace(response.Repositories[0].ID)
	}
	return fmt.Sprintf("repositories query returned %d (e.g. %s)", count, first), nil
}

// firstRunNextSteps builds actionable follow-ups tailored to the outcome.
func firstRunNextSteps(result firstRunResult, detection firstRunRuntimeDetection) []string {
	if result.succeeded() {
		return []string{
			fmt.Sprintf("Ask a deeper question: eshu story %s", quoteIfEmpty(result.RepoTarget)),
			"List everything indexed: eshu list",
		}
	}
	switch detection.Shape {
	case firstRunShapeLocalBinaries:
		return []string{
			"Start the local API: eshu api start",
			"Re-run: eshu first-run",
		}
	case firstRunShapeDockerCompose:
		return []string{
			"Start the stack: docker compose up -d",
			"Re-run: eshu first-run",
		}
	case firstRunShapeUnknown:
		return []string{
			"Build the binaries: cd go && make build && export PATH=$PATH:$(pwd)/bin",
			"Or start Docker Compose, then re-run: eshu first-run",
		}
	default:
		return []string{"Re-run: eshu first-run"}
	}
}

// quoteIfEmpty renders a placeholder for an empty repo target so the next-step
// hint stays copy-pasteable.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}

// finishFirstRun renders the result as JSON or human text and returns runErr so
// the exit code reflects the truthful outcome. When an evidence report was
// requested it also emits a redacted summary and/or writes a redacted artifact;
// a report failure is reported but never masks the run's own outcome.
func finishFirstRun(cmd *cobra.Command, opts firstRunOptions, result firstRunResult, runErr error) error {
	if result.Truth == nil {
		result.Truth = firstRunTruth(result, opts.Profile)
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
	renderFirstRunHuman(cmd.OutOrStdout(), result, runErr)
	emitFirstRunEvidence(cmd, opts, result)
	return runErr
}

// emitFirstRunEvidence prints the redacted evidence summary and/or writes the
// redacted artifact when requested. With JSON output the summary goes to stderr
// so the canonical envelope on stdout stays parseable. Any error is reported on
// stderr without overriding the run's truthful exit code.
func emitFirstRunEvidence(cmd *cobra.Command, opts firstRunOptions, result firstRunResult) {
	if !opts.Report && opts.ReportOut == "" {
		return
	}
	report := buildFirstRunEvidence(result, &firstRunEvidenceInputs{
		MCPEndpoint: resolveFirstRunMCPEndpoint(),
		Profile:     opts.Profile,
	})
	if opts.Report {
		summaryOut := cmd.OutOrStdout()
		if opts.JSON {
			summaryOut = cmd.ErrOrStderr()
		}
		renderEvidenceTerminal(summaryOut, report)
	}
	if opts.ReportOut != "" {
		if err := writeEvidenceArtifact(report, opts.ReportFormat, opts.ReportOut); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evidence artifact: %v\n", err)
			return
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote first-run evidence to %s\n", opts.ReportOut)
	}
}

// firstRunTruth labels the freshness and completeness of the first-run outcome
// using the same truth vocabulary as scan.
func firstRunTruth(result firstRunResult, profile string) map[string]any {
	freshness := "stale"
	completeness := "partial"
	if result.succeeded() && result.RepoIndexed == "complete" {
		freshness = "current"
		completeness = "complete"
	}
	return scanTruth(freshness, completeness, profile, currentGraphBackend())
}
