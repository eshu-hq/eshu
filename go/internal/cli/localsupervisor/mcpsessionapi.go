// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

// startMCPSessionCoreAPI spawns one session eshu-core-api on a reserved
// loopback port and waits for it to answer /health.
func startMCPSessionCoreAPI(ctx context.Context, dsn string, runtimeConfig RuntimeConfig, graph *ManagedGraph, layout eshulocal.Layout) (*exec.Cmd, string, string, error) {
	corePort, err := mcpSessionReservePort()
	if err != nil {
		return nil, "", "", err
	}
	coreAddr := net.JoinHostPort(mcpSessionAPIBindAddress, strconv.Itoa(corePort))
	coreEnv := ChildEnv(dsn, runtimeConfig, graph, ChildOverrides(layout, map[string]string{}, os.Getenv))
	coreCmd, err := StartChildProcess("eshu-core-api", []string{"eshu-core-api", "-addr=" + coreAddr}, coreEnv)
	if err != nil {
		return nil, "", "", err
	}
	baseURL := "http://" + coreAddr
	if err := mcpSessionWaitHTTP(ctx, baseURL+"/health", mcpSessionStartupTimeout); err != nil {
		_ = StopChildProcess(coreCmd, ShutdownTimeout)
		return nil, "", "", err
	}
	return coreCmd, "eshu-core-api", baseURL, nil
}

// startMCPSessionLocalAPI spawns one session eshu-local-api on a reserved
// loopback port and waits for it to answer /healthz. The listen address
// travels on ESHU_API_ADDR only: the in-tree API takes no -addr flag.
func startMCPSessionLocalAPI(ctx context.Context, dsn string, runtimeConfig RuntimeConfig, graph *ManagedGraph, layout eshulocal.Layout) (*exec.Cmd, string, string, error) {
	apiPort, err := mcpSessionReservePort()
	if err != nil {
		return nil, "", "", err
	}
	apiAddr := net.JoinHostPort(mcpSessionAPIBindAddress, strconv.Itoa(apiPort))
	apiEnv := ChildEnv(dsn, runtimeConfig, graph, ChildOverrides(layout, map[string]string{
		"ESHU_API_ADDR": apiAddr,
	}, os.Getenv))
	apiCmd, err := StartChildProcess("eshu-local-api", []string{"eshu-local-api"}, apiEnv)
	if err != nil {
		return nil, "", "", err
	}
	baseURL := "http://" + apiAddr
	if err := mcpSessionWaitHTTP(ctx, baseURL+"/healthz", mcpSessionStartupTimeout); err != nil {
		_ = StopChildProcess(apiCmd, ShutdownTimeout)
		return nil, "", "", err
	}
	return apiCmd, "eshu-local-api", baseURL, nil
}
