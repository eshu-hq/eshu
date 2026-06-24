// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

const (
	scanStatusEndpoint     = "/api/v0/status/pipeline"
	scanQueryProbeEndpoint = "/api/v0/repositories?limit=1"
)

var (
	scanLookPath = exec.LookPath
	scanNow      = time.Now
	scanWait     = func(ctx context.Context, interval time.Duration) error {
		if interval <= 0 {
			return nil
		}
		timer := time.NewTimer(interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}

	scanRunBootstrap = func(ctx context.Context, binary string, args []string, env []string, stdout, stderr io.Writer) error {
		cmd := exec.CommandContext(ctx, binary)
		cmd.Args = args
		cmd.Env = env
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	}
	scanFetchPipelineStatus = func(client *APIClient) (scanPipelineStatus, error) {
		var status scanPipelineStatus
		if err := client.Get(scanStatusEndpoint, &status); err != nil {
			return scanPipelineStatus{}, err
		}
		return status, nil
	}
	scanFetchQueryProbe = func(client *APIClient) (map[string]any, error) {
		var probe map[string]any
		if err := client.Get(scanQueryProbeEndpoint, &probe); err != nil {
			return nil, err
		}
		return probe, nil
	}
)

func init() {
	scanCmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Index a local source and wait until it is queryable",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runScan,
	}
	addScanFlags(scanCmd)
	addRemoteFlags(scanCmd)
	rootCmd.AddCommand(scanCmd)
}

func addScanFlags(cmd *cobra.Command) {
	cmd.Flags().BoolP("force", "f", false, "Force re-index")
	cmd.Flags().Bool("json", false, "Write the canonical scan result envelope as JSON")
	cmd.Flags().Bool("wait", true, "Wait for indexing readiness after bootstrap completes")
	cmd.Flags().Bool("allow-partial", false, "Return success for partial or degraded readiness with warnings")
	cmd.Flags().Duration("timeout", 30*time.Minute, "Maximum time to spend proving readiness")
	cmd.Flags().Duration("poll-interval", 3*time.Second, "Readiness polling interval")
	cmd.Flags().String("discovery-report", "", "Write a discovery advisory JSON report to this path")
	cmd.Flags().String("workspace-root", "", "Explicit workspace root for source detection")
}

func runScan(cmd *cobra.Command, args []string) error {
	opts, err := scanOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	client := apiClientFromCmd(cmd)
	result, err := executeScan(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), client, opts, !opts.JSON)
	return finishScan(cmd, opts, result, err)
}

func executeScan(
	parentCtx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	client *APIClient,
	opts scanOptions,
	announce bool,
) (scanResult, error) {
	result := newScanResult(opts, client.BaseURL)
	startedAt := scanNow()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(parentCtx, opts.Timeout)
	defer cancel()

	preflight, err := scanFetchPipelineStatus(client)
	if err != nil {
		return result, fmt.Errorf("scan preflight status check: %w", err)
	}
	result.StatusReport = preflight
	queryProbe, err := scanFetchQueryProbe(client)
	if err != nil {
		return result, fmt.Errorf("scan preflight query check: %w", err)
	}
	result.QueryProbe = queryProbe

	binary, err := scanLookPath("eshu-bootstrap-index")
	if err != nil {
		return result, fmt.Errorf("eshu-bootstrap-index not found in PATH: %w", err)
	}
	result.Evidence.BootstrapBinary = binary

	bootstrapStartedAt := scanNow()
	if announce {
		_, _ = fmt.Fprintf(stdout, "Scanning %s...\n", opts.Target.Root)
	}
	if err := scanRunBootstrap(
		runCtx,
		binary,
		opts.BootstrapArgs(),
		opts.BootstrapEnv(),
		stdout,
		stderr,
	); err != nil {
		return result, fmt.Errorf("run bootstrap index: %w", err)
	}
	bootstrapCompletedAt := scanNow()
	result.Timings.BootstrapCompleteMS = durationMillis(bootstrapCompletedAt.Sub(bootstrapStartedAt))

	if !opts.Wait {
		result.Status = "submitted"
		result.Truth = scanTruth("stale", "partial", opts.Profile, currentGraphBackend())
		return result, nil
	}

	readyResult, err := waitForScanReadiness(runCtx, client, opts, result, startedAt, bootstrapCompletedAt)
	if err != nil {
		if opts.AllowPartial && readyResult.StatusReport.Health.State != "" {
			readyResult.Status = "partial"
			readyResult.Truth = scanTruth("stale", "partial", opts.Profile, currentGraphBackend())
			readyResult.Warnings = append(readyResult.Warnings, err.Error())
			return readyResult, nil
		}
		return readyResult, err
	}
	readyResult.Status = "ready"
	queryProbe, err = scanFetchQueryProbe(client)
	if err != nil {
		return readyResult, fmt.Errorf("scan query readiness check: %w", err)
	}
	readyResult.QueryProbe = queryProbe
	readyResult.Truth = scanTruth("current", "complete", opts.Profile, currentGraphBackend())
	return readyResult, nil
}

func newScanResult(opts scanOptions, serviceURL string) scanResult {
	return scanResult{
		Command: "scan",
		Status:  "failed",
		Target:  opts.Target,
		Evidence: scanEvidence{
			ServiceURL:     serviceURL,
			StatusEndpoint: scanStatusEndpoint,
			QueryEndpoint:  scanQueryProbeEndpoint,
		},
		Warnings: []string{
			"collector_complete_ms is unavailable because eshu-bootstrap-index does not emit a structured parent-process collector timestamp yet",
			"projection_complete_ms is unavailable because source-local projection completion is only logged by the bootstrap child today",
		},
	}
}

