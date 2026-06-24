// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparity"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
)

type vulnScanProviderParityOptions struct {
	AllowlistFile       string
	Provider            string
	ProviderAlertsFile  string
	ProviderAPIURL      string
	ProviderTokenEnv    string
	SupportedEcosystems []string
	Limit               int
	JSON                bool
}

type vulnScanProviderParityEnvelope struct {
	Data  map[string]any     `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *vulnScanRepoError `json:"error"`
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
	providerSource, err := providerParitySource(opts)
	if err != nil {
		return finishVulnScanProviderParity(cmd, opts, vulnerabilityparityproof.AggregateReport{}, err)
	}
	report, err := vulnerabilityparityproof.CompareProviderParity(cmd.Context(), vulnerabilityparityproof.CompareRequest{
		Repositories:        repositories,
		Provider:            providerSource,
		Eshu:                providerParityEshuSource{client: apiClientFromCmd(cmd)},
		Limit:               opts.Limit,
		SupportedEcosystems: opts.SupportedEcosystems,
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
		AllowlistFile:       strings.TrimSpace(allowlistFile),
		Provider:            strings.TrimSpace(provider),
		ProviderAlertsFile:  strings.TrimSpace(providerAlertsFile),
		ProviderAPIURL:      strings.TrimSpace(providerAPIURL),
		ProviderTokenEnv:    strings.TrimSpace(providerTokenEnv),
		SupportedEcosystems: cleanStringSlice(supportedEcosystems),
		Limit:               limit,
		JSON:                jsonOutput,
	}, nil
}

func providerParitySource(
	opts vulnScanProviderParityOptions,
) (vulnerabilityparityproof.ProviderAlertSource, error) {
	if opts.ProviderAlertsFile != "" {
		return vulnerabilityparityproof.LoadProviderAlertSummaries(opts.ProviderAlertsFile)
	}
	switch normalizeProviderName(opts.Provider) {
	case "github_dependabot":
		token := providerTokenFromEnv(opts.ProviderTokenEnv)
		if token == "" {
			return nil, fmt.Errorf("provider token environment variable is not set")
		}
		return vulnerabilityparityproof.GitHubDependabotSource{
			BaseURL: opts.ProviderAPIURL,
			Token:   token,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

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

func normalizeProviderName(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
}

func finishVulnScanProviderParity(
	cmd *cobra.Command,
	opts vulnScanProviderParityOptions,
	report vulnerabilityparityproof.AggregateReport,
	err error,
) error {
	data := providerParityData(report, opts)
	envelope := vulnScanProviderParityEnvelope{
		Data:  data,
		Truth: scanTruth("exact", "fresh", "operator_provider_parity", currentGraphBackend()),
	}
	if err != nil {
		envelope.Error = &vulnScanRepoError{Message: err.Error()}
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
	return renderProviderParitySummary(cmd.OutOrStdout(), data)
}

func providerParityData(
	report vulnerabilityparityproof.AggregateReport,
	opts vulnScanProviderParityOptions,
) map[string]any {
	data := vulnerabilityparityproof.AggregateWire(report)
	data["command"] = "vuln-scan provider-parity"
	data["evidence"] = map[string]any{
		"provider_alert_source": providerParityEvidenceSource(opts),
		"eshu_findings_source":  "api",
		"findings_endpoint":     vulnScanImpactFindingsEndpoint,
	}
	return data
}

func providerParityEvidenceSource(opts vulnScanProviderParityOptions) string {
	if opts.ProviderAlertsFile != "" {
		return "operator_local_summary"
	}
	return normalizeProviderName(opts.Provider) + "_api"
}

func renderProviderParitySummary(w io.Writer, data map[string]any) error {
	_, err := fmt.Fprintf(
		w,
		"Provider parity: repositories=%v provider_alerts=%v eshu_findings=%v mismatches=%d\n",
		data["repositories_checked"],
		data["provider_alert_count"],
		data["eshu_finding_count"],
		providerParityMismatchCount(data["mismatch_classes"]),
	)
	return err
}

func providerParityMismatchCount(raw any) int {
	classes, ok := raw.([]vulnerabilityparityproof.ClassCount)
	if !ok {
		return 0
	}
	total := 0
	for _, class := range classes {
		total += class.Count
	}
	return total
}

type providerParityEshuSource struct {
	client *APIClient
}

func (s providerParityEshuSource) ListEshuFindings(
	_ context.Context,
	repo vulnerabilityparityproof.RepositoryTarget,
	limit int,
) (vulnerabilityparityproof.EshuFindingPage, error) {
	if s.client == nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("missing API client")
	}
	query := url.Values{}
	query.Set("repository_id", repo.EshuRepositoryID)
	query.Set("limit", fmt.Sprintf("%d", limit))
	var envelope vulnScanImpactFindingsEnvelope
	path := vulnScanImpactFindingsEndpoint + "?" + query.Encode()
	if err := s.client.GetEnvelope(path, &envelope); err != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("fetch Eshu findings")
	}
	if envelope.Error != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("eshu findings response returned an error")
	}
	findings, err := providerParityMapEshuFindings(envelope.Data.Findings)
	if err != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, err
	}
	return vulnerabilityparityproof.EshuFindingPage{
		Findings:       findings,
		Evidence:       providerParityEvidenceFromReadiness(envelope.Data.Readiness),
		Truncated:      envelope.Data.Truncated,
		ReadinessState: vulnScanReadinessState(envelope.Data.Readiness, len(findings)),
		FreshnessState: providerParityFreshnessState(envelope.Data.Readiness, envelope.Truth),
	}, nil
}

func providerParityMapEshuFindings(rows []map[string]any) ([]vulnerabilityparity.EshuFinding, error) {
	out := make([]vulnerabilityparity.EshuFinding, 0, len(rows))
	for idx, row := range rows {
		finding := vulnerabilityparity.EshuFinding{
			AdvisoryID:      firstNonBlankString(rowString(row, "advisory_id"), rowString(row, "cve_id")),
			CVEID:           rowString(row, "cve_id"),
			Ecosystem:       rowString(row, "ecosystem"),
			PackageName:     rowString(row, "package_name"),
			PackageID:       rowString(row, "package_id"),
			ObservedVersion: rowString(row, "observed_version"),
			FixedVersion:    rowString(row, "fixed_version"),
			Status:          providerParityStatusFromFinding(row),
		}
		if finding.AdvisoryID == "" || finding.PackageID == "" {
			return nil, fmt.Errorf("eshu finding row %d is missing required parity identity", idx+1)
		}
		out = append(out, finding)
	}
	return out, nil
}

func providerParityStatusFromFinding(row map[string]any) vulnerabilityparity.FindingStatus {
	if suppressionDismissesFinding(suppressionState(row)) {
		return vulnerabilityparity.StatusDismissed
	}
	switch rowString(row, "impact_status") {
	case "not_affected_known_fixed":
		return vulnerabilityparity.StatusFixed
	default:
		return vulnerabilityparity.StatusOpen
	}
}

func suppressionDismissesFinding(state string) bool {
	switch state {
	case "accepted_risk", "false_positive", "ignored", "not_affected", "provider_dismissed", "scope_mismatch":
		return true
	default:
		return false
	}
}

func providerParityEvidenceFromReadiness(readiness map[string]any) vulnerabilityparity.EvidenceCoverage {
	evidence := vulnerabilityparity.EvidenceCoverage{}
	for _, family := range readinessEvidenceFamilies(readiness) {
		switch family {
		case "package.consumption":
			evidence.HasDependency = true
		case "vulnerability.advisory":
			evidence.HasAdvisory = true
		case "sbom.component", "sbom.attestation":
			evidence.HasSBOM = true
		case "container_image.identity":
			evidence.HasImage = true
		}
	}
	for _, missing := range readinessMissingEvidence(readiness) {
		switch missing {
		case "owned_packages", "target_collection_incomplete":
			evidence.HasDependency = false
		case "advisory_sources":
			evidence.HasAdvisory = false
		case "sbom_or_image_evidence":
			evidence.HasSBOM = false
			evidence.HasImage = false
		}
	}
	return evidence
}

func providerParityFreshnessState(readiness map[string]any, truth map[string]any) string {
	if freshness := rowString(readiness, "freshness"); freshness != "" {
		return freshness
	}
	if freshness, ok := truth["freshness"].(map[string]any); ok {
		return rowString(freshness, "state")
	}
	return ""
}

func readinessEvidenceFamilies(readiness map[string]any) []string {
	var out []string
	items, _ := readiness["evidence_sources"].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		if factCount(row) <= 0 || !readinessEvidenceSourceFresh(row) {
			continue
		}
		if family := rowString(row, "family"); family != "" {
			out = append(out, family)
		}
	}
	return out
}

func readinessEvidenceSourceFresh(row map[string]any) bool {
	switch strings.ToLower(rowString(row, "freshness")) {
	case "", "fresh":
		return true
	default:
		return false
	}
}

func readinessMissingEvidence(readiness map[string]any) []string {
	var out []string
	items, _ := readiness["missing_evidence"].([]any)
	for _, item := range items {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func suppressionState(row map[string]any) string {
	suppression, _ := row["suppression"].(map[string]any)
	return rowString(suppression, "state")
}

func factCount(row map[string]any) int {
	switch value := row["fact_count"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func rowString(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return strings.TrimSpace(value)
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
