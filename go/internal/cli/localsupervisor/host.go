// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// Environment variables and mode values the supervisor and its CLI wrapper both
// name. The CLI uses them as flag defaults; the supervisor reads them back out
// of the child environment it built.
const (
	// ShutdownTimeout bounds how long a child service gets to exit after the
	// supervisor interrupts it, before it is killed.
	ShutdownTimeout                         = 5 * time.Second
	deferContentSearchIndexesEnv            = "ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES"
	reducerExpectedSourceLocalProjectorsEnv = "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"
	ProgressModeEnv                         = "ESHU_LOCAL_PROGRESS_MODE"
	LogModeEnv                              = "ESHU_LOCAL_LOG_MODE"
	LogDirEnv                               = "ESHU_LOCAL_LOG_DIR"
	ProgressModeAuto                        = "auto"
	ProgressModePlain                       = "plain"
	ProgressModeQuiet                       = "quiet"
	LogModeFile                             = "file"
	LogModeTerminal                         = "terminal"
	logModeQuiet                            = "quiet"
)

// The supervisor's injection seams. Each is a variable so a test can replace
// the real process, filesystem, or database interaction with a double; the
// exported ones are also the seams the CLI wrapper and the vuln-scan command
// share, so a test that stubs one sees the same behaviour on both sides.
var (
	// BuildLayout resolves a workspace root to the on-disk layout Eshu owns for
	// it (owner record, lock, logs, Postgres, graph, cache directories).
	BuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
		return eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, workspaceRoot)
	}
	localHostPrepareWorkspace = func(layout eshulocal.Layout) (*eshulocal.OwnerLock, error) {
		return eshulocal.PrepareWorkspace(layout, buildinfo.AppVersion(), eshulocal.StartupDeps{
			ReclaimDeps: eshulocal.ReclaimDeps{
				PIDAlive:      eshulocal.ProcessAlive,
				SocketHealthy: eshulocal.SocketHealthy,
				StopPostgres:  eshulocal.StopEmbeddedPostgres,
				GraphHealthy:  graphHealthyFromOwnerRecord,
				StopGraph:     stopRecordedLocalGraph,
			},
		})
	}
	localHostStartEmbeddedPostgres = eshulocal.StartEmbeddedPostgres
	// ReadOwnerRecord reads the record a running owner wrote for its workspace.
	ReadOwnerRecord           = eshulocal.ReadOwnerRecord
	localHostWriteOwnerRecord = eshulocal.WriteOwnerRecord
	localHostHostname         = os.Hostname
	localHostNow              = func() time.Time { return time.Now().UTC() }
	localHostLookPath         = exec.LookPath
	// ProcessAlive reports whether a recorded pid is still running.
	ProcessAlive = eshulocal.ProcessAlive
	// SocketHealthy reports whether a recorded Postgres socket accepts connections.
	SocketHealthy = eshulocal.SocketHealthy
	// GraphHealthy reports whether the graph backend named by an owner record
	// answers both HTTP and Bolt.
	GraphHealthy = graphHealthyFromOwnerRecord
	// StartChildProcess launches one supervised service with the given argv and
	// environment, wiring its stdio according to the log mode in that environment.
	StartChildProcess                = startLocalChildProcess
	localHostStartManagedGraph       = startManagedLocalGraph
	localHostResetAuthoritativeState = resetLocalAuthoritativeState
	localHostWaitChildProcess        = waitLocalChildProcess
	// WaitManagedChildren waits until the first child exits, then returns. One
	// named child may exit cleanly without that being an error.
	WaitManagedChildren = waitLocalHostChildren
	// WaitOwnerChildren waits for every child, keeping the owner alive when a
	// child in the allowed set exits cleanly.
	WaitOwnerChildren                             = waitLocalHostChildrenKeepingAllowedCleanExits
	localHostNumCPU                               = cpubudget.UsableCPUs
	localHostApplyBootstrap                       = applyLocalBootstrap
	localHostApplyGraphBootstrap                  = applyLocalGraphBootstrap
	localHostMarkGraphSchemaApplied               = markLocalGraphSchemaApplied
	localHostStartProgressReporter                = startLocalHostProgressReporter
	localHostStartDeferredContentSearchIndexes    = startDeferredContentSearchIndexes
	localHostContentSearchIndexExpectedProjectors = localContentSearchIndexExpectedProjectors
	localHostStartIaCReachabilityFinalizer        = startLocalIaCReachabilityFinalizer
)

