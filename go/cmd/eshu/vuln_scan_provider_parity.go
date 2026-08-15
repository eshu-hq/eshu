// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/vulnscan"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
)

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
		Truth: scanTruth("exact", "fresh", "operator_provider_parity", currentGraphBackend()),
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
