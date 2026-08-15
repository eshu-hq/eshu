// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/graphinstall"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

var (
	graphGetwd       = os.Getwd
	graphBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, workspaceRoot)
	}
	graphReadOwnerRecord    = eshulocal.ReadOwnerRecord
	graphAcquireOwnerLock   = eshulocal.AcquireOwnerLock
	graphResolveBinary      = ResolveGraphBinary
	graphReadVersion        = readLocalGraphVersion
	graphProcessAlive       = eshulocal.ProcessAlive
	graphOwnerSocketHealthy = eshulocal.SocketHealthy
	graphStopPostgres       = eshulocal.StopEmbeddedPostgres
	graphStopGraphHealthy   = graphHealthyFromOwnerRecord
	graphStopRecordedGraph  = stopRecordedLocalGraph
	graphSignalProcess      = signalProcess
	graphStopPollInterval   = 200 * time.Millisecond
	graphStopTimeout        = GraphShutdownTimeout
	graphInstallNornicDB    = graphinstall.Install
)

// StatusOutput is the `eshu graph status` payload. The CLI wrapper marshals it;
// this package fills it from the owner record and the installed backend binary.
type StatusOutput struct {
	WorkspaceRoot   string `json:"workspace_root"`
	WorkspaceID     string `json:"workspace_id"`
	OwnerPresent    bool   `json:"owner_present"`
	OwnerPID        int    `json:"owner_pid,omitempty"`
	OwnerStarted    string `json:"owner_started_at,omitempty"`
	Profile         string `json:"profile,omitempty"`
	GraphBackend    string `json:"graph_backend,omitempty"`
	GraphInstalled  bool   `json:"graph_installed"`
	GraphBinaryPath string `json:"graph_binary_path,omitempty"`
	GraphRunning    bool   `json:"graph_running"`
	GraphPID        int    `json:"graph_pid,omitempty"`
	GraphAddress    string `json:"graph_address,omitempty"`
	GraphBoltPort   int    `json:"graph_bolt_port,omitempty"`
	GraphHTTPPort   int    `json:"graph_http_port,omitempty"`
	GraphDataDir    string `json:"graph_data_dir,omitempty"`
	GraphLogPath    string `json:"graph_log_path,omitempty"`
	GraphVersion    string `json:"graph_version,omitempty"`
}

// LayoutForWorkspaceRoot resolves the workspace layout the graph subcommands
// operate on. explicitRoot is the `--workspace-root` value the CLI wrapper read
// off the cobra command; an empty value resolves the workspace from the current
// working directory.
//
// It is a variable so a CLI wrapper test can pin the layout and exercise flag
// reading and output without a real workspace on disk.
var LayoutForWorkspaceRoot = layoutForWorkspaceRoot

func layoutForWorkspaceRoot(explicitRoot string) (eshulocal.Layout, error) {
	startPath, err := graphGetwd()
	if err != nil {
		return eshulocal.Layout{}, fmt.Errorf("resolve current working directory: %w", err)
	}
	workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(startPath, explicitRoot)
	if err != nil {
		return eshulocal.Layout{}, err
	}
	layout, err := graphBuildLayout(workspaceRoot)
	if err != nil {
		return eshulocal.Layout{}, err
	}
	return layout, nil
}

// StatusForLayout reports what the workspace's local graph backend is doing:
// whether a backend binary is installed, whether an owner holds the workspace,
// and whether the recorded graph process answers HTTP and Bolt.
func StatusForLayout(layout eshulocal.Layout) (StatusOutput, error) {
	status := StatusOutput{
		WorkspaceRoot: layout.WorkspaceRoot,
		WorkspaceID:   layout.WorkspaceID,
		GraphLogPath:  filepath.Join(layout.LogsDir, "graph-nornicdb.log"),
	}
	if binaryPath, err := graphResolveBinary(); err == nil {
		status.GraphInstalled = true
		status.GraphBinaryPath = binaryPath
		if version, versionErr := graphReadVersion(binaryPath); versionErr == nil {
			status.GraphVersion = version
		}
	}

	record, err := graphReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return StatusOutput{}, err
	}

	status.OwnerPresent = true
	status.OwnerPID = record.PID
	status.OwnerStarted = record.StartedAt
	status.GraphPID = record.GraphPID
	status.GraphAddress = record.GraphAddress
	status.GraphBoltPort = record.GraphBoltPort
	status.GraphHTTPPort = record.GraphHTTPPort
	status.GraphDataDir = record.GraphDataDir
	if record.GraphVersion != "" {
		status.GraphVersion = record.GraphVersion
	}

	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return StatusOutput{}, err
	}
	status.Profile = string(runtimeConfig.Profile)
	status.GraphBackend = string(runtimeConfig.GraphBackend)

	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		status.GraphRunning = graphHealthyFromOwnerRecord(record)
	}

	return status, nil
}

