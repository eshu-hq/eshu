// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/graphinstall"
	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func init() {
	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Local graph backend operations",
	}
	rootCmd.AddCommand(graphCmd)

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install local graph backend tooling",
	}
	rootCmd.AddCommand(installCmd)

	installNornicDBCmd := &cobra.Command{
		Use:   "nornicdb",
		Short: "Install the local NornicDB binary",
		Long: strings.TrimSpace(`
Install a verified local NornicDB executable into Eshu's managed home.

Eshu currently tracks the latest NornicDB main branch. Build or download the
NornicDB binary you want to evaluate, then install it from that explicit
source:

  eshu install nornicdb --from /absolute/path/to/nornicdb-headless
  eshu install nornicdb --from /absolute/path/to/nornicdb-headless-darwin-arm64.tar.gz
  eshu install nornicdb --from /absolute/path/to/NornicDB-main-arm64-lite.pkg
  eshu install nornicdb --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz --sha256 <expected-sha256>
  eshu install nornicdb --from https://example.com/releases/NornicDB-main-arm64-lite.pkg --sha256 <expected-sha256>

The no-argument installer is intentionally unavailable while this policy is in
effect because Eshu is not pinning release assets yet. Headless remains the
default laptop artifact. Use --from with a verified full-binary artifact when
you need the larger full binary.
Signature verification is still future work.
`),
		RunE: runInstallNornicDB,
	}
	installNornicDBCmd.Flags().String("from", "", "Install from a local NornicDB binary, local archive/package, or release URL")
	installNornicDBCmd.Flags().String("sha256", "", "Expected SHA-256 checksum for the --from artifact")
	installNornicDBCmd.Flags().Bool("force", false, "Replace an existing managed NornicDB binary")
	installNornicDBCmd.Flags().Bool("full", false, "Reserved for future no-argument release installs; use --from for full binary artifacts today")
	installCmd.AddCommand(installNornicDBCmd)

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show local graph backend status for the current workspace",
		RunE:  runGraphStatus,
	}
	statusCmd.Flags().String("workspace-root", "", "Explicit workspace root for local graph status")
	graphCmd.AddCommand(statusCmd)

	graphStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the local Eshu service",
		Long: strings.TrimSpace(`
Start the local Eshu service in the foreground.

The service runs the local_authoritative profile, manages embedded Postgres and
NornicDB, then supervises the ingester and reducer used by:

  ESHU_QUERY_PROFILE=local_authoritative eshu watch .

Use Ctrl-C to stop it from the same terminal, or run "eshu graph stop" from
another terminal for the same workspace.
`),
		RunE: runGraphStart,
	}
	graphStartCmd.Flags().String("workspace-root", "", "Explicit workspace root for local graph start")
	graphStartCmd.Flags().String("progress", localsupervisor.ProgressModeAuto, "Progress output mode: auto, plain, or quiet")
	graphStartCmd.Flags().String("logs", localsupervisor.LogModeFile, "Child service log output mode: file, terminal, or quiet")
	graphStartCmd.Flags().Bool("verbose", false, "Show child service logs in the terminal")
	graphCmd.AddCommand(graphStartCmd)
	graphStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the local graph backend sidecar",
		RunE:  runGraphStop,
	}
	graphStopCmd.Flags().String("workspace-root", "", "Explicit workspace root for local graph stop")
	graphCmd.AddCommand(graphStopCmd)
	graphLogsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Show local graph backend logs",
		RunE:  runGraphLogs,
	}
	graphLogsCmd.Flags().String("workspace-root", "", "Explicit workspace root for local graph logs")
	graphCmd.AddCommand(graphLogsCmd)
	graphUpgradeCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the local graph backend sidecar",
		Long: strings.TrimSpace(`
Replace the managed local NornicDB binary from a verified source artifact.

The graph backend must be stopped first. This command accepts the same binary,
archive/package, and URL sources as eshu install nornicdb:

  eshu graph upgrade --from /absolute/path/to/nornicdb-headless
  eshu graph upgrade --from https://example.com/releases/nornicdb-headless-darwin-arm64.tar.gz --sha256 <expected-sha256>
  eshu graph upgrade --from https://example.com/releases/NornicDB-1.0.42-hotfix-arm64-lite.pkg --sha256 <expected-sha256>
`),
		RunE: runGraphUpgrade,
	}
	graphUpgradeCmd.Flags().String("workspace-root", "", "Explicit workspace root for local graph upgrade")
	graphUpgradeCmd.Flags().String("from", "", "Upgrade from an existing local NornicDB binary")
	graphUpgradeCmd.Flags().String("sha256", "", "Expected SHA-256 checksum for --from")
	graphCmd.AddCommand(graphUpgradeCmd)
}

