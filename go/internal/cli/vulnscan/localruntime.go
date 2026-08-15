// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	// LocalAPIBindAddress is the loopback address the one-shot API binds. The
	// runtime this command starts is for a single local scan, so it must never
	// be reachable off the machine.
	LocalAPIBindAddress = "127.0.0.1"
	// LocalStartupTimeout bounds both the wait for the local service owner and
	// the wait for the one-shot API to answer /healthz.
	LocalStartupTimeout = 2 * time.Minute
	// LocalPollInterval is how often those two waits re-check.
	LocalPollInterval = 200 * time.Millisecond
)

// LocalRuntime is a started (or attached) local Eshu service plus the one-shot
// API in front of it. BaseURL is the API's address rather than a client,
// because the CLI's API client type lives in package main; the command wrapper
// builds the client from it.
type LocalRuntime struct {
	// BaseURL is the one-shot API's http address, empty when startup failed.
	BaseURL string
	// BootstrapEnv is the environment the scan's bootstrap child inherits, so
	// it reaches the same Postgres and graph backend as the owner.
	BootstrapEnv []string
	// Close stops whatever this call started. It is nil only when nothing was
	// started; callers that attached to an existing owner still get a Close
	// that stops the one-shot API alone.
	Close func() error
}

// The seams below are variables so tests can drive the startup sequence
// without binding ports or spawning processes. Production callers leave them
// as declared.
var (
	// PrepareLocalRuntime attaches to, or starts, the local runtime a
	// repository scan needs.
	PrepareLocalRuntime = prepareLocalRuntime
	// ReserveLocalAPIPort picks the free loopback port the one-shot API binds.
	ReserveLocalAPIPort = reserveLocalAPIPort
	// StartLocalOwner launches `eshu local-host watch` for the workspace.
	StartLocalOwner = startLocalOwner
	// StartLocalAPI launches the one-shot eshu-api child.
	StartLocalAPI = startLocalAPI
	// StopLocalProcess terminates a child started by this package.
	StopLocalProcess = localsupervisor.StopChildProcess
	// WaitLocalAPI blocks until the one-shot API answers /healthz.
	WaitLocalAPI = waitLocalAPI
)

// prepareLocalRuntime brings up everything a repository scan needs when no
// service URL is configured: a local service owner (attached if one is already
// healthy for this workspace, started otherwise) and a one-shot API in front
// of it.
//
// Every failure after a child has started stops what it started before
// returning, so a failed scan does not leave an owner or an API running.
func prepareLocalRuntime(ctx context.Context, workspaceRoot string, stderr io.Writer) (LocalRuntime, error) {
	layout, err := localsupervisor.BuildLayout(workspaceRoot)
	if err != nil {
		//nolint:wrapcheck // BuildLayout already names the workspace and what it could not resolve
		return LocalRuntime{}, err
	}

	record, runtimeConfig, attached, err := attachLocalOwner(layout)
	if err != nil {
		return LocalRuntime{}, err
	}

	var ownerCmd *exec.Cmd
	if !attached {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "Starting local Eshu service for %s...\n", layout.WorkspaceRoot)
			_, _ = fmt.Fprintf(stderr, "Child service logs: %s\n", layout.LogsDir)
		}
		ownerCmd, err = StartLocalOwner(ctx, layout)
		if err != nil {
			return LocalRuntime{}, err
		}
		record, runtimeConfig, err = waitForLocalOwner(ctx, layout, LocalStartupTimeout)
		if err != nil {
			_ = StopLocalProcess(ownerCmd, localsupervisor.ShutdownTimeout)
			return LocalRuntime{}, err
		}
	}

	bootstrapEnv, err := envFromOwner(layout, record, runtimeConfig, nil)
	if err != nil {
		_ = stopLocalRuntime(nil, ownerCmd)
		return LocalRuntime{}, err
	}

	apiPort, err := ReserveLocalAPIPort()
	if err != nil {
		_ = stopLocalRuntime(nil, ownerCmd)
		return LocalRuntime{}, err
	}
	apiAddr := net.JoinHostPort(LocalAPIBindAddress, strconv.Itoa(apiPort))
	apiEnv, err := envFromOwner(layout, record, runtimeConfig, map[string]string{
		"ESHU_API_ADDR": apiAddr,
	})
	if err != nil {
		_ = stopLocalRuntime(nil, ownerCmd)
		return LocalRuntime{}, err
	}
	apiCmd, err := StartLocalAPI(apiEnv)
	if err != nil {
		_ = stopLocalRuntime(nil, ownerCmd)
		return LocalRuntime{}, err
	}

	baseURL := "http://" + apiAddr
	if err := WaitLocalAPI(ctx, baseURL, LocalStartupTimeout); err != nil {
		_ = stopLocalRuntime(apiCmd, ownerCmd)
		return LocalRuntime{}, err
	}

	return LocalRuntime{
		BaseURL:      baseURL,
		BootstrapEnv: bootstrapEnv,
		Close: func() error {
			return stopLocalRuntime(apiCmd, ownerCmd)
		},
	}, nil
}

