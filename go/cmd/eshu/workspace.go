// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
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
		Short: "Removed: use eshu index <path> or eshu admin reindex instead",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspacePlan,
	}
	workspaceCmd.AddCommand(planCmd)

	syncCmd := &cobra.Command{
		Use:   "sync <path>",
		Short: "Removed: use eshu index <path> or eshu admin reindex instead",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceSync,
	}
	workspaceCmd.AddCommand(syncCmd)

	wsIndexCmd := &cobra.Command{
		Use:   "index <path>",
		Short: "Removed: use eshu index <path> or eshu admin reindex instead",
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
	watchCmd.Flags().String("workspace-root", "", "Explicit workspace root for the local Eshu service")
	workspaceCmd.AddCommand(watchCmd)
}

// workspaceIndexingGuidance replaces the workspace plan, sync, and index
// commands, which posted a path-scoped body the reindex API never honored.
const workspaceIndexingGuidance = "Use `eshu index <path>` to index a local directory tree, or " +
	"`eshu admin reindex` to force every git ingester to re-parse all repositories."

func runWorkspacePlan(cmd *cobra.Command, args []string) error {
	return removedCommandError("eshu workspace plan", workspaceIndexingGuidance)
}

func runWorkspaceSync(cmd *cobra.Command, args []string) error {
	return removedCommandError("eshu workspace sync", workspaceIndexingGuidance)
}

func runWorkspaceIndex(cmd *cobra.Command, args []string) error {
	return removedCommandError("eshu workspace index", workspaceIndexingGuidance)
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
