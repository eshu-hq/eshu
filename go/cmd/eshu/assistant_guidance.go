// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/cli/assistantguidance"
	"github.com/spf13/cobra"
)

// assistantGuidanceRoot is the project root resolved from --path or cwd by the
// command's pre-run; install/status/uninstall operate relative to it.
var assistantGuidanceRoot string

// assistantPlatformFilter restricts install/status/uninstall to a single
// platform id when non-empty.
var assistantPlatformFilter string

// assistantStatusVerify enables first-run ritual diagnostics in `assistant
// status` without changing the default status table.
var assistantStatusVerify bool

// assistantInstallVerify enables the same safe ritual diagnostics after
// `assistant install` successfully writes or refreshes guidance.
var assistantInstallVerify bool

// assistantCmd groups the project-scoped assistant guidance subcommands.
var assistantCmd = &cobra.Command{
	Use:   "assistant",
	Short: "Manage project-scoped Eshu guidance for AI assistants",
	Long: `Write, inspect, and remove project-scoped instructions that tell AI
assistants (Claude Code, Codex/AGENTS.md, Cursor) to prefer Eshu's bounded
MCP/API tools for graph-backed questions and to respect Eshu truth labels.

Guidance lives inside a clearly delimited managed block, so install, reinstall,
and uninstall never disturb other content in your instruction files.`,
}

func init() {
	rootCmd.AddCommand(assistantCmd)

	assistantCmd.PersistentFlags().StringVar(&assistantGuidanceRoot, "path", "",
		"Project root to operate on (defaults to the current directory)")
	assistantCmd.PersistentFlags().StringVar(&assistantPlatformFilter, "platform", "",
		"Restrict to one assistant: claude, codex, or cursor")

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install or update Eshu guidance for supported assistants",
		RunE:  runAssistantInstall,
	}
	installCmd.Flags().BoolVar(&assistantInstallVerify, "verify", false,
		"Run safe assistant ritual diagnostics after install")
	assistantCmd.AddCommand(installCmd)
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Report whether Eshu guidance is installed and current",
		RunE:  runAssistantStatus,
	}
	statusCmd.Flags().BoolVar(&assistantStatusVerify, "verify", false,
		"Include first-run assistant ritual diagnostics")
	assistantCmd.AddCommand(statusCmd)
	assistantCmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Eshu managed guidance block from instruction files",
		RunE:  runAssistantUninstall,
	})
}

// resolveRoot returns the absolute project root from the --path flag or the
// process working directory. It stays in the command wrapper because both
// branches read process state: os.Getwd, and filepath.Abs, which resolves a
// relative --path against the same working directory.
func resolveRoot(path string) (string, error) {
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve project path %q: %w", path, err)
	}
	return abs, nil
}

// assistantSelection resolves the two persistent flags into the plain values
// internal/cli/assistantguidance takes: an absolute root and a platform list.
func assistantSelection() (string, []assistantguidance.Platform, error) {
	root, err := resolveRoot(assistantGuidanceRoot)
	if err != nil {
		return "", nil, err
	}
	platforms, err := assistantguidance.SelectPlatforms(assistantPlatformFilter)
	if err != nil {
		return "", nil, err
	}
	return root, platforms, nil
}

func runAssistantInstall(cmd *cobra.Command, _ []string) error {
	root, platforms, err := assistantSelection()
	if err != nil {
		return err
	}
	results, err := assistantguidance.NewEngine(root).Install(platforms)
	if err != nil {
		return err
	}
	return assistantguidance.RenderInstall(cmd.OutOrStdout(), root, results, assistantInstallVerify)
}

func runAssistantStatus(cmd *cobra.Command, _ []string) error {
	root, platforms, err := assistantSelection()
	if err != nil {
		return err
	}
	results, err := assistantguidance.NewEngine(root).Status(platforms)
	if err != nil {
		return err
	}
	return assistantguidance.RenderStatus(cmd.OutOrStdout(), root, results, assistantStatusVerify)
}

func runAssistantUninstall(cmd *cobra.Command, _ []string) error {
	root, platforms, err := assistantSelection()
	if err != nil {
		return err
	}
	results, err := assistantguidance.NewEngine(root).Uninstall(platforms)
	if err != nil {
		return err
	}
	assistantguidance.RenderUninstall(cmd.OutOrStdout(), root, results)
	return nil
}
