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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// scanRuntimeFor builds the production scan runtime for a resolved API client.
// It is the single place the scan family's process contact is wired: PATH
// lookup, the bootstrap child process, the inherited environment, and the API
// reads. Tests replace it to inject fakes.
var scanRuntimeFor = defaultScanRuntime

// defaultScanRuntime wires the real seams. The clock seams stay nil so
// internal/cli/scan applies its own time.Now and cancellable-timer defaults.
func defaultScanRuntime(client *APIClient) scan.Runtime {
	return scan.Runtime{
		Client:          client,
		ServiceURL:      client.BaseURL,
		Environ:         procexec.Environ(),
		LookPath:        exec.LookPath,
		RunBootstrap:    runScanBootstrap,
		FetchStatus:     scan.FetchPipelineStatus,
		FetchQueryProbe: scan.FetchQueryProbe,
	}
}

// runScanBootstrap runs the bootstrap-index child to completion. args carries
// its own argv[0], so the command is built from binary and then overridden.
func runScanBootstrap(ctx context.Context, binary string, args []string, env []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, binary)
	cmd.Args = args
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

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
	result, err := scan.Execute(
		cmd.Context(),
		cmd.OutOrStdout(),
		cmd.ErrOrStderr(),
		scanRuntimeFor(client),
		opts,
		!opts.JSON,
	)
	return finishScan(cmd, opts, result, err)
}

// scanOptionsFromCommand resolves the cobra flags into a scan.Options. Flag
// reading stays here because internal/cli/scan cannot see cobra's state.
func scanOptionsFromCommand(cmd *cobra.Command, args []string) (scan.Options, error) {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	explicitRoot, err := cmd.Flags().GetString("workspace-root")
	if err != nil {
		return scan.Options{}, err
	}
	target, err := scan.ResolveTarget(path, explicitRoot)
	if err != nil {
		return scan.Options{}, err
	}
	reposDir, err := scan.ReposDir(target.Root)
	if err != nil {
		return scan.Options{}, err
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
			return scan.Options{}, fmt.Errorf("resolve discovery report path %q: %w", discoveryReport, err)
		}
	}
	if timeout <= 0 {
		return scan.Options{}, fmt.Errorf("timeout must be greater than zero")
	}
	if pollInterval <= 0 {
		return scan.Options{}, fmt.Errorf("poll-interval must be greater than zero")
	}
	return scan.Options{
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

// pathExists reports whether a path is present on disk.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func finishScan(cmd *cobra.Command, opts scan.Options, result scan.Result, err error) error {
	if opts.JSON {
		if result.Truth == nil {
			result.Truth = scan.Truth("stale", "partial", opts.Profile, scan.CurrentGraphBackend())
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

// writeScanJSON writes an indented JSON envelope without HTML escaping. It is
// the shared writer for every CLI command that emits the canonical envelope,
// not only scan.
func writeScanJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