// LogsForLayout copies the workspace graph backend log to out, and writes it
// nowhere else, so a test can pass a buffer and read back exactly what an
// operator would see. `eshu graph logs` passes os.Stdout. A nil out discards.
func LogsForLayout(layout eshulocal.Layout, out io.Writer) error {
	logPath := filepath.Join(layout.LogsDir, "graph-nornicdb.log")
	file, err := os.Open(logPath) // #nosec G304 -- logPath is program-constructed from layout.LogsDir and a fixed filename literal
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("graph log does not exist at %q; start local_authoritative with eshu watch first", logPath)
		}
		return fmt.Errorf("open graph log %q: %w", logPath, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := io.Copy(writerOrDiscard(out), file); err != nil {
		return fmt.Errorf("print graph log %q: %w", logPath, err)
	}
	return nil
}

// StopForLayout stops the workspace's local Eshu service. A lightweight owner is
// signalled directly; an authoritative owner is signalled so it tears its graph
// backend down itself. When no live owner remains, the stale record is reclaimed
// under the owner lock instead of being left to block the next start.
func StopForLayout(layout eshulocal.Layout) error {
	record, err := graphReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no local Eshu service record for workspace %q", layout.WorkspaceRoot)
		}
		return err
	}

	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return err
	}

	if runtimeConfig.Profile == query.ProfileLocalLightweight {
		if !graphLightweightOwnerHealthy(record) {
			return graphReclaimStaleLightweightOwner(layout)
		}
		if err := graphSignalProcess(record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("signal local Eshu service pid %d: %w", record.PID, err)
		}
		return waitForOwnerStop(record, graphStopTimeout)
	}

	if record.GraphPID <= 0 {
		return fmt.Errorf("workspace %q has no local_authoritative graph backend to stop", layout.WorkspaceRoot)
	}

	if graphProcessAlive(record.PID) {
		if err := graphSignalProcess(record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("signal local Eshu service pid %d to stop graph backend: %w", record.PID, err)
		}
		return waitForGraphStop(record, graphStopTimeout)
	}

	if !graphStopGraphHealthy(record) {
		return graphReclaimStaleAuthoritativeOwner(layout)
	}
	if err := graphStopRecordedGraph(record); err != nil {
		return err
	}
	return waitForGraphStop(record, graphStopTimeout)
}

func graphLightweightOwnerHealthy(record eshulocal.OwnerRecord) bool {
	return graphProcessAlive(record.PID) && graphOwnerSocketHealthy(record.PostgresSocketPath)
}

func graphReclaimStaleLightweightOwner(layout eshulocal.Layout) (retErr error) {
	lock, err := graphAcquireOwnerLock(layout.OwnerLockPath)
	if err != nil {
		return fmt.Errorf("reclaim stale local lightweight owner: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("release owner lock: %w", err)
		}
	}()

	record, err := graphReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return err
	}
	if runtimeConfig.Profile != query.ProfileLocalLightweight {
		return fmt.Errorf("owner record changed to profile %q while reclaiming local lightweight stop", runtimeConfig.Profile)
	}
	if graphOwnerSocketHealthy(record.SocketPath) {
		return fmt.Errorf("%w: socket=%q", eshulocal.ErrWorkspaceOwnerActive, record.SocketPath)
	}
	if graphRecordedPostgresActive(record) {
		if record.PostgresDataDir == "" {
			return fmt.Errorf("%w: postgres_data_dir is required when postgres appears active", eshulocal.ErrInvalidOwnerRecord)
		}
		if err := graphStopPostgres(record.PostgresDataDir); err != nil {
			return fmt.Errorf("stop stale embedded postgres: %w", err)
		}
		if graphRecordedPostgresActive(record) {
			return fmt.Errorf("%w: pid=%d socket=%q data_dir=%q", eshulocal.ErrEmbeddedPostgresActive, record.PostgresPID, record.PostgresSocketPath, record.PostgresDataDir)
		}
	}
	return removeStaleOwnerRecord(layout.OwnerRecordPath)
}

