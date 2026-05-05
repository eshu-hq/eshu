package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

var (
	indexLookPath = exec.LookPath
	indexExec     = func(binary string, args []string, env []string) error { return syscall.Exec(binary, args, env) }
)

func init() {
	// index
	indexCmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index a local path into the code graph",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runIndex,
	}
	indexCmd.Flags().BoolP("force", "f", false, "Force re-index")
	indexCmd.Flags().String("discovery-report", "", "Write a discovery advisory JSON report to this path")
	rootCmd.AddCommand(indexCmd)

	// index-status
	indexStatusCmd := &cobra.Command{
		Use:   "index-status",
		Short: "Show the latest checkpointed indexing status",
		Args:  cobra.NoArgs,
		RunE:  runIndexStatus,
	}
	addRemoteFlags(indexStatusCmd)
	rootCmd.AddCommand(indexStatusCmd)

	// list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all indexed repositories",
		RunE:  runList,
	}
	rootCmd.AddCommand(listCmd)

	// stats
	statsCmd := &cobra.Command{
		Use:   "stats [path]",
		Short: "Show indexing statistics",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runStats,
	}
	rootCmd.AddCommand(statsCmd)

	// delete
	deleteCmd := &cobra.Command{
		Use:   "delete [path]",
		Short: "Removed: use admin deletion flows instead",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDelete,
	}
	deleteCmd.Flags().Bool("all", false, "Delete all indexed repositories")
	rootCmd.AddCommand(deleteCmd)

	// clean
	cleanCmd := &cobra.Command{
		Use:   "clean",
		Short: "Removed: use admin cleanup flows instead",
		RunE:  runClean,
	}
	rootCmd.AddCommand(cleanCmd)

	// query
	queryCmd := &cobra.Command{
		Use:   "query <query>",
		Short: "Execute a language query against indexed code",
		Args:  cobra.ExactArgs(1),
		RunE:  runQuery,
	}
	rootCmd.AddCommand(queryCmd)

	// watch
	watchCmd := &cobra.Command{
		Use:   "watch [path]",
		Short: "Watch a local path for changes and update the graph",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWatch,
	}
	watchCmd.Flags().String("scope", "auto", "Watch scope: auto, repo, or workspace")
	watchCmd.Flags().String("workspace-root", "", "Explicit workspace root for the local Eshu service")
	rootCmd.AddCommand(watchCmd)

	// unwatch
	unwatchCmd := &cobra.Command{
		Use:   "unwatch <path>",
		Short: "Removed: watcher lifecycle is owned by the Go ingester runtime",
		Args:  cobra.ExactArgs(1),
		RunE:  runUnwatch,
	}
	rootCmd.AddCommand(unwatchCmd)

	// watching
	watchingCmd := &cobra.Command{
		Use:   "watching",
		Short: "Removed: watcher lifecycle is owned by the Go ingester runtime",
		RunE:  runWatching,
	}
	rootCmd.AddCommand(watchingCmd)

	// add-package
	addPkgCmd := &cobra.Command{
		Use:   "add-package <name> <language>",
		Short: "Removed: package indexing is owned by the Go bootstrap runtime",
		Args:  cobra.ExactArgs(2),
		RunE:  runAddPackage,
	}
	rootCmd.AddCommand(addPkgCmd)

	// finalize (deprecated)
	finalizeCmd := &cobra.Command{
		Use:        "finalize",
		Short:      "Deprecated: recovery is owned by the Go ingester",
		Deprecated: "use Go ingester admin endpoints instead",
		RunE: func(cmd *cobra.Command, args []string) error {
			printError("'eshu finalize' has been removed.")
			_, _ = fmt.Fprintln(os.Stderr, "Recovery operations are now owned by the Go ingester.")
			_, _ = fmt.Fprintln(os.Stderr, "Use the ingester admin endpoints:")
			_, _ = fmt.Fprintln(os.Stderr, "  POST http://<ingester>:8080/admin/refinalize")
			_, _ = fmt.Fprintln(os.Stderr, "  POST http://<ingester>:8080/admin/replay")
			return fmt.Errorf("command removed")
		},
	}
	rootCmd.AddCommand(finalizeCmd)

	// Shortcuts
	indexShortcutCmd := &cobra.Command{
		Use:   "i [path]",
		Short: "Shortcut for 'eshu index'",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runIndex,
	}
	indexShortcutCmd.Flags().BoolP("force", "f", false, "Force re-index")
	indexShortcutCmd.Flags().String("discovery-report", "", "Write a discovery advisory JSON report to this path")
	rootCmd.AddCommand(indexShortcutCmd)
	rootCmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "Shortcut for 'eshu list'",
		RunE:  runList,
	})
	rmCmd := &cobra.Command{
		Use:   "rm [path]",
		Short: "Shortcut for 'eshu delete'",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runDelete,
	}
	rmCmd.Flags().Bool("all", false, "Delete all indexed repositories")
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(&cobra.Command{
		Use:   "w [path]",
		Short: "Shortcut for 'eshu watch'",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWatch,
	})
}

