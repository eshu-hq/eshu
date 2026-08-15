// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// MCPHTTPEnvFromOwner builds the environment for an HTTP MCP server that
// attaches to the workspace's running local owner, so graph and content reads hit
// the same workspace-scoped stores the owner supervises. It refuses when no
// healthy owner holds the workspace rather than starting a second one.
func MCPHTTPEnvFromOwner(layout eshulocal.Layout, host string, port int) ([]string, error) {
	record, err := ReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no running local Eshu service owner for workspace %q; start it with eshu graph start --workspace-root %q", layout.WorkspaceRoot, layout.WorkspaceRoot)
		}
		return nil, err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return nil, fmt.Errorf("owner record workspace %q does not match requested workspace %q", record.WorkspaceID, layout.WorkspaceID)
	}
	if !ProcessAlive(record.PID) {
		return nil, fmt.Errorf("no running local Eshu service owner for workspace %q; recorded owner pid %d is not alive", layout.WorkspaceRoot, record.PID)
	}
	if !SocketHealthy(record.PostgresSocketPath) {
		return nil, fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy Postgres socket", layout.WorkspaceRoot)
	}
	if record.PostgresPort <= 0 {
		return nil, fmt.Errorf("owner record for workspace %q missing postgres_port", layout.WorkspaceRoot)
	}

	runtimeConfig, err := RuntimeConfigFromOwnerRecord(record)
	if err != nil {
		return nil, err
	}
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && !GraphHealthy(record) {
		return nil, fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy graph backend", layout.WorkspaceRoot)
	}

	overrides := map[string]string{
		"ESHU_MCP_TRANSPORT": "http",
		"ESHU_MCP_ADDR":      fmt.Sprintf("%s:%d", host, port),
	}
	for key, value := range MCPHTTPAllowUnauthenticatedOverride(host) {
		overrides[key] = value
	}
	return ChildEnv(
		eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort),
		runtimeConfig,
		ManagedGraphFromRecord(record),
		overrides,
	), nil
}

// MCPHTTPAllowUnauthenticatedOverride returns an ESHU_MCP_ALLOW_UNAUTHENTICATED
// override for the local `eshu mcp start --transport http` path, unless the
// bind host is non-loopback or the operator's own environment already sets it.
//
// Issue #5168 added a startup gate: ESHU_MCP_TRANSPORT=http with no
// resolvable credential source refuses to start unless
// ESHU_MCP_ALLOW_UNAUTHENTICATED=true. The documented local/loopback flow
// (`eshu mcp start --workspace-root <repo> --transport http --host 127.0.0.1`,
// see docs/public/run-locally/mcp-local.md) has never required any credential
// setup, so the local CLI path opts into that escape hatch by default to keep
// it working with zero configuration.
//
// The default is gated on a LOOPBACK bind so it matches the escape hatch's own
// "loopback/dev only" contract and does not silently defeat the startup gate
// on a publicly reachable bind. This matters because the Helm chart runs the
// exact same subcommand -- `eshu mcp start --transport http` -- with the
// cobra default host 0.0.0.0 (all interfaces). Gating on loopback means a
// Helm pod (0.0.0.0) does NOT get the escape hatch, so the gate correctly
// governs there; if that deployment's ESHU_API_KEY secret ever resolved
// empty, the pod fails closed instead of serving an open MCP transport. The
// chart also sets ESHU_MCP_ALLOW_UNAUTHENTICATED=false explicitly as
// defense-in-depth; that explicit value (visible via procexec.Environ) wins here
// regardless of host. A directly launched eshu-mcp-server binary (Compose or
// any deployment that does not go through this CLI command) never runs this
// code at all and keeps the strict default.
func MCPHTTPAllowUnauthenticatedOverride(host string) map[string]string {
	if !isLoopbackBindHost(host) {
		return nil
	}
	if localHostEnvValue(procexec.Environ(), "ESHU_MCP_ALLOW_UNAUTHENTICATED") != "" {
		return nil
	}
	return map[string]string{"ESHU_MCP_ALLOW_UNAUTHENTICATED": "true"}
}

// isLoopbackBindHost reports whether host binds only the loopback interface
// (127.0.0.0/8, ::1, localhost, or an empty host that defaults to loopback in
// the local CLI flow). A wildcard bind such as 0.0.0.0 or :: -- or any
// routable address -- is not loopback.
func isLoopbackBindHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
