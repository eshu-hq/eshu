// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	cliconfig "github.com/eshu-hq/eshu/go/internal/cli/config"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/cli/vulnscan"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
	"github.com/spf13/cobra"
)

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
	// Guard the CONCRETE pointer, not RepoDeps.Client. A nil *APIClient boxed
	// into the RepoClient interface is a non-nil interface value, so vulnscan's
	// own nil check cannot see it and reposelector.Resolve would panic inside
	// APIClient.do -- the failure missingAPIClientError exists to prevent.
	// Neither constructor above can return nil today, which is exactly why this
	// belongs here: it keeps the invariant enforced by code rather than by the
	// current behaviour of apiClientFromCmd and NewAPIClient. The analyze family
	// keeps the same guard in repository_selector.go.
	if client == nil {
		return fmt.Errorf("resolve scanned repository: %w",
			missingAPIClientError(opts.Scan.Target.Root))
	}
	deps := vulnscan.RepoDeps{
		Client:            client,
		ServiceURL:        client.BaseURL,
		ScanRuntime:       scanRuntimeFor(client),
		Stdout:            cmd.OutOrStdout(),
		Stderr:            cmd.ErrOrStderr(),
		StartedAt:         startedAt,
		CloseLocalRuntime: closeLocalRuntime,
	}
	return vulnScanRunError(vulnscan.RunRepo(cmd.Context(), deps, opts))
}

// vulnScanRunError maps the outcome of vulnscan.RunRepo onto the CLI's
// exit-code contract. internal/cli/vulnscan classifies the run and chooses the
// number, carried as a *vulnscan.Failure; commandExitError is defined here, in
// package main, so the conversion has to happen here too. Any other error --
// a scan or transport failure, an unresolved selector, a write failure -- is
// returned unchanged, which is exit code 1 with the error's own text.
func vulnScanRunError(err error) error {
	var failure *vulnscan.Failure
	if errors.As(err, &failure) {
		return commandExitError{message: failure.Message, code: failure.Code}
	}
	return err
}

