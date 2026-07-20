// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server commands",
}

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "HTTP API server commands",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Combined service commands",
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(serveCmd)

	// mcp start
	mcpStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Eshu MCP server",
		RunE:  runMCPStart,
	}
	mcpStartCmd.Flags().StringP("transport", "t", "stdio", "Transport mode: stdio, http, or sse")
	mcpStartCmd.Flags().String("host", "0.0.0.0", "Host to bind HTTP MCP server")
	mcpStartCmd.Flags().IntP("port", "p", 8080, "Port for HTTP MCP server")
	mcpStartCmd.Flags().String("workspace-root", "", "Explicit workspace root for the local Eshu owner")
	mcpStartCmd.Flags().String("profile", "", "Runtime profile for a new local owner: local_authoritative (default; embedded NornicDB graph for call-graph and Cypher answers) or local_lightweight (Postgres only, no graph). For a Neo4j-backed authoritative owner, set ESHU_QUERY_PROFILE and ESHU_GRAPH_BACKEND instead.")
	mcpCmd.AddCommand(mcpStartCmd)

	// mcp setup
	mcpCmd.AddCommand(newMCPSetupCmd())

	// mcp tools
	mcpToolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "List available MCP tools",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("MCP tools are served by the Go MCP server.")
			fmt.Println("Start the server with 'eshu mcp start' and connect via your IDE.")
		},
	}
	mcpCmd.AddCommand(mcpToolsCmd)

	// api start
	apiStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the HTTP API server",
		RunE:  runAPIStart,
	}
	apiStartCmd.Flags().String("host", "127.0.0.1", "Host to bind")
	apiStartCmd.Flags().IntP("port", "p", 8080, "Port for the API server")
	apiCmd.AddCommand(apiStartCmd)

	// serve start
	serveStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the combined HTTP API and MCP service",
		RunE:  runServeStart,
	}
	serveStartCmd.Flags().String("host", "0.0.0.0", "Host to bind")
	serveStartCmd.Flags().IntP("port", "p", 8080, "Port for the combined service")
	serveCmd.AddCommand(serveStartCmd)

	// Shortcut: eshu m -> mcp setup
	rootCmd.AddCommand(newMCPSetupAliasCmd())

	// Shortcut: eshu start -> mcp start (deprecated)
	startAlias := &cobra.Command{
		Use:        "start",
		Short:      "Deprecated: use 'eshu mcp start' instead",
		Deprecated: "use 'eshu mcp start' instead",
		RunE:       runMCPStart,
	}
	rootCmd.AddCommand(startAlias)
}

var (
	eshuExecutable = os.Executable
	eshuGetwd      = os.Getwd
	eshuLookPath   = exec.LookPath
	eshuExec       = func(binary string, args []string, env []string) error { return syscall.Exec(binary, args, env) } // #nosec G204 -- binary is resolved via LookPath from a fixed name; args are program-constructed
	eshuEnviron    = os.Environ
)

