package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Multi-repository workspace operations",
}

func init() {
	rootCmd.AddCommand(workspaceCmd)

	planCmd := &cobra.Command{
		Use:   "plan <path>",
		Short: "Plan workspace indexing for a directory tree",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspacePlan,
	}
	workspaceCmd.AddCommand(planCmd)

	syncCmd := &cobra.Command{
		Use:   "sync <path>",
		Short: "Sync a workspace directory tree",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceSync,
	}
	workspaceCmd.AddCommand(syncCmd)

	wsIndexCmd := &cobra.Command{
		Use:   "index <path>",
		Short: "Index all repositories in a workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceIndex,
	}
	workspaceCmd.AddCommand(wsIndexCmd)

	statusCmd := &cobra.Command{
		Use:   "status [path]",
		Short: "Show workspace indexing status",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWorkspaceStatus,
	}
	addRemoteFlags(statusCmd)
	workspaceCmd.AddCommand(statusCmd)

	watchCmd := &cobra.Command{
		Use:   "watch <path>",
		Short: "Watch all repositories in a workspace for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceWatch,
	}
	watchCmd.Flags().String("workspace-root", "", "Explicit workspace root for local host ownership")
	workspaceCmd.AddCommand(watchCmd)
}

func runWorkspacePlan(cmd *cobra.Command, args []string) error {
	fmt.Printf("Planning workspace indexing for: %s\n", args[0])
	client := NewAPIClient("", "", "")
	var result any
	if err := client.Post("/api/v0/admin/reindex", map[string]any{
		"scope":  "workspace",
		"path":   args[0],
		"action": "plan",
	}, &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runWorkspaceSync(cmd *cobra.Command, args []string) error {
	fmt.Printf("Syncing workspace: %s\n", args[0])
	client := NewAPIClient("", "", "")
	var result any
	if err := client.Post("/api/v0/admin/reindex", map[string]any{
		"scope":  "workspace",
		"path":   args[0],
		"action": "sync",
	}, &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runWorkspaceIndex(cmd *cobra.Command, args []string) error {
	fmt.Printf("Indexing workspace: %s\n", args[0])
	client := NewAPIClient("", "", "")
	var result any
	if err := client.Post("/api/v0/admin/reindex", map[string]any{
		"scope":  "workspace",
		"path":   args[0],
		"action": "index",
	}, &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runWorkspaceStatus(cmd *cobra.Command, args []string) error {
	client := apiClient()
	var result any
	if err := client.Get("/api/v0/status/pipeline", &result); err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runWorkspaceWatch(cmd *cobra.Command, args []string) error {
	if err := cmd.Flags().Set("workspace-root", args[0]); err != nil {
		return err
	}
	return runWatch(cmd, args)
}