func runGraphStatus(cmd *cobra.Command, args []string) error {
	layout, err := graphLayoutFromCommand(cmd)
	if err != nil {
		return err
	}

	status, err := localsupervisor.StatusForLayout(layout)
	if err != nil {
		return err
	}
	printJSON(status)
	return nil
}

func runGraphLogs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu graph logs accepts flags only, got %d argument(s)", len(args))
	}
	layout, err := graphLayoutFromCommand(cmd)
	if err != nil {
		return err
	}
	return localsupervisor.LogsForLayout(layout, os.Stdout)
}

func runGraphStart(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu graph start accepts flags only, got %d argument(s)", len(args))
	}
	layout, err := graphLayoutFromCommand(cmd)
	if err != nil {
		return err
	}
	progressMode, err := cmd.Flags().GetString("progress")
	if err != nil {
		return err
	}
	progressMode = strings.TrimSpace(progressMode)
	if err := localsupervisor.ValidateProgressMode(progressMode); err != nil {
		return err
	}
	logMode, err := cmd.Flags().GetString("logs")
	if err != nil {
		return err
	}
	logMode = strings.TrimSpace(logMode)
	if verbose, err := cmd.Flags().GetBool("verbose"); err != nil {
		return err
	} else if verbose {
		logMode = localsupervisor.LogModeTerminal
	}
	if err := localsupervisor.ValidateLogMode(logMode); err != nil {
		return err
	}
	binary, err := procexec.Executable()
	if err != nil {
		return fmt.Errorf("resolve eshu executable: %w", err)
	}
	env := procexec.MergeEnvironment(procexec.Environ(), map[string]string{
		"ESHU_QUERY_PROFILE":            string(query.ProfileLocalAuthoritative),
		"ESHU_GRAPH_BACKEND":            string(query.GraphBackendNornicDB),
		localsupervisor.ProgressModeEnv: progressMode,
		localsupervisor.LogModeEnv:      logMode,
		localsupervisor.LogDirEnv:       layout.LogsDir,
	})
	fmt.Fprintf(os.Stderr, "Starting local Eshu service for %s...\n", layout.WorkspaceRoot)
	if logMode == localsupervisor.LogModeFile {
		fmt.Fprintf(os.Stderr, "Child service logs: %s\n", layout.LogsDir)
	}
	return procexec.Exec(binary, []string{procexec.CleanExecutableArg0(binary), "local-host", "watch", layout.WorkspaceRoot}, env)
}

func runGraphStop(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu graph stop accepts flags only, got %d argument(s)", len(args))
	}
	layout, err := graphLayoutFromCommand(cmd)
	if err != nil {
		return err
	}
	return localsupervisor.StopForLayout(layout)
}

func runGraphUpgrade(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu graph upgrade accepts flags only, got %d argument(s)", len(args))
	}
	layout, err := graphLayoutFromCommand(cmd)
	if err != nil {
		return err
	}
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	checksum, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return err
	}
	result, err := localsupervisor.UpgradeForLayout(layout, graphinstall.Options{
		From:        from,
		SHA256:      checksum,
		Force:       true,
		ReadVersion: localsupervisor.ReadGraphVersion,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

// graphLayoutFromCommand reads the --workspace-root flag and hands it to the
// supervisor, which owns workspace resolution.
func graphLayoutFromCommand(cmd *cobra.Command) (eshulocal.Layout, error) {
	explicitRoot, err := cmd.Flags().GetString("workspace-root")
	if err != nil {
		return eshulocal.Layout{}, err
	}
	return localsupervisor.LayoutForWorkspaceRoot(explicitRoot)
}