func runMCPStart(cmd *cobra.Command, args []string) error {
	rawTransport, _ := cmd.Flags().GetString("transport")
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	workspaceRootFlag, _ := cmd.Flags().GetString("workspace-root")
	profileFlag, _ := cmd.Flags().GetString("profile")
	transport, err := normalizeMCPTransport(rawTransport)
	if err != nil {
		return err
	}

	profileOverrides, err := mcpStartProfileOverrides(profileFlag)
	if err != nil {
		return err
	}
	if len(profileOverrides) > 0 && transport != "stdio" {
		return fmt.Errorf("--profile applies to the local stdio owner only; it is not valid with --transport %s", transport)
	}

	if transport == "stdio" {
		startPath, err := eshuGetwd()
		if err != nil {
			return fmt.Errorf("resolve current working directory: %w", err)
		}
		workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(startPath, workspaceRootFlag)
		if err != nil {
			return err
		}

		binary, err := eshuExecutable()
		if err != nil {
			return fmt.Errorf("resolve eshu executable: %w", err)
		}
		env := eshuEnviron()
		if len(profileOverrides) > 0 {
			env = mergeEnvironment(env, profileOverrides)
		}
		return eshuExec(binary, []string{cleanExecutableArg0(binary), "local-host", "mcp-stdio", workspaceRoot}, env)
	}

	binary, err := eshuLookPath("eshu-mcp-server")
	if err != nil {
		printError("eshu-mcp-server binary not found in PATH.")
		fmt.Println("\nThe MCP server is a Go binary. Ensure:")
		fmt.Println("  1. Go binaries are built: cd go && make build")
		fmt.Println("  2. Binary is in PATH: export PATH=$PATH:$(pwd)/go/bin")
		return fmt.Errorf("eshu-mcp-server not found")
	}

	httpOverrides := map[string]string{
		"ESHU_MCP_TRANSPORT": transport,
		"ESHU_MCP_ADDR":      fmt.Sprintf("%s:%d", host, port),
	}
	for key, value := range mcpHTTPAllowUnauthenticatedOverride(host) {
		httpOverrides[key] = value
	}
	env := mergeEnvironment(eshuEnviron(), httpOverrides)
	if strings.TrimSpace(workspaceRootFlag) != "" {
		startPath, err := eshuGetwd()
		if err != nil {
			return fmt.Errorf("resolve current working directory: %w", err)
		}
		workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(startPath, workspaceRootFlag)
		if err != nil {
			return err
		}
		layout, err := localHostBuildLayout(workspaceRoot)
		if err != nil {
			return err
		}
		env, err = localMCPHTTPEnvFromOwner(layout, host, port)
		if err != nil {
			return err
		}
	}

	fmt.Printf("Starting Eshu MCP Server (%s transport)...\n", transport)
	return eshuExec(binary, []string{"eshu-mcp-server"}, env)
}

// mcpStartProfileOverrides translates an explicit --profile request into the
// environment overrides the local owner reads. An empty request yields no
// overrides so the mcp-stdio owner applies its authoritative default and a
// running owner of any profile can still be attached. Only the two local
// profiles are accepted; production and full-stack are rejected with guidance.
func mcpStartProfileOverrides(profileFlag string) (map[string]string, error) {
	raw := strings.TrimSpace(profileFlag)
	if raw == "" {
		return nil, nil
	}
	profile, err := query.ParseQueryProfile(raw)
	if err != nil {
		return nil, fmt.Errorf("parse --profile: %w", err)
	}
	switch profile {
	case query.ProfileLocalLightweight:
		// Clear any inherited ESHU_GRAPH_BACKEND: lightweight rejects a non-empty
		// graph backend, so an explicit --profile local_lightweight must fully
		// determine the runtime config rather than fail on a shell-set backend.
		return map[string]string{
			"ESHU_QUERY_PROFILE": string(profile),
			"ESHU_GRAPH_BACKEND": "",
		}, nil
	case query.ProfileLocalAuthoritative:
		return map[string]string{
			"ESHU_QUERY_PROFILE": string(profile),
			"ESHU_GRAPH_BACKEND": string(query.GraphBackendNornicDB),
		}, nil
	default:
		return nil, fmt.Errorf(
			"eshu mcp start supports only %q or %q profiles, got %q",
			query.ProfileLocalLightweight,
			query.ProfileLocalAuthoritative,
			profile,
		)
	}
}

// normalizeMCPTransport keeps the historical sse flag value as an alias for
// the current HTTP JSON-RPC transport used by eshu-mcp-server.
func normalizeMCPTransport(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "stdio":
		return "stdio", nil
	case "http", "sse":
		return "http", nil
	default:
		return "", fmt.Errorf("unsupported MCP transport %q: expected stdio, http, or sse", raw)
	}
}

