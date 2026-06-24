package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const vulnScanImpactFindingsEndpoint = "/api/v0/supply-chain/impact/findings"

// vulnScanNow is overridable so tests can pin wall-clock time without racing
// the live scanner clock. Production callers use time.Now.
var vulnScanNow = time.Now

type vulnScanRepoOptions struct {
	Scan         scanOptions
	Limit        int
	ImpactStatus string
	RepoID       string
	Broad        bool
	ExportFormat string
}

type vulnScanRepoResult struct {
	Command        string               `json:"command"`
	Status         string               `json:"status"`
	ReadinessState string               `json:"readiness_state"`
	ScopeMode      string               `json:"scope_mode"`
	Target         scanTarget           `json:"target"`
	RepositoryID   string               `json:"repository_id,omitempty"`
	Scan           scanResult           `json:"scan"`
	Findings       []map[string]any     `json:"findings"`
	Count          int                  `json:"count"`
	Limit          int                  `json:"limit"`
	Truncated      bool                 `json:"truncated"`
	NextCursor     map[string]any       `json:"next_cursor,omitempty"`
	Readiness      map[string]any       `json:"readiness,omitempty"`
	ScopePlan      *vulnScanScopePlan   `json:"scope_plan,omitempty"`
	Performance    *vulnScanPerformance `json:"scan_performance,omitempty"`
	Report         *vulnScanReport      `json:"report,omitempty"`
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
	startedAt := vulnScanNow()
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
		if localRuntime.Client == nil {
			if localRuntime.Close != nil {
				_ = localRuntime.Close()
			}
			return fmt.Errorf("local vulnerability scan runtime did not return an API client")
		}
		client = localRuntime.Client
		opts.Scan.RuntimeEnv = localRuntime.BootstrapEnv
		closeLocalRuntime = localRuntime.Close
	}
	result := newVulnScanRepoResult(opts, client.BaseURL)
	scanStdout := cmd.OutOrStdout()
	if opts.Scan.JSON || opts.ExportFormat != "" {
		scanStdout = cmd.ErrOrStderr()
	}
	scanResult, err := executeScan(cmd.Context(), scanStdout, cmd.ErrOrStderr(), client, opts.Scan, !opts.Scan.JSON)
	result.Scan = scanResult
	result.Status = scanResult.Status
	result.Warnings = append(result.Warnings, scanResult.Warnings...)
	if err != nil {
		result.ReadinessState = "target_incomplete"
		recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	if scanResult.Status != "ready" {
		result.ReadinessState = "target_incomplete"
		recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
		err := commandExitError{message: "vulnerability scan target is not ready; rerun with --wait=true before reading findings", code: 4}
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}

	repositoryID, err := resolveVulnScanRepoID(cmd, client, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	result.RepositoryID = repositoryID

	findings, err := fetchVulnScanRepoImpactFindings(client, repositoryID, opts)
	if err != nil {
		result.ReadinessState = "evidence_incomplete"
		recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, scanResult.Truth, err, closeLocalRuntime)
	}
	if findings.Error != nil {
		result.ReadinessState = "evidence_incomplete"
		recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
		err := commandExitError{message: findings.Error.Message, code: 4}
		return finishVulnScanRepoAfterCleanup(cmd, opts, result, findings.Truth, err, closeLocalRuntime)
	}
	result.Findings = findings.Data.Findings
	result.Count = findings.Data.Count
	result.Limit = findings.Data.Limit
	result.Truncated = findings.Data.Truncated
	result.NextCursor = findings.Data.NextCursor
	result.Readiness = findings.Data.Readiness
	result.ReadinessState = vulnScanReadinessState(findings.Data.Readiness, result.Count)

	scopeErr := applyVulnScanScope(&result)
	recordVulnScanPerformance(&result, startedAt, opts.Scan.Target.Root)
	if scopeErr == nil {
		scopeErr = vulnScanExitErrorForResult(result)
	}
	return finishVulnScanRepoAfterCleanup(cmd, opts, result, findings.Truth, scopeErr, closeLocalRuntime)
}

