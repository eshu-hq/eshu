// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
	"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench"
	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/spf13/cobra"
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
	deps := firstRunDeps(client)
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

// firstRunDeps builds the production seam set runFirstRun hands to
// firstrun.Execute. It is the single construction site for those seams so a
// test can prove the wiring — in particular that Deps.ResolveMCPEndpoint
// carries the config-backed resolver the mcp_endpoint_is_api diagnostic
// depends on. Workspace resolution stays in runFirstRun because it needs the
// command's path argument.
func firstRunDeps(client *APIClient) firstrun.Deps {
	return firstrun.Deps{
		Probe:              defaultFirstRunRuntimeProbe(),
		FetchStatus:        scan.FetchPipelineStatus,
		ListRepos:          firstRunListRepositories,
		RunScan:            scan.Execute,
		ReposDir:           scan.ReposDir,
		ScanRuntime:        scanRuntimeFor(client),
		MatchesSelector:    firstRunSelectorMatches,
		ResolveMCPEndpoint: resolveFirstRunMCPEndpoint,
	}
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

// firstRunListRepositories is the default repositories query seam. It decodes
// into the shared reposelector.ListResponse wire shape and copies the entries
// into the firstrun package's plain-value model at the boundary, so firstrun
// never has to name the API payload.
func firstRunListRepositories(client scan.Client) (firstrun.RepositoryList, error) {
	var response reposelector.ListResponse
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

// firstRunSelectorMatches adapts reposelector's matching rules to the firstrun
// package's plain-value repository model. first-run has no selector flag: the
// selector is the workspace root it resolved, matched against the indexed
// repositories to decide whether an existing index is reusable
// (firstrun.firstRunRepoMatchesTarget). Routing it through reposelector means
// that match canonicalizes paths and resolves symlinks the same way an
// explicit --repo selector would.
func firstRunSelectorMatches(repo firstrun.Repository, selector string) bool {
	return reposelector.Matches(reposelector.Entry{
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
	if value := strings.TrimSpace(cliconfig.ResolveValue("ESHU_MCP_URL", "")); value != "" {
		return value
	}
	return strings.TrimSpace(cliconfig.ResolveValue("ESHU_MCP_ENDPOINT", ""))
}

func init() {
	rootCmd.AddCommand(newFirstRunBenchmarkCommand())
}

// newFirstRunBenchmarkCommand builds a fresh first-run-benchmark command. A
// constructor (rather than a package-level singleton) keeps each invocation,
// including tests, free of leaked flag state.
func newFirstRunBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "first-run-benchmark",
		Short: "Score a first-run --json envelope against the onboarding benchmark",
		Long: `first-run-benchmark scores the canonical envelope emitted by
"eshu first-run --json" against the first-five-minutes onboarding success
criteria from issue #1772.

It reads the envelope from a file (--envelope) or stdin, evaluates whether a
new user reached one useful answer with bounded evidence, and prints a
scorecard. The benchmark FAILS (non-zero exit) when the "first answer" comes
from health-only status rather than a completed indexing and bounded query
proof: a missing query answer, missing truth metadata, missing source handle,
incomplete indexing, or an error envelope all reject the run.

Typical use:

  eshu first-run --json > /tmp/first-run.json
  eshu first-run-benchmark --envelope /tmp/first-run.json --path local_binary`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runFirstRunBenchmark,
	}
	cmd.Flags().String("envelope", "", "Path to a first-run --json envelope (default: read stdin)")
	cmd.Flags().String("path", "local_binary", "Onboarding path label: local_binary, local_compose, or hosted")
	cmd.Flags().Int("manual-steps", firstrunbench.NotMeasuredManualSteps, "Declared manual copy/paste step count for this path (negative = not declared)")
	cmd.Flags().Bool("json", false, "Emit the scorecard as JSON")
	return cmd
}

// runFirstRunBenchmark reads the envelope, scores it, prints the scorecard, and
// returns a non-zero exit (via error) when the benchmark verdict is FAIL so the
// health-only-rejection invariant is enforced at the process boundary.
func runFirstRunBenchmark(cmd *cobra.Command, _ []string) error {
	envelopePath, _ := cmd.Flags().GetString("envelope")
	pathLabel, _ := cmd.Flags().GetString("path")
	manualSteps, _ := cmd.Flags().GetInt("manual-steps")
	jsonOut, _ := cmd.Flags().GetBool("json")

	raw, err := firstrunbench.ReadEnvelope(cmd.InOrStdin(), envelopePath)
	if err != nil {
		return err
	}
	env, err := firstrun.ParseEnvelope(raw)
	if err != nil {
		return err
	}

	verdict := firstrunbench.Evaluate(env, firstrunbench.Measurements{
		Path:        pathLabel,
		ManualSteps: manualSteps,
	})

	if jsonOut {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), verdict); writeErr != nil {
			return writeErr
		}
	} else {
		firstrunbench.RenderVerdict(cmd.OutOrStdout(), verdict)
	}
	if !verdict.Pass {
		return fmt.Errorf("first-answer benchmark FAILED: %s", strings.Join(verdict.FailureReasons(), "; "))
	}
	return nil
}

