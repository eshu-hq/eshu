// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/localsupervisor"
)

// The `local-host` subtree is hidden: `eshu graph start`, `eshu watch`, and
// `eshu mcp start` re-exec into it, and no operator is meant to type it. The
// supervisor itself lives in internal/cli/localsupervisor; this file is only the
// cobra registration and the signal handling that a RunE owns.
func init() {
	localHostCmd := &cobra.Command{
		Use:    "local-host",
		Hidden: true,
		Short:  "Internal local lightweight host supervisor",
	}

	watchCmd := &cobra.Command{
		Use:    "watch <workspace-root>",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE:   runLocalHostWatch,
	}
	mcpStdioCmd := &cobra.Command{
		Use:    "mcp-stdio <workspace-root>",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE:   runLocalHostMCPStdio,
	}

	localHostCmd.AddCommand(watchCmd, mcpStdioCmd)
	rootCmd.AddCommand(localHostCmd)
}

func runLocalHostWatch(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return localsupervisor.RunOwnedHost(ctx, os.Stderr, args[0], localsupervisor.ModeWatch)
}

func runLocalHostMCPStdio(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	layout, err := localsupervisor.BuildLayout(args[0])
	if err != nil {
		return err
	}
	if attached, err := localsupervisor.RunAttachedMCPStdio(ctx, layout); attached || err != nil {
		return err
	}
	return localsupervisor.RunOwnedHostWithLayout(ctx, os.Stderr, layout, localsupervisor.ModeMCPStdio)
}
