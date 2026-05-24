package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const vulnScanImpactFindingsEndpoint = "/api/v0/supply-chain/impact/findings"

type vulnScanRepoOptions struct {
	Scan         scanOptions
	Limit        int
	ImpactStatus string
	RepoID       string
}

type vulnScanRepoResult struct {
	Command        string               `json:"command"`
	Status         string               `json:"status"`
	ReadinessState string               `json:"readiness_state"`
	Target         scanTarget           `json:"target"`
	RepositoryID   string               `json:"repository_id,omitempty"`
	Scan           scanResult           `json:"scan"`
	Findings       []map[string]any     `json:"findings"`
	Count          int                  `json:"count"`
	Limit          int                  `json:"limit"`
	Truncated      bool                 `json:"truncated"`
	NextCursor     map[string]any       `json:"next_cursor,omitempty"`
	Readiness      map[string]any       `json:"readiness,omitempty"`
	Warnings       []string             `json:"warnings,omitempty"`
	Evidence       vulnScanRepoEvidence `json:"evidence"`
}

type vulnScanRepoEvidence struct {
	ServiceURL       string `json:"service_url"`
	FindingsEndpoint string `json:"findings_endpoint"`
}

type vulnScanRepoEnvelope struct {
	Data  vulnScanRepoResult `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *vulnScanRepoError `json:"error"`
}

type vulnScanRepoError struct {
	Message string `json:"message"`
}

type vulnScanImpactFindingsEnvelope struct {
	Data  vulnScanImpactFindingsData `json:"data"`
	Truth map[string]any             `json:"truth"`
	Error *vulnScanRepoError         `json:"error"`
}

type vulnScanImpactFindingsData struct {
	Findings   []map[string]any `json:"findings"`
	Count      int              `json:"count"`
	Limit      int              `json:"limit"`
	Truncated  bool             `json:"truncated"`
	NextCursor map[string]any   `json:"next_cursor,omitempty"`
	Readiness  map[string]any   `json:"readiness,omitempty"`
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
	vulnScanCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(vulnScanCmd)
}

func addVulnScanRepoFlags(cmd *cobra.Command) {
	addScanFlags(cmd)
	cmd.Flags().Int("limit", 50, "Maximum vulnerability impact findings to return")
	cmd.Flags().String("impact-status", "", "Filter impact findings by reducer-owned impact status")
	cmd.Flags().String("repo-id", "", "Exact repository id to query after local scan readiness")
}

func runVulnScanRepo(cmd *cobra.Command, args []string) error {
	opts, err := vulnScanRepoOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	client := apiClientFromCmd(cmd)
	result := newVulnScanRepoResult(opts, client.BaseURL)
	scanStdout := cmd.OutOrStdout()
	if opts.Scan.JSON {
		scanStdout = cmd.ErrOrStderr()
	}
	scanResult, err := executeScan(cmd.Context(), scanStdout, cmd.ErrOrStderr(), client, opts.Scan, !opts.Scan.JSON)
	result.Scan = scanResult
	result.Status = scanResult.Status
	result.Warnings = append(result.Warnings, scanResult.Warnings...)
	if err != nil {
		result.ReadinessState = "target_incomplete"
		return finishVulnScanRepo(cmd, opts, result, scanResult.Truth, err)
	}
	if scanResult.Status != "ready" {
		result.ReadinessState = "target_incomplete"
		err := commandExitError{message: "vulnerability scan target is not ready; rerun with --wait=true before reading findings", code: 4}
		return finishVulnScanRepo(cmd, opts, result, scanResult.Truth, err)
	}

	repositoryID, err := resolveVulnScanRepoID(cmd, client, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		return finishVulnScanRepo(cmd, opts, result, scanResult.Truth, err)
	}
	result.RepositoryID = repositoryID

	findings, err := fetchVulnScanRepoImpactFindings(client, repositoryID, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		return finishVulnScanRepo(cmd, opts, result, scanResult.Truth, err)
	}
	if findings.Error != nil {
		result.ReadinessState = "evidence_incomplete"
		err := commandExitError{message: findings.Error.Message, code: 4}
		return finishVulnScanRepo(cmd, opts, result, findings.Truth, err)
	}
	result.Findings = findings.Data.Findings
	result.Count = findings.Data.Count
	result.Limit = findings.Data.Limit
	result.Truncated = findings.Data.Truncated
	result.NextCursor = findings.Data.NextCursor
	result.Readiness = findings.Data.Readiness
	result.ReadinessState = vulnScanReadinessState(findings.Data.Readiness, result.Count)
	return finishVulnScanRepo(cmd, opts, result, findings.Truth, nil)
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
	return vulnScanRepoOptions{
		Scan:         scanOpts,
		Limit:        limit,
		ImpactStatus: strings.TrimSpace(impactStatus),
		RepoID:       strings.TrimSpace(repoID),
	}, nil
}

func newVulnScanRepoResult(opts vulnScanRepoOptions, serviceURL string) vulnScanRepoResult {
	return vulnScanRepoResult{
		Command:        "vuln-scan repo",
		Status:         "failed",
		ReadinessState: "target_incomplete",
		Target:         opts.Scan.Target,
		Findings:       []map[string]any{},
		Limit:          opts.Limit,
		Evidence: vulnScanRepoEvidence{
			ServiceURL:       serviceURL,
			FindingsEndpoint: vulnScanImpactFindingsEndpoint,
		},
	}
}

func resolveVulnScanRepoID(cmd *cobra.Command, client *APIClient, opts vulnScanRepoOptions) (string, error) {
	if opts.RepoID != "" {
		return opts.RepoID, nil
	}
	repositoryID, err := resolveRepositorySelector(cmd, client, opts.Scan.Target.Root)
	if err != nil {
		return "", fmt.Errorf("resolve scanned repository: %w", err)
	}
	return repositoryID, nil
}

func fetchVulnScanRepoImpactFindings(
	client *APIClient,
	repositoryID string,
	opts vulnScanRepoOptions,
) (vulnScanImpactFindingsEnvelope, error) {
	if client == nil {
		return vulnScanImpactFindingsEnvelope{}, fmt.Errorf("missing API client")
	}
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return vulnScanImpactFindingsEnvelope{}, fmt.Errorf("repository id is required")
	}
	query := url.Values{}
	query.Set("repository_id", repositoryID)
	query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	if opts.ImpactStatus != "" {
		query.Set("impact_status", opts.ImpactStatus)
	}
	var envelope vulnScanImpactFindingsEnvelope
	path := vulnScanImpactFindingsEndpoint + "?" + query.Encode()
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return vulnScanImpactFindingsEnvelope{}, fmt.Errorf("fetch vulnerability impact findings: %w", err)
	}
	return envelope, nil
}

// vulnScanReadinessState prefers the server-side readiness verdict so the CLI
// surfaces not_configured, target_incomplete, evidence_incomplete, unsupported,
// or readiness_unavailable when the server reports them. It only falls back to
// the count-based heuristic when the server response does not carry a
// readiness envelope (older API versions).
func vulnScanReadinessState(readiness map[string]any, count int) string {
	if state, ok := readiness["readiness_state"].(string); ok && strings.TrimSpace(state) != "" {
		return strings.TrimSpace(state)
	}
	if count > 0 {
		return "ready_with_findings"
	}
	return "ready_zero_findings"
}

func finishVulnScanRepo(
	cmd *cobra.Command,
	opts vulnScanRepoOptions,
	result vulnScanRepoResult,
	truth map[string]any,
	err error,
) error {
	if truth == nil {
		truth = scanTruth("stale", "partial", opts.Scan.Profile, currentGraphBackend())
	}
	envelope := vulnScanRepoEnvelope{
		Data:  result,
		Truth: truth,
	}
	if err != nil {
		envelope.Error = &vulnScanRepoError{Message: err.Error()}
	}
	if opts.Scan.JSON {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		return err
	}
	return renderVulnScanRepoSummary(cmd.OutOrStdout(), result)
}

func renderVulnScanRepoSummary(w io.Writer, result vulnScanRepoResult) error {
	if _, err := fmt.Fprintf(w, "Vulnerability scan: %s\n", result.ReadinessState); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Repository: %s\n", result.RepositoryID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Findings: %d", result.Count); err != nil {
		return err
	}
	if result.Truncated {
		if _, err := fmt.Fprint(w, " (truncated)"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}