// applyVulnScanScope builds the scope plan from the readiness envelope, runs
// fail-closed guards, and surfaces the broad-mode note when the operator opted
// into wider advisory coverage. It returns the fail-closed error so the caller
// can short-circuit the success path while still emitting the JSON envelope
// with the scope plan attached.
func applyVulnScanScope(result *vulnScanRepoResult) error {
	plan := buildVulnScanScopePlan(result.ScopeMode, result.Readiness)
	state, missing, failErr := applyScopedGuards(&plan, result.ReadinessState)
	if plan.StopThreshold == "" {
		plan.StopThreshold = state
	}
	result.ScopePlan = &plan

	if plan.Mode == vulnScanScopeModeBroad {
		if failErr != nil {
			result.ReadinessState = state
			if len(missing) > 0 {
				result.Warnings = append(
					result.Warnings,
					fmt.Sprintf("broad mode fail-closed: %s", strings.Join(missing, ", ")),
				)
			}
			return failErr
		}
		result.Warnings = append(
			result.Warnings,
			"broad mode skipped advisory freshness fail-closed guard; package-registry metadata still must be fresh when observed dependencies require it",
		)
		return nil
	}
	if failErr == nil {
		return nil
	}
	result.ReadinessState = state
	if len(missing) > 0 {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("vuln-scan fail-closed: %s", strings.Join(missing, ", ")),
		)
	}
	return failErr
}

// recordVulnScanPerformance stamps the scan_performance block onto the result
// at the very end of the run so wall-time covers the full orchestrated flow
// (bootstrap, readiness wait, findings read, scope guards) rather than a
// single stage. The function is safe to call multiple times — the most recent
// call wins.
func recordVulnScanPerformance(result *vulnScanRepoResult, startedAt time.Time, repoRoot string) {
	plan := vulnScanScopePlan{Mode: result.ScopeMode, StopThreshold: result.ReadinessState}
	if result.ScopePlan != nil {
		plan = *result.ScopePlan
		if plan.StopThreshold == "" {
			plan.StopThreshold = result.ReadinessState
		}
	}
	perf := captureVulnScanPerformance(startedAt, vulnScanNow(), plan, repoRoot)
	result.Performance = &perf
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
	case "", vulnScanExportFormatSARIF, vulnScanExportFormatVEX:
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

func newVulnScanRepoResult(opts vulnScanRepoOptions, serviceURL string) vulnScanRepoResult {
	return vulnScanRepoResult{
		Command:        "vuln-scan repo",
		Status:         "failed",
		ReadinessState: "target_incomplete",
		ScopeMode:      resolveScopeMode(opts.Broad),
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
	report := buildVulnScanReport(result, vulnScanNow())
	result.Report = &report
	if opts.ExportFormat == vulnScanExportFormatSARIF {
		if err != nil && !isVulnScanScannerExit(err) {
			return err
		}
		if writeErr := writeVulnScanSARIF(cmd.OutOrStdout(), result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	if opts.ExportFormat == vulnScanExportFormatVEX {
		if err != nil && !isVulnScanScannerExit(err) {
			return err
		}
		if writeErr := writeVulnScanVEX(cmd.OutOrStdout(), result, report); writeErr != nil {
			return writeErr
		}
		return err
	}
	envelope := vulnScanRepoEnvelope{
		Data:  result,
		Truth: truth,
	}
	if err != nil && !isVulnScanFindingsExit(err) {
		envelope.Error = &vulnScanRepoError{Message: err.Error()}
	}
	if opts.Scan.JSON {
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if isVulnScanScannerExit(err) {
			if renderErr := renderVulnScanRepoSummary(cmd.OutOrStdout(), result); renderErr != nil {
				return renderErr
			}
		}
		return err
	}
	return renderVulnScanRepoSummary(cmd.OutOrStdout(), result)
}

func finishVulnScanRepoAfterCleanup(
	cmd *cobra.Command,
	opts vulnScanRepoOptions,
	result vulnScanRepoResult,
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

func vulnScanHasConfiguredServiceURL(cmd *cobra.Command) bool {
	serviceURL, _ := cmd.Flags().GetString("service-url")
	if strings.TrimSpace(serviceURL) != "" {
		return true
	}
	profile, _ := cmd.Flags().GetString("profile")
	if strings.TrimSpace(resolveConfigValue("ESHU_SERVICE_URL", profile)) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("ESHU_SERVICE_URL")) != ""
}