type scanOptions struct {
	Force           bool
	JSON            bool
	Wait            bool
	AllowPartial    bool
	Timeout         time.Duration
	PollInterval    time.Duration
	DiscoveryReport string
	ReposDir        string
	Profile         string
	Target          scanTarget
	RuntimeEnv      []string
}

type scanTarget struct {
	Path string `json:"path"`
	Root string `json:"root"`
	Kind string `json:"kind"`
}

func scanOptionsFromCommand(cmd *cobra.Command, args []string) (scanOptions, error) {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	explicitRoot, err := cmd.Flags().GetString("workspace-root")
	if err != nil {
		return scanOptions{}, err
	}
	target, err := resolveScanTarget(path, explicitRoot)
	if err != nil {
		return scanOptions{}, err
	}
	reposDir, err := scanReposDir(target.Root)
	if err != nil {
		return scanOptions{}, err
	}
	force, _ := cmd.Flags().GetBool("force")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	wait, _ := cmd.Flags().GetBool("wait")
	allowPartial, _ := cmd.Flags().GetBool("allow-partial")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	discoveryReport, _ := cmd.Flags().GetString("discovery-report")
	profile, _ := cmd.Flags().GetString("profile")
	if strings.TrimSpace(discoveryReport) != "" {
		discoveryReport, err = filepath.Abs(discoveryReport)
		if err != nil {
			return scanOptions{}, fmt.Errorf("resolve discovery report path %q: %w", discoveryReport, err)
		}
	}
	if timeout <= 0 {
		return scanOptions{}, fmt.Errorf("timeout must be greater than zero")
	}
	if pollInterval <= 0 {
		return scanOptions{}, fmt.Errorf("poll-interval must be greater than zero")
	}
	return scanOptions{
		Force:           force,
		JSON:            jsonOutput,
		Wait:            wait,
		AllowPartial:    allowPartial,
		Timeout:         timeout,
		PollInterval:    pollInterval,
		DiscoveryReport: discoveryReport,
		ReposDir:        reposDir,
		Profile:         profile,
		Target:          target,
	}, nil
}

func scanReposDir(root string) (string, error) {
	layout, err := eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, root)
	if err != nil {
		return "", fmt.Errorf("resolve scan cache: %w", err)
	}
	return filepath.Join(layout.CacheDir, "repos"), nil
}

func resolveScanTarget(path, explicitRoot string) (scanTarget, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return scanTarget{}, err
	}
	root, err := eshulocal.ResolveWorkspaceRoot(absPath, explicitRoot)
	if err != nil {
		return scanTarget{}, err
	}
	return scanTarget{
		Path: absPath,
		Root: root,
		Kind: scanTargetKind(root, strings.TrimSpace(explicitRoot) != ""),
	}, nil
}

func scanTargetKind(root string, explicit bool) string {
	if explicit {
		return "workspace"
	}
	if pathExists(filepath.Join(root, ".eshu.yaml")) {
		return "workspace"
	}
	if pathExists(filepath.Join(root, ".git")) {
		return "repository"
	}
	return "directory"
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (o scanOptions) BootstrapArgs() []string {
	args := []string{"eshu-bootstrap-index", "--path", o.Target.Root}
	if o.Force {
		args = append(args, "--force")
	}
	return args
}

func (o scanOptions) BootstrapEnv() []string {
	overrides := map[string]string{
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   o.Target.Root,
		"ESHU_FILESYSTEM_DIRECT": "true",
		"ESHU_REPOS_DIR":         o.ReposDir,
	}
	if strings.TrimSpace(o.DiscoveryReport) != "" {
		overrides["ESHU_DISCOVERY_REPORT"] = o.DiscoveryReport
	}
	base := eshuEnviron()
	if len(o.RuntimeEnv) > 0 {
		base = append([]string(nil), o.RuntimeEnv...)
	}
	return mergeEnvironment(base, overrides)
}

func finishScan(cmd *cobra.Command, opts scanOptions, result scanResult, err error) error {
	if opts.JSON {
		if result.Truth == nil {
			result.Truth = scanTruth("stale", "partial", opts.Profile, currentGraphBackend())
		}
		envelope := map[string]any{
			"data":  result,
			"truth": result.Truth,
			"error": nil,
		}
		if err != nil {
			envelope["error"] = map[string]any{"message": err.Error()}
		}
		if writeErr := writeScanJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		return err
	}
	switch result.Status {
	case "ready":
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Scan ready: %s\n", result.Target.Root)
	case "partial":
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Scan partial: %s\n", result.Target.Root)
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
		}
	}
	return nil
}

func writeScanJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func scanTruth(freshness, completeness, profile, backend string) map[string]any {
	return map[string]any{
		"level":        "runtime",
		"freshness":    freshness,
		"completeness": completeness,
		"profile":      profile,
		"backend":      backend,
	}
}

func currentGraphBackend() string {
	if backend := strings.TrimSpace(os.Getenv("ESHU_GRAPH_BACKEND")); backend != "" {
		return backend
	}
	return "unknown"
}

func durationMillis(d time.Duration) int64 {
	return d.Milliseconds()
}