func graphReclaimStaleAuthoritativeOwner(layout eshulocal.Layout) (retErr error) {
	lock, err := graphAcquireOwnerLock(layout.OwnerLockPath)
	if err != nil {
		return fmt.Errorf("reclaim stale local authoritative owner: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("release owner lock: %w", err)
		}
	}()

	record, err := graphReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return err
	}
	if runtimeConfig.Profile != query.ProfileLocalAuthoritative {
		return fmt.Errorf("owner record changed to profile %q while reclaiming local authoritative stop", runtimeConfig.Profile)
	}
	if graphProcessAlive(record.PID) || graphStopGraphHealthy(record) {
		return fmt.Errorf("%w: pid=%d graph_pid=%d", eshulocal.ErrWorkspaceOwnerActive, record.PID, record.GraphPID)
	}
	if graphRecordedPostgresActive(record) {
		if record.PostgresDataDir == "" {
			return fmt.Errorf("%w: postgres_data_dir is required when postgres appears active", eshulocal.ErrInvalidOwnerRecord)
		}
		if err := graphStopPostgres(record.PostgresDataDir); err != nil {
			return fmt.Errorf("stop stale embedded postgres: %w", err)
		}
		if graphRecordedPostgresActive(record) {
			return fmt.Errorf("%w: pid=%d socket=%q data_dir=%q", eshulocal.ErrEmbeddedPostgresActive, record.PostgresPID, record.PostgresSocketPath, record.PostgresDataDir)
		}
	}
	return removeStaleOwnerRecord(layout.OwnerRecordPath)
}

func graphRecordedPostgresActive(record eshulocal.OwnerRecord) bool {
	return graphProcessAlive(record.PostgresPID) || graphOwnerSocketHealthy(record.PostgresSocketPath)
}

func removeStaleOwnerRecord(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale owner record %q: %w", path, err)
	}
	return nil
}

func waitForOwnerStop(record eshulocal.OwnerRecord, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !graphProcessAlive(record.PID) {
			return nil
		}
		time.Sleep(graphStopPollInterval)
	}
	return fmt.Errorf("local Eshu service pid %d did not stop within %s", record.PID, timeout)
}

// UpgradeForLayout replaces the managed graph backend binary. It refuses while
// the workspace owner or its graph backend is still alive, because replacing the
// binary underneath a running backend leaves the recorded version lying.
func UpgradeForLayout(layout eshulocal.Layout, opts graphinstall.Options) (graphinstall.Result, error) {
	record, err := graphReadOwnerRecord(layout.OwnerRecordPath)
	if err == nil && (graphProcessAlive(record.PID) || graphStopGraphHealthy(record)) {
		return graphinstall.Result{}, fmt.Errorf("workspace graph backend is running; run eshu graph stop before upgrade")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return graphinstall.Result{}, err
	}
	opts.Force = true
	return graphInstallNornicDB(opts)
}

func waitForGraphStop(record eshulocal.OwnerRecord, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !graphStopGraphHealthy(record) {
			return nil
		}
		time.Sleep(graphStopPollInterval)
	}
	return fmt.Errorf("graph backend pid %d did not stop within %s", record.GraphPID, timeout)
}

func signalProcess(pid int, signal os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return process.Signal(signal)
}

// ValidateProgressMode rejects a `--progress` value the owner cannot honour.
func ValidateProgressMode(mode string) error {
	switch mode {
	case ProgressModeAuto, ProgressModePlain, ProgressModeQuiet:
		return nil
	default:
		return fmt.Errorf("unsupported --progress %q; expected %s, %s, or %s", mode, ProgressModeAuto, ProgressModePlain, ProgressModeQuiet)
	}
}

// ValidateLogMode rejects a `--logs` value the child-process supervisor cannot
// honour.
func ValidateLogMode(mode string) error {
	switch mode {
	case LogModeFile, LogModeTerminal, logModeQuiet:
		return nil
	default:
		return fmt.Errorf("unsupported --logs %q; expected %s, %s, or %s", mode, LogModeFile, LogModeTerminal, logModeQuiet)
	}
}

// writerOrDiscard keeps a nil writer from panicking a supervisor goroutine that
// only wanted to report progress.
func writerOrDiscard(out io.Writer) io.Writer {
	if out == nil {
		return io.Discard
	}
	return out
}
