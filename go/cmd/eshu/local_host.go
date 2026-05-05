package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const localHostShutdownTimeout = 5 * time.Second
const deferContentSearchIndexesEnv = "ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES"
const reducerExpectedSourceLocalProjectorsEnv = "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"

var (
	localHostBuildLayout = func(workspaceRoot string) (eshulocal.Layout, error) {
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
	localHostStartEmbeddedPostgres                = eshulocal.StartEmbeddedPostgres
	localHostReadOwnerRecord                      = eshulocal.ReadOwnerRecord
	localHostWriteOwnerRecord                     = eshulocal.WriteOwnerRecord
	localHostHostname                             = os.Hostname
	localHostNow                                  = func() time.Time { return time.Now().UTC() }
	localHostLookPath                             = exec.LookPath
	localHostProcessAlive                         = eshulocal.ProcessAlive
	localHostSocketHealthy                        = eshulocal.SocketHealthy
	localHostGraphHealthy                         = graphHealthyFromOwnerRecord
	localHostStartChildProcess                    = startLocalChildProcess
	localHostStartManagedGraph                    = startManagedLocalGraph
	localHostWaitChildProcess                     = waitLocalChildProcess
	localHostWaitManagedChildren                  = waitLocalHostChildren
	localHostWaitOwnerChildren                    = waitLocalHostChildrenKeepingAllowedCleanExits
	localHostApplyBootstrap                       = applyLocalBootstrap
	localHostApplyGraphBootstrap                  = applyLocalGraphBootstrap
	localHostStartProgressReporter                = startLocalHostProgressReporter
	localHostStartDeferredContentSearchIndexes    = startDeferredContentSearchIndexes
	localHostContentSearchIndexExpectedProjectors = localContentSearchIndexExpectedProjectors
	localHostStartIaCReachabilityFinalizer        = startLocalIaCReachabilityFinalizer
)

func init() {
	localHostCmd := &cobra.Command{
		Use:    "local-host",
		Hidden: true,
		Short:  "Internal local lightweight host supervisor",
	}

	watchCmd := &cobra.Command{
		Use:    "watch <workspace-root>",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE:   runLocalHostWatch,
	}
	mcpStdioCmd := &cobra.Command{
		Use:    "mcp-stdio <workspace-root>",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE:   runLocalHostMCPStdio,
	}

	localHostCmd.AddCommand(watchCmd, mcpStdioCmd)
	rootCmd.AddCommand(localHostCmd)
}

func runLocalHostWatch(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runOwnedLocalHost(ctx, args[0], localHostModeWatch)
}

func runLocalHostMCPStdio(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	layout, err := localHostBuildLayout(args[0])
	if err != nil {
		return err
	}
	if attached, err := runAttachedLocalMCPStdio(ctx, layout); attached || err != nil {
		return err
	}
	return runOwnedLocalHostWithLayout(ctx, layout, localHostModeMCPStdio)
}

type localHostMode string

const (
	localHostModeWatch    localHostMode = "watch"
	localHostModeMCPStdio localHostMode = "mcp_stdio"
)

type localHostRuntimeConfig struct {
	Profile      query.QueryProfile
	GraphBackend query.GraphBackend
}

func runOwnedLocalHost(ctx context.Context, workspaceRoot string, mode localHostMode) error {
	layout, err := localHostBuildLayout(workspaceRoot)
	if err != nil {
		return err
	}
	return runOwnedLocalHostWithLayout(ctx, layout, mode)
}

func runOwnedLocalHostWithLayout(ctx context.Context, layout eshulocal.Layout, mode localHostMode) (retErr error) {
	runtimeConfig, err := resolveLocalHostRuntimeConfig(os.Getenv)
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

	managedPostgres, err := localHostStartEmbeddedPostgres(ctx, layout)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := managedPostgres.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	fmt.Fprintln(os.Stderr, "bootstrapping local postgres schema...")
	if err := localHostApplyBootstrap(ctx, managedPostgres.DSN); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "local postgres schema ready")

	var managedGraph *managedLocalGraph
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		managedGraph, err = localHostStartManagedGraph(ctx, layout, runtimeConfig)
		if err != nil {
			return err
		}
		defer func() {
			if err := stopManagedLocalGraph(managedGraph, localGraphShutdownTimeout); err != nil && retErr == nil {
				retErr = err
			}
		}()
		fmt.Fprintln(os.Stderr, "bootstrapping local graph schema...")
		if err := localHostApplyGraphBootstrap(ctx, runtimeConfig, managedGraph); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "local graph schema ready")
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
		stopIaCReachability, err := localHostStartIaCReachabilityFinalizer(ctx, managedPostgres.DSN, expectedProjectors)
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
		stopDeferredIndexes, err := localHostStartDeferredContentSearchIndexes(ctx, managedPostgres.DSN, expectedProjectors)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: deferred content search index maintainer unavailable: %v\n", err)
		} else {
			defer func() {
				if err := stopDeferredIndexes(); err != nil && retErr == nil {
					retErr = fmt.Errorf("stop deferred content search index maintainer: %w", err)
				}
			}()
		}
	}

	children := make([]localHostChild, 0, 3)

	if runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		reducerCmd, err := localHostStartChildProcess(
			"eshu-reducer",
			[]string{"eshu-reducer"},
			localHostEnv(managedPostgres.DSN, runtimeConfig, managedGraph, localHostReducerOverrides(expectedProjectors)),
		)
		if err != nil {
			return err
		}
		defer func() {
			if err := stopLocalChildProcess(reducerCmd, localHostShutdownTimeout); err != nil && retErr == nil {
				retErr = err
			}
		}()
		children = append(children, localHostChild{name: "eshu-reducer", cmd: reducerCmd})
	}

	ingester, err := localHostStartChildProcess("eshu-ingester", []string{"eshu-ingester", "--watch", layout.WorkspaceRoot}, localHostEnv(managedPostgres.DSN, runtimeConfig, managedGraph, localHostIngesterOverrides(layout, mode, runtimeConfig)))
	if err != nil {
		return err
	}
	defer func() {
		if err := stopLocalChildProcess(ingester, localHostShutdownTimeout); err != nil && retErr == nil {
			retErr = err
		}
	}()
	children = append(children, localHostChild{name: "eshu-ingester", cmd: ingester})

	if mode == localHostModeWatch {
		stopProgress, progressErr := localHostStartProgressReporter(
			ctx,
			layout.WorkspaceRoot,
			managedPostgres.DSN,
			runtimeConfig,
		)
		if progressErr != nil {
			fmt.Fprintf(os.Stderr, "warning: local progress reporter unavailable: %v\n", progressErr)
		} else {
			defer func() {
				if err := stopProgress(); err != nil && retErr == nil {
					retErr = fmt.Errorf("stop local progress reporter: %w", err)
				}
			}()
		}
	}

	if mode == localHostModeWatch && runtimeConfig.Profile == query.ProfileLocalAuthoritative {
		return localHostWaitOwnerChildren(ctx, children, map[string]struct{}{
			"eshu-ingester": {},
		})
	}
	if mode == localHostModeWatch {
		return localHostWaitManagedChildren(ctx, children, "")
	}

	mcpServer, err := localHostStartChildProcess("eshu-mcp-server", []string{"eshu-mcp-server"}, localHostEnv(managedPostgres.DSN, runtimeConfig, managedGraph, map[string]string{
		"ESHU_MCP_TRANSPORT": "stdio",
	}))
	if err != nil {
		return err
	}
	defer func() {
		if err := stopLocalChildProcess(mcpServer, localHostShutdownTimeout); err != nil && retErr == nil {
			retErr = err
		}
	}()
	children = append(children, localHostChild{name: "eshu-mcp-server", cmd: mcpServer})
	return localHostWaitManagedChildren(ctx, children, "eshu-mcp-server")
}

