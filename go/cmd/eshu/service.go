// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
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
		startPath, err := procexec.Getwd()
		if err != nil {
			return fmt.Errorf("resolve current working directory: %w", err)
		}
		workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(startPath, workspaceRootFlag)
		if err != nil {
			return err
		}

		binary, err := procexec.Executable()
		if err != nil {
			return fmt.Errorf("resolve eshu executable: %w", err)
		}
		env := procexec.Environ()
		if len(profileOverrides) > 0 {
			env = procexec.MergeEnvironment(env, profileOverrides)
		}
		return procexec.Exec(binary, []string{procexec.CleanExecutableArg0(binary), "local-host", "mcp-stdio", workspaceRoot}, env)
	}

	binary, err := procexec.LookPath("eshu-mcp-server")
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
	for key, value := range localsupervisor.MCPHTTPAllowUnauthenticatedOverride(host) {
		httpOverrides[key] = value
	}
	env := procexec.MergeEnvironment(procexec.Environ(), httpOverrides)
	if strings.TrimSpace(workspaceRootFlag) != "" {
		startPath, err := procexec.Getwd()
		if err != nil {
			return fmt.Errorf("resolve current working directory: %w", err)
		}
		workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(startPath, workspaceRootFlag)
		if err != nil {
			return err
		}
		layout, err := localsupervisor.BuildLayout(workspaceRoot)
		if err != nil {
			return err
		}
		env, err = localsupervisor.MCPHTTPEnvFromOwner(layout, host, port)
		if err != nil {
			return err
		}
	}

	fmt.Printf("Starting Eshu MCP Server (%s transport)...\n", transport)
	return procexec.Exec(binary, []string{"eshu-mcp-server"}, env)
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