// HostMode selects what the owner process supervises. Watch mode runs the
// indexer; MCP stdio mode additionally runs an MCP server on the process stdio.
type HostMode string

// The two owner modes. They differ in their default profile and in which child
// services the owner starts and waits on.
const (
	ModeWatch    HostMode = "watch"
	ModeMCPStdio HostMode = "mcp_stdio"
)

// RuntimeConfig is the resolved profile and graph backend an owner runs under.
// A lightweight owner carries an empty GraphBackend; an authoritative owner
// always carries one.
type RuntimeConfig struct {
	Profile      query.QueryProfile
	GraphBackend query.GraphBackend
}

// RunOwnedHost takes ownership of the workspace at workspaceRoot and supervises
// its local services until ctx is cancelled. Progress lines go to out; pass
// os.Stderr for the CLI behaviour, or a buffer to capture them.
func RunOwnedHost(ctx context.Context, out io.Writer, workspaceRoot string, mode HostMode) error {
	layout, err := BuildLayout(workspaceRoot)
	if err != nil {
		return err
	}
	return RunOwnedHostWithLayout(ctx, out, layout, mode)
}

// defaultProfileForMode picks the owner profile used when the operator has not
// requested one explicitly. An MCP stdio owner defaults to local_authoritative
// so a fresh `eshu mcp start` boots embedded Postgres + NornicDB + reducer +
// ingester in one binary and can answer graph-backed questions (transitive
// callers, import dependencies, read-only Cypher) immediately. Watch-mode owners
// (the lightweight indexer) stay Postgres-only by default.
func defaultProfileForMode(mode HostMode) query.QueryProfile {
	if mode == ModeMCPStdio {
		return query.ProfileLocalAuthoritative
	}
	return query.ProfileLocalLightweight
}

