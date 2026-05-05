package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Graph relationships and code quality analysis",
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	// analyze calls
	callsCmd := &cobra.Command{
		Use:   "calls <function>",
		Short: "Show what a function calls",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeCalls,
	}
	addRemoteFlags(callsCmd)
	addAnalyzeRepoSelectorFlags(callsCmd)
	callsCmd.Flags().Bool("transitive", false, "Include transitive callees")
	callsCmd.Flags().Int("depth", 5, "Maximum traversal depth for transitive callees")
	analyzeCmd.AddCommand(callsCmd)

	// analyze callers
	callersCmd := &cobra.Command{
		Use:   "callers <function>",
		Short: "Show what calls a function",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeCallers,
	}
	addRemoteFlags(callersCmd)
	addAnalyzeRepoSelectorFlags(callersCmd)
	callersCmd.Flags().Bool("transitive", false, "Include transitive callers")
	callersCmd.Flags().Int("depth", 5, "Maximum traversal depth for transitive callers")
	analyzeCmd.AddCommand(callersCmd)

	// analyze chain
	chainCmd := &cobra.Command{
		Use:   "chain <from> <to>",
		Short: "Show call chain between two functions",
		Args:  cobra.ExactArgs(2),
		RunE:  runAnalyzeChain,
	}
	addRemoteFlags(chainCmd)
	addAnalyzeRepoSelectorFlags(chainCmd)
	chainCmd.Flags().Int("depth", 5, "Maximum traversal depth")
	analyzeCmd.AddCommand(chainCmd)

	// analyze deps
	depsCmd := &cobra.Command{
		Use:   "deps <module>",
		Short: "Show import and dependency relationships",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeDeps,
	}
	addRemoteFlags(depsCmd)
	addAnalyzeRepoSelectorFlags(depsCmd)
	analyzeCmd.AddCommand(depsCmd)

	// analyze tree
	treeCmd := &cobra.Command{
		Use:   "tree <class>",
		Short: "Show inheritance hierarchy",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeTree,
	}
	addRemoteFlags(treeCmd)
	addAnalyzeRepoSelectorFlags(treeCmd)
	analyzeCmd.AddCommand(treeCmd)

	// analyze complexity
	complexityCmd := &cobra.Command{
		Use:   "complexity",
		Short: "Show function complexity",
		RunE:  runAnalyzeComplexity,
	}
	addRemoteFlags(complexityCmd)
	analyzeCmd.AddCommand(complexityCmd)

	// analyze dead-code
	deadCodeCmd := &cobra.Command{
		Use:   "dead-code",
		Short: "Find potentially unused functions and classes",
		RunE:  runAnalyzeDeadCode,
	}
	addRemoteFlags(deadCodeCmd)
	deadCodeCmd.Flags().String("repo", "", "Optional repository selector (ID, name, slug, or path)")
	deadCodeCmd.Flags().String("repo-id", "", "Optional repository ID filter")
	deadCodeCmd.Flags().Int("limit", 100, "Maximum dead-code candidates to return")
	deadCodeCmd.Flags().StringSlice("exclude", nil, "Decorator names to exclude from dead-code results")
	deadCodeCmd.Flags().Bool("fail-on-found", false, "Exit non-zero when any dead-code candidates are found")
	analyzeCmd.AddCommand(deadCodeCmd)

	// analyze overrides
	overridesCmd := &cobra.Command{
		Use:   "overrides <name>",
		Short: "Find implementations across classes",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeOverrides,
	}
	addRemoteFlags(overridesCmd)
	addAnalyzeRepoSelectorFlags(overridesCmd)
	analyzeCmd.AddCommand(overridesCmd)

	// analyze variable
	analyzeVarCmd := &cobra.Command{
		Use:   "variable <name>",
		Short: "Show variable definitions and usage",
		Args:  cobra.ExactArgs(1),
		RunE:  runAnalyzeVariable,
	}
	addRemoteFlags(analyzeVarCmd)
	analyzeCmd.AddCommand(analyzeVarCmd)
}

func runAnalyzeCalls(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	transitive, _ := cmd.Flags().GetBool("transitive")
	depth, _ := cmd.Flags().GetInt("depth")
	var result any
	body := map[string]any{
		"name":              args[0],
		"direction":         "outgoing",
		"relationship_type": "CALLS",
		"repo_id":           repoID,
	}
	if transitive {
		body["transitive"] = true
		body["max_depth"] = depth
	}
	err = client.Post("/api/v0/code/relationships", body, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeCallers(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	transitive, _ := cmd.Flags().GetBool("transitive")
	depth, _ := cmd.Flags().GetInt("depth")
	var result any
	body := map[string]any{
		"name":              args[0],
		"direction":         "incoming",
		"relationship_type": "CALLS",
		"repo_id":           repoID,
	}
	if transitive {
		body["transitive"] = true
		body["max_depth"] = depth
	}
	err = client.Post("/api/v0/code/relationships", body, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeChain(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	depth, _ := cmd.Flags().GetInt("depth")
	var result any
	err = client.Post("/api/v0/code/call-chain", map[string]any{
		"start":     args[0],
		"end":       args[1],
		"repo_id":   repoID,
		"max_depth": depth,
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeDeps(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	var result any
	err = client.Post("/api/v0/code/relationships", map[string]any{
		"name":              args[0],
		"direction":         "outgoing",
		"relationship_type": "IMPORTS",
		"repo_id":           repoID,
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeTree(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	var result any
	err = client.Post("/api/v0/code/relationships", map[string]any{
		"name":              args[0],
		"direction":         "both",
		"relationship_type": "INHERITS",
		"repo_id":           repoID,
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeComplexity(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/complexity", map[string]any{}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeDeadCode(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	exclusions, _ := cmd.Flags().GetStringSlice("exclude")
	limit, _ := cmd.Flags().GetInt("limit")
	failOnFound, _ := cmd.Flags().GetBool("fail-on-found")

	var result map[string]any
	err = client.Post("/api/v0/code/dead-code", map[string]any{
		"repo_id":                repoID,
		"limit":                  limit,
		"exclude_decorated_with": exclusions,
	}, &result)
	if err != nil {
		return err
	}
	if failOnFound {
		if rows, ok := result["results"].([]any); ok && len(rows) > 0 {
			return fmt.Errorf("found %d dead-code candidates", len(rows))
		}
	}
	printJSON(result)
	return nil
}

func runAnalyzeOverrides(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	repoID, err := resolveRepositorySelectorFromFlags(cmd, client)
	if err != nil {
		return err
	}
	var result any
	err = client.Post("/api/v0/code/relationships", map[string]any{
		"name":              args[0],
		"direction":         "incoming",
		"relationship_type": "OVERRIDES",
		"repo_id":           repoID,
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runAnalyzeVariable(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query":         args[0],
		"search_type":   "variable",
		"include_usage": true,
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func addAnalyzeRepoSelectorFlags(cmd *cobra.Command) {
	cmd.Flags().String("repo", "", "Optional repository selector (ID, name, slug, or path)")
	cmd.Flags().String("repo-id", "", "Optional repository ID filter")
}
