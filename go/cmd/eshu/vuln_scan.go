// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/reposelector"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/cli/vulnscan"
)

type vulnScanRepoOptions struct {
	Scan         scan.Options
	Limit        int
	ImpactStatus string
	RepoID       string
	Broad        bool
	ExportFormat string
}

type vulnScanRepoEnvelope struct {
	Data  vulnscan.Result     `json:"data"`
	Truth map[string]any      `json:"truth"`
	Error *vulnscan.RepoError `json:"error"`
}

func init() {
	vulnScanCmd := &cobra.Command{
		Use:   "vuln-scan",
		Short: "Run local vulnerability evidence workflows",
	}
	repoCmd := &cobra.Command{
		Use:   "repo [path]",
		Short: "Index a local repository and list reducer-owned vulnerability impact findings",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runVulnScanRepo,
	}
	addVulnScanRepoFlags(repoCmd)
	addRemoteFlags(repoCmd)
	providerParityCmd := &cobra.Command{
		Use:   "provider-parity",
		Short: "Compare provider alerts to Eshu findings with aggregate-only output",
		Args:  cobra.NoArgs,
		RunE:  runVulnScanProviderParity,
	}
	addVulnScanProviderParityFlags(providerParityCmd)
	addRemoteFlags(providerParityCmd)
	vulnScanCmd.AddCommand(repoCmd)
	vulnScanCmd.AddCommand(providerParityCmd)
	rootCmd.AddCommand(vulnScanCmd)
}

func addVulnScanRepoFlags(cmd *cobra.Command) {
	addScanFlags(cmd)
	cmd.Flags().Int("limit", 50, "Maximum vulnerability impact findings to return")
	cmd.Flags().String("impact-status", "", "Filter impact findings by reducer-owned impact status")
	cmd.Flags().String("repo-id", "", "Exact repository id to query after local scan readiness")
	cmd.Flags().Bool(
		"broad",
		false,
		"Skip the scoped fail-closed guards and accept advisory/package coverage beyond observed dependencies",
	)
	cmd.Flags().String("export", "", "Write a scanner report export format to stdout (supported: sarif, vex)")
}