func runIndex(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	discoveryReport, _ := cmd.Flags().GetString("discovery-report")

	binary, err := indexLookPath("eshu-bootstrap-index")
	if err != nil {
		printError("eshu-bootstrap-index binary not found in PATH.")
		fmt.Println("Build with: cd go && go build -o bin/ ./cmd/bootstrap-index/")
		return fmt.Errorf("eshu-bootstrap-index not found")
	}

	cmdArgs := []string{"eshu-bootstrap-index", "--path", absPath}
	if force {
		cmdArgs = append(cmdArgs, "--force")
	}
	env := os.Environ()
	if strings.TrimSpace(discoveryReport) != "" {
		reportPath, err := filepath.Abs(discoveryReport)
		if err != nil {
			return fmt.Errorf("resolve discovery report path %q: %w", discoveryReport, err)
		}
		env = append(env, "ESHU_DISCOVERY_REPORT="+reportPath)
	}

	fmt.Printf("Indexing %s...\n", absPath)
	return indexExec(binary, cmdArgs, env)
}

func runIndexStatus(cmd *cobra.Command, args []string) error {
	client := apiClient()
	var result any
	if err := client.Get("/api/v0/index-status", &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	client := NewAPIClient("", "", "")
	var result any
	if err := client.Get("/api/v0/repositories", &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	client := NewAPIClient("", "", "")
	repoSelector := ""
	if len(args) > 0 {
		var err error
		repoSelector, err = normalizeRepositoryStatsSelector(args[0])
		if err != nil {
			return err
		}
	}
	if repoSelector != "" {
		var result any
		if err := client.Get(fmt.Sprintf("/api/v0/repositories/%s/stats", url.PathEscape(repoSelector)), &result); err != nil {
			return err
		}
		printJSON(result)
	} else {
		// Get overall pipeline status
		var result any
		if err := client.Get("/api/v0/status/pipeline", &result); err != nil {
			return err
		}
		printJSON(result)
	}
	return nil
}

func normalizeRepositoryStatsSelector(arg string) (string, error) {
	if arg == "" {
		return "", nil
	}
	if _, err := os.Stat(arg); err == nil {
		return filepath.Abs(arg)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return arg, nil
}

func runDelete(cmd *cobra.Command, args []string) error {
	allRepos, _ := cmd.Flags().GetBool("all")
	if !allRepos && len(args) == 0 {
		printError("Please provide a path or use --all to delete all repositories")
		return fmt.Errorf("missing path")
	}
	if allRepos {
		return removedCommandError(
			"eshu delete --all",
			"Use the admin API or an explicit graph mutation workflow until a dedicated Go delete command ships.",
		)
	}

	return removedCommandError(
		"eshu delete",
		"Use the admin API or an explicit graph mutation workflow until a dedicated Go delete command ships.",
	)
}

func runClean(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu clean",
		"Use the Go admin APIs directly until a dedicated cleanup command exists.",
	)
}

func runQuery(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	if err := client.Post("/api/v0/code/language-query", map[string]any{
		"query": args[0],
	}, &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	explicitRoot, err := cmd.Flags().GetString("workspace-root")
	if err != nil {
		return err
	}
	workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(path, explicitRoot)
	if err != nil {
		return err
	}

	binary, err := eshuExecutable()
	if err != nil {
		printError("eshu executable not found.")
		return fmt.Errorf("eshu executable not found")
	}

	fmt.Printf("Watching %s for changes...\n", workspaceRoot)
	return eshuExec(binary, []string{cleanExecutableArg0(binary), "local-host", "watch", workspaceRoot}, eshuEnviron())
}

func runUnwatch(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu unwatch",
		"Watcher lifecycle is currently owned by the Go ingester runtime rather than the public CLI.",
	)
}

func runWatching(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu watching",
		"Watcher lifecycle is currently owned by the Go ingester runtime rather than the public CLI.",
	)
}

func runAddPackage(cmd *cobra.Command, args []string) error {
	return removedCommandError(
		"eshu add-package",
		"Package indexing is currently owned by the Go bootstrap-index runtime, not a standalone public CLI command.",
	)
}

// addRemoteFlags adds --service-url, --api-key, and --profile flags to a command.
func addRemoteFlags(cmd *cobra.Command) {
	cmd.Flags().String("service-url", "", "API service URL (overrides config)")
	cmd.Flags().String("api-key", "", "API key for authentication")
	cmd.Flags().String("profile", "", "Config profile name")
}

// apiClient creates an APIClient from command flags or defaults.
func apiClient() *APIClient {
	return NewAPIClient("", "", "")
}

// apiClientFromCmd creates an APIClient from cobra command flags.
func apiClientFromCmd(cmd *cobra.Command) *APIClient {
	serviceURL, _ := cmd.Flags().GetString("service-url")
	apiKey, _ := cmd.Flags().GetString("api-key")
	profile, _ := cmd.Flags().GetString("profile")
	return NewAPIClient(serviceURL, apiKey, profile)
}