// evidenceEnvelopeMaxBytes bounds how much of a saved envelope the report
// subcommand reads, so a malformed or hostile stream cannot exhaust memory.
const evidenceEnvelopeMaxBytes = 8 << 20 // 8 MiB

// newFirstRunReportCmd builds the `eshu first-run report` subcommand. It renders
// a redacted evidence artifact from a saved `eshu first-run --json` envelope so
// an operator can regenerate the support packet without re-running onboarding.
func newFirstRunReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Render a redacted first-run evidence artifact from a saved --json envelope",
		Long: `report renders a redacted first-run evidence artifact (Markdown or JSON)
from a previously captured 'eshu first-run --json' envelope. It re-uses the
result first-run already computed and never re-runs indexing or queries, so it
is safe to run offline against a saved envelope.

Every endpoint, path, and free-text field is scrubbed before it is rendered: an
embedded 'user:password@' credential and a credential-shaped query parameter are
both replaced, and an absolute path is reduced to its final element. A secret in
a URL path segment, or written as bare prose with no key beside it, is not
detected. See docs/public/reference/first-run-evidence.md for the full limits.`,
		Args: cobra.NoArgs,
		RunE: runFirstRunReport,
	}
	cmd.Flags().String("from", "", "Path to a saved 'eshu first-run --json' envelope (defaults to stdin)")
	cmd.Flags().String("format", "md", "Artifact format: md or json")
	cmd.Flags().String("out", "", "Write the artifact to this path instead of stdout")
	return cmd
}

// runFirstRunReport reads the saved envelope, projects it into the evidence
// report, and renders it in the requested format.
func runFirstRunReport(cmd *cobra.Command, _ []string) error {
	from, _ := cmd.Flags().GetString("from")
	format, _ := cmd.Flags().GetString("format")
	out, _ := cmd.Flags().GetString("out")

	raw, err := readEvidenceEnvelope(cmd, from)
	if err != nil {
		return err
	}
	result, err := firstRunResultFromEnvelope(raw)
	if err != nil {
		return err
	}
	report := firstrun.BuildEvidence(result, nil)
	if out != "" {
		if err := firstrun.WriteEvidenceArtifact(report, format, out); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote first-run evidence to %s\n", out)
		return nil
	}
	data, err := firstrun.RenderEvidenceArtifact(report, format)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

// readEvidenceEnvelope reads the saved envelope bytes from a path, or from stdin
// when no path is given.
func readEvidenceEnvelope(cmd *cobra.Command, path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), evidenceEnvelopeMaxBytes))
		if err != nil {
			return nil, fmt.Errorf("read first-run envelope from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag pointing to a local first-run envelope file, not an HTTP request param
	if err != nil {
		return nil, fmt.Errorf("read first-run envelope: %w", err)
	}
	return data, nil
}

// firstRunResultFromEnvelope decodes a saved first-run envelope into a result,
// restoring the truth metadata onto the result so the rendered report carries
// it. It decodes through firstrun.ParseEnvelope, the one canonical envelope
// contract, so the evidence report and the onboarding benchmark consume the
// same persisted shape. The envelope must be the object emitted by
// 'eshu first-run --json'.
func firstRunResultFromEnvelope(raw []byte) (firstrun.Result, error) {
	envelope, err := firstrun.ParseEnvelope(raw)
	if err != nil {
		return firstrun.Result{}, err
	}
	if strings.TrimSpace(envelope.Data.Command) == "" {
		return firstrun.Result{}, fmt.Errorf("first-run envelope is missing its data block")
	}
	result := envelope.Data
	if result.Truth == nil {
		result.Truth = envelope.Truth
	}
	return result, nil
}