func runVulnScanRepo(cmd *cobra.Command, args []string) error {
	startedAt := vulnscan.Now()
	opts, err := vulnScanRepoOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	client := apiClientFromCmd(cmd)
	var closeLocalRuntime func() error
	if !vulnScanHasConfiguredServiceURL(cmd) {
		localRuntime, err := vulnScanPrepareLocalRuntime(cmd.Context(), opts.Scan.Target.Root, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		if strings.TrimSpace(localRuntime.BaseURL) == "" {
			if localRuntime.Close != nil {
				_ = localRuntime.Close()
			}
			return fmt.Errorf("local vulnerability scan runtime did not return an API client")
		}
		client = NewAPIClient(localRuntime.BaseURL, "", "")
		opts.Scan.RuntimeEnv = localRuntime.BootstrapEnv
		closeLocalRuntime = localRuntime.Close
	}
	result := vulnscan.NewResult(
		vulnscan.Target{
			Path: opts.Scan.Target.Path,
			Root: opts.Scan.Target.Root,
			Kind: opts.Scan.Target.Kind,
		},
		opts.Limit,
		opts.Broad,
		client.BaseURL,
	)
	scanStdout := cmd.OutOrStdout()
	if opts.Scan.JSON || opts.ExportFormat != "" {
		scanStdout = cmd.ErrOrStderr()
	}
	scanResult, err := scan.Execute(cmd.Context(), scanStdout, cmd.ErrOrStderr(), scanRuntimeFor(client), opts.Scan, !opts.Scan.JSON)
	result.Scan = scanResult
	result.Status = scanResult.Status
	result.Warnings = append(result.Warnings, scanResult.Warnings...)
	if err != nil {
		result.ReadinessState = "target_incomplete"
		vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	if scanResult.Status != "ready" {
		result.ReadinessState = "target_incomplete"
		vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		err := commandExitError{message: "vulnerability scan target is not ready; rerun with --wait=true before reading findings", code: 4}
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}

	repositoryID, err := resolveVulnScanRepoID(client, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	result.RepositoryID = repositoryID

	findings, err := vulnscan.FetchImpactFindings(client, repositoryID, opts.Limit, opts.ImpactStatus)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	if findings.Error != nil {
		result.ReadinessState = "evidence_incomplete"
		vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
		err := commandExitError{message: findings.Error.Message, code: 4}
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, findings.Truth, err, closeLocalRuntime)
	}
	result.Findings = findings.Data.Findings
	result.Count = findings.Data.Count
	result.Limit = findings.Data.Limit
	result.Truncated = findings.Data.Truncated
	result.NextCursor = findings.Data.NextCursor
	result.Readiness = findings.Data.Readiness
	result.ReadinessState = vulnscan.ReadinessState(findings.Data.Readiness, result.Count)

	scopeErr := vulnScanExitError(vulnscan.ApplyScope(&result))
	vulnscan.RecordPerformance(&result, startedAt, opts.Scan.Target.Root)
	if scopeErr == nil {
		scopeErr = vulnScanExitError(vulnscan.ExitFailure(result))
	}
	return finishVulnScanRepoAfterCleanup(cmd, opts, result, findings.Truth, scopeErr, closeLocalRuntime)
}

// vulnScanExitError maps a vulnscan.Failure onto the CLI's exit-code contract.
// internal/cli/vulnscan classifies the failure and chooses the number;
// commandExitError is defined here, in package main, so the conversion has to
// happen here too. A nil failure stays nil, which is why the parameter is the
// concrete pointer type rather than error: a nil *Failure boxed into an error
// is not nil, and every caller here is asking "did this fail".
func vulnScanExitError(failure *vulnscan.Failure) error {
	if failure == nil {
		return nil
	}
	return commandExitError{message: failure.Message, code: failure.Code}
}

func vulnScanRepoOptionsFromCommand(cmd *cobra.Command, args []string) (vulnScanRepoOptions, error) {
	scanOpts, err := scanOptionsFromCommand(cmd, args)
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	if limit <= 0 {
		return vulnScanRepoOptions{}, commandExitError{message: "--limit must be greater than 0", code: 2}
	}
	if limit > 200 {
		return vulnScanRepoOptions{}, commandExitError{message: "--limit must be 200 or lower", code: 2}
	}
	impactStatus, err := cmd.Flags().GetString("impact-status")
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	repoID, err := cmd.Flags().GetString("repo-id")
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	broad, err := cmd.Flags().GetBool("broad")
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	exportFormat, err := cmd.Flags().GetString("export")
	if err != nil {
		return vulnScanRepoOptions{}, err
	}
	exportFormat = strings.ToLower(strings.TrimSpace(exportFormat))
	switch exportFormat {
	case "", vulnscan.ExportFormatSARIF, vulnscan.ExportFormatVEX:
	default:
		return vulnScanRepoOptions{}, commandExitError{message: fmt.Sprintf("unsupported --export %q: expected sarif or vex", exportFormat), code: 2}
	}
	if exportFormat != "" && scanOpts.JSON {
		return vulnScanRepoOptions{}, commandExitError{message: "--json cannot be combined with --export; use one output contract", code: 2}
	}
	return vulnScanRepoOptions{
		Scan:         scanOpts,
		Limit:        limit,
		ImpactStatus: strings.TrimSpace(impactStatus),
		RepoID:       strings.TrimSpace(repoID),
		Broad:        broad,
		ExportFormat: exportFormat,
	}, nil
}

// resolveVulnScanRepoID returns the canonical repository ID the scan reports
// against. An explicit --repo-id wins outright; otherwise the scanned root
// path is resolved as a repository selector, so `eshu vuln-scan repo .` names
// the repository the operator is standing in.
func resolveVulnScanRepoID(client *APIClient, opts vulnScanRepoOptions) (string, error) {
	if opts.RepoID != "" {
		return opts.RepoID, nil
	}
	repositoryID, err := reposelector.Resolve(client, opts.Scan.Target.Root)
	if err != nil {
		return "", fmt.Errorf("resolve scanned repository: %w", err)
	}
	return repositoryID, nil
}

func finishVulnScanRepo(
	cmd *cobra.Command,
	opts vulnScanRepoOptions,
	result vulnscan.Result,
	truth map[string]any,
	err error,
) error {
	if truth == nil {
		truth = scan.Truth("stale", "partial", opts.Scan.Profile, scan.CurrentGraphBackend())
	}
	report := vulnscan.BuildReport(result, vulnscan.Now())
	result.Report = &report
	if opts.ExportFormat == vulnscan.ExportFormatSARIF {
		if err != nil && !isVulnScanScannerExit(err) {
			return err
		}
		if writeErr := vulnscan.WriteSARIF(cmd.OutOrStdout(), result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	if opts.ExportFormat == vulnscan.ExportFormatVEX {
		if err != nil && !isVulnScanScannerExit(err) {
			return err
		}
		if writeErr := vulnscan.WriteVEX(cmd.OutOrStdout(), result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	envelope := vulnScanRepoEnvelope{
		Data:  result,
		Truth: truth,
	}
	if err != nil && !isVulnScanFindingsExit(err) {
		envelope.Error = &vulnscan.RepoError{Message: err.Error()}
	}
	if opts.Scan.JSON {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if isVulnScanScannerExit(err) {
			if renderErr := vulnscan.RenderSummary(cmd.OutOrStdout(), result); renderErr != nil {
				return renderErr
			}
		}
		return err
	}
	return vulnscan.RenderSummary(cmd.OutOrStdout(), result)
}

func finishVulnScanRepoAfterCleanup(
	cmd *cobra.Command,
	opts vulnScanRepoOptions,
	result vulnscan.Result,
	truth map[string]any,
	err error,
	cleanup func() error,
) error {
	if cleanup != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			warning := fmt.Sprintf("local runtime cleanup failed: %v", cleanupErr)
			result.Warnings = append(result.Warnings, warning)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
		}
	}
	return finishVulnScanRepo(cmd, opts, result, truth, err)
}

// isVulnScanFindingsExit reports whether err is the findings-present exit
// (code 3), which is a successful scan that found something rather than a
// failure, so the JSON envelope carries no error member.
func isVulnScanFindingsExit(err error) bool {
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 3
}

// isVulnScanScannerExit reports whether err is one of the scanner exits the
// report is still written for: findings present, evidence incomplete, or
// unsupported target evidence. Any other error skips the report, because the
// run never produced one.
func isVulnScanScannerExit(err error) bool {
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	switch exitErr.ExitCode() {
	case 3, 4, 5:
		return true
	default:
		return false
	}
}

func vulnScanHasConfiguredServiceURL(cmd *cobra.Command) bool {
	serviceURL, _ := cmd.Flags().GetString("service-url")
	if strings.TrimSpace(serviceURL) != "" {
		return true
	}
	profile, _ := cmd.Flags().GetString("profile")
	if strings.TrimSpace(cliconfig.ResolveValue("ESHU_SERVICE_URL", profile)) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("ESHU_SERVICE_URL")) != ""
}

// vulnScanPrepareLocalRuntime is the seam the command-level tests replace to
// drive the repo subcommand without starting a real local service.
var vulnScanPrepareLocalRuntime = vulnscan.PrepareLocalRuntime
