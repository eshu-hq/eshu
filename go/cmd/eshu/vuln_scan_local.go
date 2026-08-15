// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
	vulnScanLocalAPIBindAddress = "127.0.0.1"
	vulnScanLocalStartupTimeout = 2 * time.Minute
	vulnScanLocalPollInterval   = 200 * time.Millisecond
)

type vulnScanLocalRuntime struct {
	Client       *APIClient
	BootstrapEnv []string
	Close        func() error
}

var (
	vulnScanPrepareLocalRuntime = prepareVulnScanLocalRuntime
	vulnScanReserveLocalAPIPort = reserveVulnScanLocalAPIPort
	vulnScanStartLocalOwner     = startVulnScanLocalOwner
	vulnScanStartLocalAPI       = startVulnScanLocalAPI
	vulnScanStopLocalProcess    = localsupervisor.StopChildProcess
	vulnScanWaitLocalAPI        = waitVulnScanLocalAPI
)

func prepareVulnScanLocalRuntime(ctx context.Context, workspaceRoot string, stderr io.Writer) (vulnScanLocalRuntime, error) {
	layout, err := localsupervisor.BuildLayout(workspaceRoot)
	if err != nil {
		return vulnScanLocalRuntime{}, err
	}

	record, runtimeConfig, attached, err := attachVulnScanLocalOwner(layout)
	if err != nil {
		return vulnScanLocalRuntime{}, err
	}

	var ownerCmd *exec.Cmd
	if !attached {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "Starting local Eshu service for %s...\n", layout.WorkspaceRoot)
			_, _ = fmt.Fprintf(stderr, "Child service logs: %s\n", layout.LogsDir)
		}
		ownerCmd, err = vulnScanStartLocalOwner(ctx, layout)
		if err != nil {
			return vulnScanLocalRuntime{}, err
		}
		record, runtimeConfig, err = waitForVulnScanLocalOwner(ctx, layout, vulnScanLocalStartupTimeout)
		if err != nil {
			_ = vulnScanStopLocalProcess(ownerCmd, localsupervisor.ShutdownTimeout)
			return vulnScanLocalRuntime{}, err
		}
	}

	bootstrapEnv, err := vulnScanEnvFromOwner(layout, record, runtimeConfig, nil)
	if err != nil {
		_ = stopVulnScanLocalRuntime(nil, ownerCmd)
		return vulnScanLocalRuntime{}, err
	}

	apiPort, err := vulnScanReserveLocalAPIPort()
	if err != nil {
		_ = stopVulnScanLocalRuntime(nil, ownerCmd)
		return vulnScanLocalRuntime{}, err
	}
	apiAddr := net.JoinHostPort(vulnScanLocalAPIBindAddress, strconv.Itoa(apiPort))
	apiEnv, err := vulnScanEnvFromOwner(layout, record, runtimeConfig, map[string]string{
		"ESHU_API_ADDR": apiAddr,
	})
	if err != nil {
		_ = stopVulnScanLocalRuntime(nil, ownerCmd)
		return vulnScanLocalRuntime{}, err
	}
	apiCmd, err := vulnScanStartLocalAPI(apiEnv)
	if err != nil {
		_ = stopVulnScanLocalRuntime(nil, ownerCmd)
		return vulnScanLocalRuntime{}, err
	}

	baseURL := "http://" + apiAddr
	if err := vulnScanWaitLocalAPI(ctx, baseURL, vulnScanLocalStartupTimeout); err != nil {
		_ = stopVulnScanLocalRuntime(apiCmd, ownerCmd)
		return vulnScanLocalRuntime{}, err
	}

	return vulnScanLocalRuntime{
		Client:       NewAPIClient(baseURL, "", ""),
		BootstrapEnv: bootstrapEnv,
		Close: func() error {
			return stopVulnScanLocalRuntime(apiCmd, ownerCmd)
		},
	}, nil
}

func attachVulnScanLocalOwner(layout eshulocal.Layout) (eshulocal.OwnerRecord, localsupervisor.RuntimeConfig, bool, error) {
	record, err := localsupervisor.ReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return eshulocal.OwnerRecord{}, localsupervisor.RuntimeConfig{}, false, nil
		}
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

func waitForVulnScanLocalOwner(
	ctx context.Context,
	layout eshulocal.Layout,
	timeout time.Duration,
) (eshulocal.OwnerRecord, localsupervisor.RuntimeConfig, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(vulnScanLocalPollInterval)
	defer ticker.Stop()

	for {
		record, runtimeConfig, attached, err := attachVulnScanLocalOwner(layout)
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

func vulnScanEnvFromOwner(
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
		eshulocal.PostgresDSN(vulnScanLocalAPIBindAddress, record.PostgresPort),
		runtimeConfig,
		localsupervisor.ManagedGraphFromRecord(record),
		localsupervisor.ChildOverrides(layout, childOverrides, os.Getenv),
	)
	return env, nil
}

func reserveVulnScanLocalAPIPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(vulnScanLocalAPIBindAddress, "0"))
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

func startVulnScanLocalOwner(ctx context.Context, layout eshulocal.Layout) (*exec.Cmd, error) {
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

func startVulnScanLocalAPI(env []string) (*exec.Cmd, error) {
	return localsupervisor.StartChildProcess("eshu-api", []string{"eshu-api"}, env)
}

func waitVulnScanLocalAPI(ctx context.Context, baseURL string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: localsupervisor.GraphHealthTimeout}
	ticker := time.NewTicker(vulnScanLocalPollInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/healthz", nil)
		if err != nil {
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

func stopVulnScanLocalRuntime(apiCmd, ownerCmd *exec.Cmd) error {
	var err error
	if apiCmd != nil {
		err = errors.Join(err, vulnScanStopLocalProcess(apiCmd, localsupervisor.ShutdownTimeout))
	}
	if ownerCmd != nil {
		err = errors.Join(err, vulnScanStopLocalProcess(ownerCmd, localsupervisor.ShutdownTimeout))
	}
	return err
}