func localHostReducerOverrides(expectedProjectors int) map[string]string {
	if expectedProjectors <= 0 {
		return nil
	}
	return map[string]string{
		reducerExpectedSourceLocalProjectorsEnv: strconv.Itoa(expectedProjectors),
	}
}

func runAttachedLocalMCPStdio(ctx context.Context, layout eshulocal.Layout) (bool, error) {
	record, err := localHostReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return false, nil
	}
	if !localHostProcessAlive(record.PID) {
		return false, nil
	}
	if !localHostSocketHealthy(record.PostgresSocketPath) {
		return false, nil
	}
	if record.PostgresPort <= 0 {
		return false, fmt.Errorf("owner record missing postgres_port")
	}
	dsn := eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort)
	runtimeConfig, err := runtimeConfigFromOwnerRecord(record)
	if err != nil {
		return false, err
	}
	requestedRuntimeConfig, requestedExplicit, err := requestedAttachRuntimeConfig(os.Getenv)
	if err != nil {
		return false, err
	}
	if requestedExplicit && requestedRuntimeConfig != runtimeConfig {
		return true, fmt.Errorf(
			"workspace owner is running profile %q with graph backend %q; requested profile %q with graph backend %q does not match",
			runtimeConfig.Profile,
			runtimeConfig.GraphBackend,
			requestedRuntimeConfig.Profile,
			requestedRuntimeConfig.GraphBackend,
		)
	}
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && !localHostGraphHealthy(record) {
		return true, fmt.Errorf(
			"workspace owner is running profile %q with graph backend %q, but the graph backend is unhealthy; run eshu graph status or restart the workspace owner",
			runtimeConfig.Profile,
			runtimeConfig.GraphBackend,
		)
	}

	mcpServer, err := localHostStartChildProcess("eshu-mcp-server", []string{"eshu-mcp-server"}, localHostEnv(dsn, runtimeConfig, managedGraphFromRecord(record), map[string]string{
		"ESHU_MCP_TRANSPORT": "stdio",
	}))
	if err != nil {
		return true, err
	}
	return true, localHostWaitChildProcess(ctx, mcpServer)
}
