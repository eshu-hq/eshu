// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"net"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
)

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