// RunOwnedHostWithLayout is RunOwnedHost against an already-resolved layout. It
// holds the workspace owner lock for its whole run and tears every resource it
// started back down on the way out, so a failure part-way through does not leave
// an embedded Postgres, a graph backend, or a child service behind.
func RunOwnedHostWithLayout(ctx context.Context, out io.Writer, layout eshulocal.Layout, mode HostMode) (retErr error) {
	out = writerOrDiscard(out)
	runtimeConfig, err := resolveLocalHostRuntimeConfigWithDefault(os.Getenv, defaultProfileForMode(mode))
	if err != nil {
		return err
	}

	lock, err := localHostPrepareWorkspace(layout)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(layout.OwnerRecordPath); err != nil && !errors.Is(err, os.ErrNotExist) && retErr == nil {
			retErr = fmt.Errorf("remove owner record: %w", err)
		}
		if closeErr := lock.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		if err := localHostResetAuthoritativeState(layout); err != nil {
			return err
		}
	}

	managedPostgres, err := localHostStartEmbeddedPostgres(ctx, layout)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := managedPostgres.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	_, _ = fmt.Fprintln(out, "bootstrapping local postgres schema...")
	if err := localHostApplyBootstrap(ctx, managedPostgres.DSN); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "local postgres schema ready")

	var managedGraph *ManagedGraph
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		managedGraph, err = localHostStartManagedGraph(ctx, layout, runtimeConfig)
		if err != nil {
			return err
		}
		defer func() {
			if err := StopManagedGraph(managedGraph, GraphShutdownTimeout); err != nil && retErr == nil {
				retErr = err
			}
		}()
		_, _ = fmt.Fprintln(out, "bootstrapping local graph schema...")
		if err := localHostApplyGraphBootstrap(ctx, runtimeConfig, managedGraph); err != nil {
			return err
		}
		if err := localHostMarkGraphSchemaApplied(ctx, managedPostgres.DSN, runtimeConfig, managedGraph); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "local graph schema ready")
	}

	hostname, err := localHostHostname()
	if err != nil {
		return fmt.Errorf("resolve hostname: %w", err)
	}
	record := eshulocal.OwnerRecord{
		PID:                os.Getpid(),
		StartedAt:          localHostNow().Format(time.RFC3339Nano),
		Hostname:           hostname,
		WorkspaceID:        layout.WorkspaceID,
		Version:            buildinfo.AppVersion(),
		PostgresPID:        managedPostgres.PID,
		PostgresPort:       managedPostgres.Port,
		PostgresDataDir:    managedPostgres.DataDir,
		PostgresSocketDir:  managedPostgres.SocketDir,
		PostgresSocketPath: managedPostgres.SocketPath,
		Profile:            string(runtimeConfig.Profile),
		GraphBackend:       string(runtimeConfig.GraphBackend),
		GraphAddress:       graphAddress(managedGraph),
		GraphPID:           graphPID(managedGraph),
		GraphBoltPort:      graphBoltPort(managedGraph),
		GraphHTTPPort:      graphHTTPPort(managedGraph),
		GraphDataDir:       graphDataDir(managedGraph),
		GraphVersion:       graphVersion(managedGraph),
		GraphUsername:      graphUsername(managedGraph),
		GraphPassword:      graphPassword(managedGraph),
	}
	if err := localHostWriteOwnerRecord(layout.OwnerRecordPath, record); err != nil {
		return err
	}

	expectedProjectors := 0
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		expectedProjectors, err = localHostContentSearchIndexExpectedProjectors(layout.WorkspaceRoot)
		if err != nil {
			return fmt.Errorf("discover workspace repos for local authoritative finalization: %w", err)
		}
		stopIaCReachability, err := localHostStartIaCReachabilityFinalizer(ctx, out, managedPostgres.DSN, expectedProjectors)
		if err != nil {
			return err
		}
		defer func() {
			if err := stopIaCReachability(); err != nil && retErr == nil {
				retErr = fmt.Errorf("stop IaC reachability finalizer: %w", err)
			}
		}()
	}

	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && deferContentSearchIndexes(os.Getenv) {
		stopDeferredIndexes, err := localHostStartDeferredContentSearchIndexes(ctx, out, managedPostgres.DSN, expectedProjectors)
		if err != nil {
			_, _ = fmt.Fprintf(out, "warning: deferred content search index maintainer unavailable: %v\n", err)
		} else {
			defer func() {
				if err := stopDeferredIndexes(); err != nil && retErr == nil {
					retErr = fmt.Errorf("stop deferred content search index maintainer: %w", err)
				}
			}()
		}
	}

	children := make([]Child, 0, 3)

	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		reducerCmd, err := StartChildProcess(
			"eshu-reducer",
			[]string{"eshu-reducer"},
			ChildEnv(managedPostgres.DSN, runtimeConfig, managedGraph, ChildOverrides(layout, localHostReducerOverrides(expectedProjectors, runtimeConfig, os.Getenv), os.Getenv)),
		)
		if err != nil {
			return err
		}
		defer func() {
			if err := StopChildProcess(reducerCmd, ShutdownTimeout); err != nil && retErr == nil {
				retErr = err
			}
		}()
		children = append(children, Child{name: "eshu-reducer", cmd: reducerCmd})
	}

	ingester, err := StartChildProcess("eshu-ingester", []string{"eshu-ingester", "--watch", layout.WorkspaceRoot}, ChildEnv(managedPostgres.DSN, runtimeConfig, managedGraph, ChildOverrides(layout, localHostIngesterOverrides(layout, mode, runtimeConfig, os.Getenv), os.Getenv)))
	if err != nil {
		return err
	}
	defer func() {
		if err := StopChildProcess(ingester, ShutdownTimeout); err != nil && retErr == nil {
			retErr = err
		}
	}()
	children = append(children, Child{name: "eshu-ingester", cmd: ingester})

	if mode == ModeWatch && localHostProgressEnabled(os.Getenv) {
		stopProgress, progressErr := localHostStartProgressReporter(
			ctx,
			layout.WorkspaceRoot,
			managedPostgres.DSN,
			runtimeConfig,
		)
		if progressErr != nil {
			_, _ = fmt.Fprintf(out, "warning: local progress reporter unavailable: %v\n", progressErr)
		} else {
			defer func() {
				if err := stopProgress(); err != nil && retErr == nil {
					retErr = fmt.Errorf("stop local progress reporter: %w", err)
				}
			}()
		}
	}

	if mode == ModeWatch && runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		return WaitOwnerChildren(ctx, children, map[string]struct{}{
			"eshu-ingester": {},
		})
	}
	if mode == ModeWatch {
		return WaitManagedChildren(ctx, children, "")
	}

	mcpServer, err := StartChildProcess("eshu-mcp-server", []string{"eshu-mcp-server"}, ChildEnv(managedPostgres.DSN, runtimeConfig, managedGraph, localHostMCPStdioOverrides()))
	if err != nil {
		return err
	}
	defer func() {
		if err := StopChildProcess(mcpServer, ShutdownTimeout); err != nil && retErr == nil {
			retErr = err
		}
	}()
	children = append(children, Child{name: "eshu-mcp-server", cmd: mcpServer})
	return WaitManagedChildren(ctx, children, "eshu-mcp-server")
}