func vulnScanRepoOptionsFromCommand(cmd *cobra.Command, args []string) (vulnscan.RepoOptions, error) {
	scanOpts, err := scanOptionsFromCommand(cmd, args)
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	if limit <= 0 {
		return vulnscan.RepoOptions{}, commandExitError{message: "--limit must be greater than 0", code: 2}
	}
	if limit > 200 {
		return vulnscan.RepoOptions{}, commandExitError{message: "--limit must be 200 or lower", code: 2}
	}
	impactStatus, err := cmd.Flags().GetString("impact-status")
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	repoID, err := cmd.Flags().GetString("repo-id")
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	broad, err := cmd.Flags().GetBool("broad")
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	exportFormat, err := cmd.Flags().GetString("export")
	if err != nil {
		return vulnscan.RepoOptions{}, err
	}
	exportFormat = strings.ToLower(strings.TrimSpace(exportFormat))
	switch exportFormat {
	case "", vulnscan.ExportFormatSARIF, vulnscan.ExportFormatVEX:
	default:
		return vulnscan.RepoOptions{}, commandExitError{message: fmt.Sprintf("unsupported --export %q: expected sarif or vex", exportFormat), code: 2}
	}
	if exportFormat != "" && scanOpts.JSON {
		return vulnscan.RepoOptions{}, commandExitError{message: "--json cannot be combined with --export; use one output contract", code: 2}
	}
	return vulnscan.RepoOptions{
		Scan:         scanOpts,
		Limit:        limit,
		ImpactStatus: strings.TrimSpace(impactStatus),
		RepoID:       strings.TrimSpace(repoID),
		Broad:        broad,
		ExportFormat: exportFormat,
	}, nil
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

type vulnScanProviderParityOptions struct {
	AllowlistFile string
	JSON          bool
	Parity        vulnscan.ParityOptions
}

type vulnScanProviderParityEnvelope struct {
	Data  map[string]any      `json:"data"`
	Truth map[string]any      `json:"truth"`
	Error *vulnscan.RepoError `json:"error"`
}

func addVulnScanProviderParityFlags(cmd *cobra.Command) {
	cmd.Flags().String("allowlist-file", "", "Path to operator-local provider/Eshu repository allowlist JSON")
	cmd.Flags().String("provider", "github-dependabot", "Provider alert source to fetch when --provider-alerts-file is not set")
	cmd.Flags().String("provider-alerts-file", "", "Path to operator-local generic provider alert summary JSON")
	cmd.Flags().String("provider-api-url", "https://api.github.com", "Provider API base URL")
	cmd.Flags().String("provider-token-env", "GITHUB_TOKEN", "Environment variable that holds the provider API token")
	cmd.Flags().StringSlice("supported-ecosystem", nil, "Ecosystem Eshu should classify as supported; repeat or comma-separate")
	cmd.Flags().Int("limit", 200, "Maximum Eshu vulnerability impact findings to read per repository")
	cmd.Flags().Bool("json", false, "Write aggregate provider parity proof as JSON")
	_ = cmd.Flags().MarkHidden("provider-api-url")
}

func runVulnScanProviderParity(cmd *cobra.Command, _ []string) error {
	opts, err := vulnScanProviderParityOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	repositories, err := vulnerabilityparityproof.LoadRepositoryAllowlist(opts.AllowlistFile)
	if err != nil {
		return finishVulnScanProviderParity(cmd, opts, vulnerabilityparityproof.AggregateReport{}, err)
	}
	providerSource, err := vulnscan.ParitySource(opts.Parity)
	if err != nil {
		return finishVulnScanProviderParity(cmd, opts, vulnerabilityparityproof.AggregateReport{}, err)
	}
	report, err := vulnerabilityparityproof.CompareProviderParity(cmd.Context(), vulnerabilityparityproof.CompareRequest{
		Repositories:        repositories,
		Provider:            providerSource,
		Eshu:                vulnscan.EshuSource{Client: apiClientFromCmd(cmd)},
		Limit:               opts.Parity.Limit,
		SupportedEcosystems: opts.Parity.SupportedEcosystems,
	})
	return finishVulnScanProviderParity(cmd, opts, report, err)
}

func vulnScanProviderParityOptionsFromCommand(cmd *cobra.Command) (vulnScanProviderParityOptions, error) {
	allowlistFile, err := cmd.Flags().GetString("allowlist-file")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	provider, err := cmd.Flags().GetString("provider")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	providerAlertsFile, err := cmd.Flags().GetString("provider-alerts-file")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	providerAPIURL, err := cmd.Flags().GetString("provider-api-url")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	providerTokenEnv, err := cmd.Flags().GetString("provider-token-env")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	supportedEcosystems, err := cmd.Flags().GetStringSlice("supported-ecosystem")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return vulnScanProviderParityOptions{}, err
	}
	if strings.TrimSpace(allowlistFile) == "" {
		return vulnScanProviderParityOptions{}, commandExitError{message: "--allowlist-file is required", code: 2}
	}
	if limit <= 0 || limit > 200 {
		return vulnScanProviderParityOptions{}, commandExitError{message: "--limit must be between 1 and 200", code: 2}
	}
	return vulnScanProviderParityOptions{
		AllowlistFile: strings.TrimSpace(allowlistFile),
		JSON:          jsonOutput,
		Parity: vulnscan.ParityOptions{
			Provider:            strings.TrimSpace(provider),
			ProviderAlertsFile:  strings.TrimSpace(providerAlertsFile),
			ProviderAPIURL:      strings.TrimSpace(providerAPIURL),
			ProviderToken:       providerTokenFromEnv(providerTokenEnv),
			SupportedEcosystems: vulnscan.CleanStringSlice(supportedEcosystems),
			Limit:               limit,
		},
	}, nil
}

// providerTokenFromEnv resolves the provider API token from the environment
// variable the operator named. GH_TOKEN is accepted as a fallback for the
// GITHUB_TOKEN default only, matching the gh CLI; a variable the operator
// named explicitly is used as given, with no second guess.
func providerTokenFromEnv(envName string) string {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		envName = "GITHUB_TOKEN"
	}
	if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
		return token
	}
	if envName == "GITHUB_TOKEN" {
		return strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}
	return ""
}

func finishVulnScanProviderParity(
	cmd *cobra.Command,
	opts vulnScanProviderParityOptions,
	report vulnerabilityparityproof.AggregateReport,
	err error,
) error {
	data := vulnscan.ParityData(report, opts.Parity)
	envelope := vulnScanProviderParityEnvelope{
		Data:  data,
		Truth: scan.Truth("exact", "fresh", "operator_provider_parity", scan.CurrentGraphBackend()),
	}
	if err != nil {
		envelope.Error = &vulnscan.RepoError{Message: err.Error()}
	}
	if opts.JSON {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		return err
	}
	return vulnscan.RenderParitySummary(cmd.OutOrStdout(), data)
}
