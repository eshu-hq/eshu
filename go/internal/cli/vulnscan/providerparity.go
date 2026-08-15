// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparity"
	"github.com/eshu-hq/eshu/go/internal/vulnerabilityparityproof"
)

// ParityOptions is the selector set for `eshu vuln-scan provider-parity`,
// holding flag values after the command wrapper has read and validated them.
// ProviderToken is the resolved secret rather than the variable name: reading
// the process environment is the wrapper's job, so nothing here touches os.
type ParityOptions struct {
	// Provider names the alert source to fetch when ProviderAlertsFile is
	// empty. Dashes and case are normalized before it is matched.
	Provider string
	// ProviderAlertsFile is an operator-local alert summary. When set it wins
	// over Provider, and no provider API is called.
	ProviderAlertsFile string
	// ProviderAPIURL is the provider API base URL.
	ProviderAPIURL string
	// ProviderToken is the already-resolved provider API token.
	ProviderToken string
	// SupportedEcosystems are the ecosystems Eshu should classify as
	// supported, lowercased and blank-stripped.
	SupportedEcosystems []string
	// Limit caps the Eshu findings read per repository.
	Limit int
}

// ParitySource picks where provider alerts come from: an operator-local
// summary file when one is named, otherwise the provider's API. Only GitHub
// Dependabot is wired today, and a missing token fails rather than silently
// producing an empty alert set that would read as perfect parity.
func ParitySource(opts ParityOptions) (vulnerabilityparityproof.ProviderAlertSource, error) {
	if opts.ProviderAlertsFile != "" {
		//nolint:wrapcheck // the loader's own message names the file and what was wrong with it; a prefix here would displace it
		return vulnerabilityparityproof.LoadProviderAlertSummaries(opts.ProviderAlertsFile)
	}
	switch NormalizeProviderName(opts.Provider) {
	case "github_dependabot":
		if strings.TrimSpace(opts.ProviderToken) == "" {
			return nil, fmt.Errorf("provider token environment variable is not set")
		}
		return vulnerabilityparityproof.GitHubDependabotSource{
			BaseURL: opts.ProviderAPIURL,
			Token:   opts.ProviderToken,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

// NormalizeProviderName folds a provider name to the lowercase underscore form
// the source switch matches, so `github-dependabot` and `GitHub_Dependabot`
// both resolve.
func NormalizeProviderName(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
}

// CleanStringSlice lowercases and trims each value and drops the blanks. The
// wrapper uses it on the repeatable --supported-ecosystem flag.
func CleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// ParityData renders the aggregate report as the wire map the command writes,
// stamped with the command name and where each side's evidence came from.
// Only aggregates cross this boundary: per-repository provider alert detail
// stays inside the proof package so a parity run cannot leak a private
// repository's advisory list into a shareable artifact.
func ParityData(
	report vulnerabilityparityproof.AggregateReport,
	opts ParityOptions,
) map[string]any {
	data := vulnerabilityparityproof.AggregateWire(report)
	data["command"] = "vuln-scan provider-parity"
	data["evidence"] = map[string]any{
		"provider_alert_source": ParityEvidenceSource(opts),
		"eshu_findings_source":  "api",
		"findings_endpoint":     ImpactFindingsEndpoint,
	}
	return data
}

// ParityEvidenceSource names where the provider alerts came from, so a reader
// can tell an API-backed run from one replayed off a local summary.
func ParityEvidenceSource(opts ParityOptions) string {
	if opts.ProviderAlertsFile != "" {
		return "operator_local_summary"
	}
	return NormalizeProviderName(opts.Provider) + "_api"
}

// RenderParitySummary writes the one-line human summary for a parity run.
func RenderParitySummary(w io.Writer, data map[string]any) error {
	return writef(
		w,
		"Provider parity: repositories=%v provider_alerts=%v eshu_findings=%v mismatches=%d\n",
		data["repositories_checked"],
		data["provider_alert_count"],
		data["eshu_finding_count"],
		parityMismatchCount(data["mismatch_classes"]),
	)
}

// parityMismatchCount sums the per-class mismatch counts. A value of another
// type counts as zero, which is safe because ParityData always produces the
// typed slice and the summary line is not the run's verdict.
func parityMismatchCount(raw any) int {
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

// EshuSource reads Eshu's own findings for a parity comparison through the
// impact-findings API.
type EshuSource struct {
	// Client is the transport. A nil client fails the read rather than
	// reporting zero findings, which would read as Eshu missing everything.
	Client EnvelopeFetcher
}

// ListEshuFindings implements the proof package's Eshu side. Its error
// messages deliberately carry no repository id, URL, or server text: this
// output is meant to be shareable, and a failure message is the easiest place
// for a private repository name to escape.
func (s EshuSource) ListEshuFindings(
	_ context.Context,
	repo vulnerabilityparityproof.RepositoryTarget,
	limit int,
) (vulnerabilityparityproof.EshuFindingPage, error) {
	if s.Client == nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("missing API client")
	}
	query := url.Values{}
	query.Set("repository_id", repo.EshuRepositoryID)
	query.Set("limit", fmt.Sprintf("%d", limit))
	var envelope ImpactFindingsEnvelope
	path := ImpactFindingsEndpoint + "?" + query.Encode()
	if err := s.Client.GetEnvelope(path, &envelope); err != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("fetch Eshu findings")
	}
	if envelope.Error != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, fmt.Errorf("eshu findings response returned an error")
	}
	findings, err := MapEshuFindings(envelope.Data.Findings)
	if err != nil {
		return vulnerabilityparityproof.EshuFindingPage{}, err
	}
	return vulnerabilityparityproof.EshuFindingPage{
		Findings:       findings,
		Evidence:       ParityEvidenceFromReadiness(envelope.Data.Readiness),
		Truncated:      envelope.Data.Truncated,
		ReadinessState: ReadinessState(envelope.Data.Readiness, len(findings)),
		FreshnessState: parityFreshnessState(envelope.Data.Readiness, envelope.Truth),
	}, nil
}

// MapEshuFindings converts raw finding rows into the parity package's finding
// shape. A row missing advisory or package identity fails the whole read: a
// finding that cannot be matched would otherwise silently become a provider
// mismatch, which is the exact answer a parity run is supposed to establish.
func MapEshuFindings(rows []map[string]any) ([]vulnerabilityparity.EshuFinding, error) {
	out := make([]vulnerabilityparity.EshuFinding, 0, len(rows))
	for idx, row := range rows {
		finding := vulnerabilityparity.EshuFinding{
			AdvisoryID:      firstNonEmpty(rowString(row, "advisory_id"), rowString(row, "cve_id")),
			CVEID:           rowString(row, "cve_id"),
			Ecosystem:       rowString(row, "ecosystem"),
			PackageName:     rowString(row, "package_name"),
			PackageID:       rowString(row, "package_id"),
			ObservedVersion: rowString(row, "observed_version"),
			FixedVersion:    rowString(row, "fixed_version"),
			Status:          parityStatusFromFinding(row),
		}
		if finding.AdvisoryID == "" || finding.PackageID == "" {
			return nil, fmt.Errorf("eshu finding row %d is missing required parity identity", idx+1)
		}
		out = append(out, finding)
	}
	return out, nil
}

// parityStatusFromFinding maps a finding onto the parity status. A suppressed
// finding counts as dismissed regardless of impact status, matching how
// providers report an alert an operator has closed.
func parityStatusFromFinding(row map[string]any) vulnerabilityparity.FindingStatus {
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

// suppressionDismissesFinding lists the suppression states that count as an
// operator having closed the finding.
func suppressionDismissesFinding(state string) bool {
	switch state {
	case "accepted_risk", "false_positive", "ignored", "not_affected", "provider_dismissed", "scope_mismatch":
		return true
	default:
		return false
	}
}

// ParityEvidenceFromReadiness reads which evidence families the readiness
// envelope proves are present and fresh, then clears any the envelope
// explicitly reports as missing. The two passes are not redundant: a family
// can appear in evidence_sources with facts while the server still reports the
// evidence incomplete, and the missing list wins.
func ParityEvidenceFromReadiness(readiness map[string]any) vulnerabilityparity.EvidenceCoverage {
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

// parityFreshnessState prefers the readiness envelope's freshness and falls
// back to the response truth block.
func parityFreshnessState(readiness map[string]any, truth map[string]any) string {
	if freshness := rowString(readiness, "freshness"); freshness != "" {
		return freshness
	}
	if freshness, ok := truth["freshness"].(map[string]any); ok {
		return rowString(freshness, "state")
	}
	return ""
}

// readinessEvidenceFamilies lists the families that both carry facts and are
// fresh. A family with facts but stale evidence does not count as coverage,
// which is what keeps a parity run from reporting Eshu as complete off a stale
// cache.
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

// readinessEvidenceSourceFresh treats an absent freshness value as fresh,
// matching older servers that did not report one, and anything other than
// `fresh` as not fresh.
func readinessEvidenceSourceFresh(row map[string]any) bool {
	switch strings.ToLower(rowString(row, "freshness")) {
	case "", "fresh":
		return true
	default:
		return false
	}
}

// readinessMissingEvidence lists the envelope's missing-evidence reasons.
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

// suppressionState reads the finding's suppression state, or "" when the
// finding carries no suppression block.
func suppressionState(row map[string]any) string {
	suppression, _ := row["suppression"].(map[string]any)
	return rowString(suppression, "state")
}

// factCount reads a readiness row's fact_count.
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

// rowString reads a trimmed string out of a readiness or finding row.
func rowString(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return strings.TrimSpace(value)
}