// ChildOverrides adds the log-mode and log-directory settings every supervised
// child needs, without overwriting values the operator set in the environment.
func ChildOverrides(layout eshulocal.Layout, overrides map[string]string, getenv func(string) string) map[string]string {
	merged := make(map[string]string, len(overrides)+2)
	for key, value := range overrides {
		merged[key] = value
	}
	setOverrideIfUnset(merged, getenv, LogModeEnv, LogModeFile)
	setOverrideIfUnset(merged, getenv, LogDirEnv, layout.LogsDir)
	return merged
}

func localHostMCPStdioOverrides() map[string]string {
	return map[string]string{
		"ESHU_MCP_TRANSPORT": "stdio",
		LogModeEnv:           LogModeTerminal,
	}
}

func localHostProgressEnabled(getenv func(string) string) bool {
	return localHostProgressMode(getenv) != ProgressModeQuiet
}

func localHostProgressMode(getenv func(string) string) string {
	if getenv == nil {
		return ProgressModeAuto
	}
	mode := strings.TrimSpace(getenv(ProgressModeEnv))
	switch mode {
	case ProgressModeAuto, ProgressModePlain, ProgressModeQuiet:
		return mode
	case "":
		return ProgressModeAuto
	default:
		return ProgressModeAuto
	}
}

func localHostReducerOverrides(expectedProjectors int, runtimeConfig RuntimeConfig, getenv func(string) string) map[string]string {
	overrides := map[string]string{}
	if expectedProjectors > 0 {
		overrides[reducerExpectedSourceLocalProjectorsEnv] = strconv.Itoa(expectedProjectors)
	}
	if shouldTuneLocalAuthoritativeNornicDB(runtimeConfig) {
		setOverrideIfUnset(overrides, getenv, "ESHU_REDUCER_WORKERS", strconv.Itoa(localHostCPUCount()))
	}
	return overrides
}

// RunAttachedMCPStdio attaches an MCP stdio server to an owner that already
// holds the workspace. The bool reports whether an owner was found: false means
// the caller should start one itself. A healthy owner running a profile the
// caller explicitly asked NOT to run is reported as an error, not silently
// attached.
func RunAttachedMCPStdio(ctx context.Context, layout eshulocal.Layout) (bool, error) {
	record, err := ReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return false, nil
	}
	if !ProcessAlive(record.PID) {
		return false, nil
	}
	if !SocketHealthy(record.PostgresSocketPath) {
		return false, nil
	}
	if record.PostgresPort <= 0 {
		return false, fmt.Errorf("owner record missing postgres_port")
	}
	dsn := eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort)
	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return false, err
	}
	requestedRuntimeConfig, requestedExplicit, err := requestedAttachRuntimeConfig(os.Getenv)
	if err != nil {
		return false, err
	}
	if requestedExplicit && requestedRuntimeConfig != runtimeConfig {
		return true, fmt.Errorf(
			"local Eshu service is running profile %q with graph backend %q; requested profile %q with graph backend %q does not match",
			runtimeConfig.Profile,
			runtimeConfig.GraphBackend,
			requestedRuntimeConfig.Profile,
			requestedRuntimeConfig.GraphBackend,
		)
	}
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && !GraphHealthy(record) {
		return true, fmt.Errorf(
			"local Eshu service is running profile %q with graph backend %q, but the graph backend is unhealthy; run eshu graph status or restart the service",
			runtimeConfig.Profile,
			runtimeConfig.GraphBackend,
		)
	}

	mcpServer, err := StartChildProcess("eshu-mcp-server", []string{"eshu-mcp-server"}, ChildEnv(dsn, runtimeConfig, ManagedGraphFromRecord(record), localHostMCPStdioOverrides()))
	if err != nil {
		return true, err
	}
	return true, localHostWaitChildProcess(ctx, mcpServer)
}