// localMCPHTTPEnvFromOwner attaches an HTTP MCP server to the active local
// owner so graph and content reads use the same workspace-scoped stores.
func localMCPHTTPEnvFromOwner(layout eshulocal.Layout, host string, port int) ([]string, error) {
	record, err := localHostReadOwnerRecord(layout.OwnerRecordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no running local Eshu service owner for workspace %q; start it with eshu graph start --workspace-root %q", layout.WorkspaceRoot, layout.WorkspaceRoot)
		}
		return nil, err
	}
	if record.WorkspaceID != "" && record.WorkspaceID != layout.WorkspaceID {
		return nil, fmt.Errorf("owner record workspace %q does not match requested workspace %q", record.WorkspaceID, layout.WorkspaceID)
	}
	if !localHostProcessAlive(record.PID) {
		return nil, fmt.Errorf("no running local Eshu service owner for workspace %q; recorded owner pid %d is not alive", layout.WorkspaceRoot, record.PID)
	}
	if !localHostSocketHealthy(record.PostgresSocketPath) {
		return nil, fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy Postgres socket", layout.WorkspaceRoot)
	}
	if record.PostgresPort <= 0 {
		return nil, fmt.Errorf("owner record for workspace %q missing postgres_port", layout.WorkspaceRoot)
	}

	runtimeConfig, err := runtimeConfigFromOwnerRecord(record)
	if err != nil {
		return nil, err
	}
	if runtimeConfig.Profile == query.ProfileLocalAuthoritative && !localHostGraphHealthy(record) {
		return nil, fmt.Errorf("local Eshu service owner for workspace %q has an unhealthy graph backend", layout.WorkspaceRoot)
	}

	overrides := map[string]string{
		"ESHU_MCP_TRANSPORT": "http",
		"ESHU_MCP_ADDR":      fmt.Sprintf("%s:%d", host, port),
	}
	for key, value := range mcpHTTPAllowUnauthenticatedOverride(host) {
		overrides[key] = value
	}
	return localHostEnv(
		eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort),
		runtimeConfig,
		managedGraphFromRecord(record),
		overrides,
	), nil
}

// mcpHTTPAllowUnauthenticatedOverride returns an ESHU_MCP_ALLOW_UNAUTHENTICATED
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
// defense-in-depth; that explicit value (visible via eshuEnviron) wins here
// regardless of host. A directly launched eshu-mcp-server binary (Compose or
// any deployment that does not go through this CLI command) never runs this
// code at all and keeps the strict default.
func mcpHTTPAllowUnauthenticatedOverride(host string) map[string]string {
	if !isLoopbackBindHost(host) {
		return nil
	}
	if localHostEnvValue(eshuEnviron(), "ESHU_MCP_ALLOW_UNAUTHENTICATED") != "" {
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

func runAPIStart(cmd *cobra.Command, args []string) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")

	binary, err := exec.LookPath("eshu-api")
	if err != nil {
		printError("eshu-api binary not found in PATH.")
		return fmt.Errorf("eshu-api not found")
	}

	if err := os.Setenv("ESHU_API_ADDR", fmt.Sprintf("%s:%d", host, port)); err != nil {
		return err
	}
	fmt.Printf("Starting Eshu HTTP API on %s:%d...\n", host, port)
	return syscall.Exec(binary, []string{"eshu-api"}, os.Environ()) // #nosec G204 -- binary is LookPath("eshu-api"); args literal
}

func runServeStart(cmd *cobra.Command, args []string) error {
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")

	binary, err := exec.LookPath("eshu-api")
	if err != nil {
		printError("eshu-api binary not found in PATH.")
		return fmt.Errorf("eshu-api not found")
	}

	if err := os.Setenv("ESHU_API_ADDR", fmt.Sprintf("%s:%d", host, port)); err != nil {
		return err
	}
	fmt.Printf("Starting Eshu service (HTTP API + MCP) on %s:%d...\n", host, port)
	return syscall.Exec(binary, []string{"eshu-api"}, os.Environ()) // #nosec G204 -- binary is LookPath("eshu-api"); args literal
}

func cleanExecutableArg0(binary string) string {
	name := strings.TrimSpace(filepath.Base(binary))
	if name == "" {
		return "eshu"
	}
	return name
}