// attachLocalOwner reports whether a healthy local service owner already
// serves this workspace. A missing owner record is not an error — it means
// there is nothing to attach to — but an owner that exists and is wrong is:
// a mismatched workspace, an unhealthy Postgres socket, a missing port, a
// profile other than local_authoritative, or an unhealthy graph backend all
// fail rather than being silently replaced, because the answer this command
// gives depends on which service it read.
func attachLocalOwner(layout eshulocal.Layout) (eshulocal.OwnerRecord, localsupervisor.RuntimeConfig, bool, error) {
	record, err := localsupervisor.ReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false, nil
		}
		//nolint:wrapcheck // the caller matches os.ErrNotExist above; wrapping the other cases would make the pair inconsistent for no added context
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false, err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false,
			fmt.Errorf("owner record workspace %q does not match requested workspace %q", record.WorkspaceID, layout.WorkspaceID)
	}
	if !localsupervisor.ProcessAlive(record.PID) {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false, nil
	}
	if !localsupervisor.SocketHealthy(record.PostgresSocketPath) {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false,
			fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy Postgres socket", layout.WorkspaceRoot)
	}
	if record.PostgresPort <= 0 {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false,
			fmt.Errorf("owner record for workspace %q missing postgres_port", layout.WorkspaceRoot)
	}
	runtimeConfig, err := localsupervisor.RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		//nolint:wrapcheck // the owner-record error already names the field it could not read
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false, err
	}
	if runtimeConfig.Profile != query.ProfileLocalAuthoritative {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false,
			fmt.Errorf("vuln-scan repo requires local_authoritative; running local Eshu service profile is %q", runtimeConfig.Profile)
	}
	if !localsupervisor.GraphHealthy(record) {
		return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false,
			fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy graph backend", layout.WorkspaceRoot)
	}
	return record, runtimeConfig, true, nil
}

