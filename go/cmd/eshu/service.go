package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/mcp"
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
	mcpSetupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure IDE and CLI MCP integrations",
		Long: "Print platform-specific MCP client config and optionally install it.\n\n" +
			"By default this prints a safe snippet and writes nothing. Use --write to\n" +
			"merge the eshu server entry into the platform config, preserving existing\n" +
			"servers and keys. Use --hosted with --service-url for an HTTP endpoint;\n" +
			"the bearer token is emitted as a ${ESHU_API_KEY} reference, never inline.",
		RunE: runMCPSetup,
	}
	mcpSetupCmd.Flags().String("platform", "generic", "Target MCP client: "+strings.Join(supportedPlatformNames(), ", "))
	mcpSetupCmd.Flags().Bool("hosted", false, "Generate hosted HTTP setup instead of local stdio")
	mcpSetupCmd.Flags().Bool("write", false, "Merge the config into the platform's file instead of printing it")
	mcpSetupCmd.Flags().String("target", "", "Override the file path used by --write")
	mcpSetupCmd.Flags().Bool("verify", false, "Run staged verification (config, reachable, tools, first query)")
	addRemoteFlags(mcpSetupCmd)
	mcpCmd.AddCommand(mcpSetupCmd)

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
	mAlias := &cobra.Command{
		Use:    "m",
		Short:  "Shortcut for 'eshu mcp setup'",
		Hidden: false,
		RunE:   runMCPSetup,
	}
	mAlias.Flags().String("platform", "generic", "Target MCP client: "+strings.Join(supportedPlatformNames(), ", "))
	mAlias.Flags().Bool("hosted", false, "Generate hosted HTTP setup instead of local stdio")
	mAlias.Flags().Bool("write", false, "Merge the config into the platform's file instead of printing it")
	mAlias.Flags().String("target", "", "Override the file path used by --write")
	mAlias.Flags().Bool("verify", false, "Run staged verification (config, reachable, tools, first query)")
	addRemoteFlags(mAlias)
	rootCmd.AddCommand(mAlias)

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
	eshuExec       = func(binary string, args []string, env []string) error { return syscall.Exec(binary, args, env) }
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

	env := mergeEnvironment(eshuEnviron(), map[string]string{
		"ESHU_MCP_TRANSPORT": transport,
		"ESHU_MCP_ADDR":      fmt.Sprintf("%s:%d", host, port),
	})
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

	return localHostEnv(
		eshulocal.PostgresDSN("127.0.0.1", record.PostgresPort),
		runtimeConfig,
		managedGraphFromRecord(record),
		map[string]string{
			"ESHU_MCP_TRANSPORT": "http",
			"ESHU_MCP_ADDR":      fmt.Sprintf("%s:%d", host, port),
		},
	), nil
}

func runMCPSetup(cmd *cobra.Command, args []string) error {
	platformName, _ := cmd.Flags().GetString("platform")
	hosted, _ := cmd.Flags().GetBool("hosted")
	write, _ := cmd.Flags().GetBool("write")
	target, _ := cmd.Flags().GetString("target")
	verify, _ := cmd.Flags().GetBool("verify")

	platform, err := resolvePlatform(platformName)
	if err != nil {
		return err
	}

	req := mcpSetupRequest{Mode: modeLocalStdio}
	if hosted {
		req.Mode = modeHostedHTTP
		client := apiClientFromCmd(cmd)
		req.ServiceURL = client.BaseURL
		req.APIKey = client.APIKey
	}

	if write {
		return mcpSetupWrite(platform, req, target)
	}

	if verify {
		return mcpSetupVerify(cmd, platform, req)
	}

	snippet, err := renderSetupSnippet(platform, req)
	if err != nil {
		return err
	}
	fmt.Print(snippet)
	return nil
}

// mcpSetupWrite merges the eshu entry into the platform config file and reports
// where it landed. It never prints a raw token.
func mcpSetupWrite(platform *mcpPlatform, req mcpSetupRequest, target string) error {
	if !platform.Writable {
		return fmt.Errorf("platform %q does not support --write; print the snippet and add it to %s manually",
			platform.Name, platform.TargetFile)
	}
	path := strings.TrimSpace(target)
	if path == "" {
		def, err := defaultWriteTarget(platform)
		if err != nil {
			return err
		}
		path = def
	}
	if err := writeMCPServerConfig(platform, req, path); err != nil {
		return err
	}
	printSuccess(fmt.Sprintf("Merged eshu MCP server into %s", describeWriteTarget(path)))
	return nil
}

// mcpSetupVerify runs the staged verification, reusing the API client for hosted
// reachability and the embedded read-only MCP tool surface for tool visibility.
func mcpSetupVerify(cmd *cobra.Command, platform *mcpPlatform, req mcpSetupRequest) error {
	snippet, err := renderSetupSnippet(platform, req)
	if err != nil {
		snippet = ""
	}

	var health healthProber
	var query queryProber
	if req.Mode == modeHostedHTTP {
		client := apiClientFromCmd(cmd)
		health = apiHealthProber{client: client}
		query = apiQueryProber{client: client}
	}

	report := runVerification(snippet, mcp.ReadOnlyTools, health, query)
	fmt.Print(renderVerifyReport(report))
	if !report.allOK() {
		return fmt.Errorf("mcp setup verification failed")
	}
	return nil
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
	return syscall.Exec(binary, []string{"eshu-api"}, os.Environ())
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
	return syscall.Exec(binary, []string{"eshu-api"}, os.Environ())
}

func cleanExecutableArg0(binary string) string {
	name := strings.TrimSpace(filepath.Base(binary))
	if name == "" {
		return "eshu"
	}
	return name
}