// waitForLocalOwner polls until the owner this call started becomes
// attachable, or the timeout expires. It names the log directory in the
// timeout error because that is where the reason will be.
func waitForLocalOwner(
	ctx context.Context,
	layout eshulocal.Layout,
	timeout time.Duration,
) (eshulocal.OwnerRecord, localsupervisor.RuntimeConfig, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(LocalPollInterval)
	defer ticker.Stop()

	for {
		record, runtimeConfig, attached, err := attachLocalOwner(layout)
		if err != nil {
			return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, err
		}
		if attached {
			return record, runtimeConfig, nil
		}

		select {
		case <-waitCtx.Done():
			return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{},
				fmt.Errorf("wait for local Eshu service owner timed out after %s; see %s: %w", timeout, layout.LogsDir, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// envFromOwner composes the child environment for a process that must talk to
// the same Postgres and graph backend as the owner. overrides are applied on
// top; the process environment is read through localsupervisor.ChildOverrides,
// the same way that package composes its own children's environments.
func envFromOwner(
	layout eshulocal.Layout,
	record eshulocal.OwnerRecord,
	runtimeConfig localsupervisor.RuntimeConfig,
	overrides map[string]string,
) ([]string, error) {
	if record.PostgresPort <= 0 {
		return nil, fmt.Errorf("owner record for workspace %q missing postgres_port", layout.WorkspaceRoot)
	}
	childOverrides := make(map[string]string, len(overrides))
	for key, value := range overrides {
		childOverrides[key] = value
	}
	env := localsupervisor.ChildEnv(
		eshulocal.PostgresDSN(LocalAPIBindAddress, record.PostgresPort),
		runtimeConfig,
		localsupervisor.ManagedGraphFromRecord(record),
		localsupervisor.ChildOverrides(layout, childOverrides, os.Getenv),
	)
	return env, nil
}

// reserveLocalAPIPort asks the kernel for a free loopback port and releases it
// immediately, so the API child can bind it. The gap between release and bind
// is a race the one-shot runtime accepts: a collision surfaces as a failed API
// startup, not as a wrong answer.
func reserveLocalAPIPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(LocalAPIBindAddress, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve local API port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("reserve local API port: invalid tcp address %T", listener.Addr())
	}
	return addr.Port, nil
}

// startLocalOwner launches `eshu local-host watch` for the workspace, pinned
// to the local_authoritative profile and the NornicDB backend, with progress
// quiet and logs directed to the workspace log directory so the scan's own
// output stays readable.
func startLocalOwner(ctx context.Context, layout eshulocal.Layout) (*exec.Cmd, error) {
	binary, err := procexec.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve eshu executable: %w", err)
	}
	env := procexec.MergeEnvironment(procexec.Environ(), map[string]string{
		"ESHU_QUERY_PROFILE":            string(query.ProfileLocalAuthoritative),
		"ESHU_GRAPH_BACKEND":            string(query.GraphBackendNornicDB),
		localsupervisor.ProgressModeEnv: localsupervisor.ProgressModeQuiet,
		localsupervisor.LogModeEnv:      localsupervisor.LogModeFile,
		localsupervisor.LogDirEnv:       layout.LogsDir,
	})
	if ctx == nil {
		ctx = context.Background()
	}
	args := []string{
		procexec.CleanExecutableArg0(binary),
		"local-host",
		"watch",
		layout.WorkspaceRoot,
	}
	cmd := exec.CommandContext(ctx, binary, args[1:]...) // #nosec G204 -- binary is the current process path from os.Executable(); args are program-constructed literals
	cmd.Args = args
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start eshu-local-host: %w", err)
	}
	return cmd, nil
}

// startLocalAPI launches the one-shot eshu-api child with the owner-derived
// environment.
func startLocalAPI(env []string) (*exec.Cmd, error) {
	//nolint:wrapcheck // StartChildProcess already names the child it failed to start
	return localsupervisor.StartChildProcess("eshu-api", []string{"eshu-api"}, env)
}

// waitLocalAPI polls /healthz until the one-shot API answers 200 or the
// timeout expires. Transport errors are retried rather than returned, because
// the API is still starting when the first few attempts fail.
func waitLocalAPI(ctx context.Context, baseURL string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: localsupervisor.GraphHealthTimeout}
	ticker := time.NewTicker(LocalPollInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/healthz", nil)
		if err != nil {
			//nolint:wrapcheck // net/http's error names the malformed URL, which is the whole failure
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for local vulnerability scan API timed out after %s: %w", timeout, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// stopLocalRuntime stops both children, joining the errors so one failing stop
// does not hide the other.
func stopLocalRuntime(apiCmd, ownerCmd *exec.Cmd) error {
	var err error
	if apiCmd != nil {
		err = errors.Join(err, StopLocalProcess(apiCmd, localsupervisor.ShutdownTimeout))
	}
	if ownerCmd != nil {
		err = errors.Join(err, StopLocalProcess(ownerCmd, localsupervisor.ShutdownTimeout))
	}
	return err
}
