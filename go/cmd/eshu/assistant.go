// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/assistantguidance"
	"github.com/eshu-hq/eshu/go/internal/cli/hookpreflight"
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

func init() {
	assistantCmd.AddCommand(assistantHookCommand())
}

func assistantHookCommand() *cobra.Command {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Run opt-in assistant hook helpers",
	}
	hookCmd.AddCommand(newAssistantHookPreflightCommand())
	return hookCmd
}

func newAssistantHookPreflightCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "preflight",
		Short:        "Classify an opt-in assistant fast-path preflight",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runAssistantHookPreflight,
	}
	cmd.Flags().String("host", "", "Assistant hook host family; only claude is supported")
	cmd.Flags().Bool("enabled", false, "Explicitly enable the hook preflight")
	cmd.Flags().String("trigger", "", "Trigger class such as read, search, glob, symbol, or prompt")
	cmd.Flags().String("tool", "", "Host tool name, when available")
	cmd.Flags().String("repo-path", "", "Repo-relative path scope")
	cmd.Flags().String("entity-id", "", "Eshu entity ID scope")
	cmd.Flags().String("service", "", "Service scope")
	cmd.Flags().String("workload", "", "Workload scope")
	cmd.Flags().String("environment", "", "Environment scope")
	cmd.Flags().String("resource", "", "Resource handle scope")
	cmd.Flags().String("freshness", hookpreflight.FreshnessFresh, "Freshness state: fresh, stale, building, or unavailable")
	cmd.Flags().String("permission", hookpreflight.PermissionAllowed, "Permission state: allowed or denied")
	cmd.Flags().Duration("budget", hookpreflight.DefaultBudget, "Hook preflight wall-time budget")
	cmd.Flags().Bool("json", false, "Emit Claude hook JSON when advisory context is available")
	return cmd
}

func runAssistantHookPreflight(cmd *cobra.Command, _ []string) error {
	start := time.Now()
	input, err := assistantHookInputFromCommand(cmd)
	if err != nil {
		return err
	}
	hookPayload, payloadOK := readClaudePreToolUseInput(cmd)
	if payloadOK {
		hookpreflight.MergeClaudePreToolUseInput(&input, hookPayload)
	}
	input.Elapsed = time.Since(start)
	output := hookpreflight.Evaluate(input)
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		if output.Decision != hookpreflight.DecisionAdvise {
			return nil
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(hookpreflight.ClaudePreToolUseOutputForPreflight(output))
	}
	hookpreflight.RenderPreflightText(cmd.OutOrStdout(), output)
	return nil
}

func assistantHookInputFromCommand(cmd *cobra.Command) (hookpreflight.Input, error) {
	host, _ := cmd.Flags().GetString("host")
	enabled, _ := cmd.Flags().GetBool("enabled")
	trigger, _ := cmd.Flags().GetString("trigger")
	tool, _ := cmd.Flags().GetString("tool")
	repoPath, _ := cmd.Flags().GetString("repo-path")
	entityID, _ := cmd.Flags().GetString("entity-id")
	service, _ := cmd.Flags().GetString("service")
	workload, _ := cmd.Flags().GetString("workload")
	environment, _ := cmd.Flags().GetString("environment")
	resource, _ := cmd.Flags().GetString("resource")
	freshness, _ := cmd.Flags().GetString("freshness")
	permission, _ := cmd.Flags().GetString("permission")
	budget, _ := cmd.Flags().GetDuration("budget")
	if budget <= 0 {
		return hookpreflight.Input{}, fmt.Errorf("budget must be greater than zero")
	}
	return hookpreflight.Input{
		Host:        host,
		Enabled:     enabled,
		Trigger:     trigger,
		Tool:        tool,
		RepoPath:    repoPath,
		EntityID:    entityID,
		Service:     service,
		Workload:    workload,
		Environment: environment,
		Resource:    resource,
		Freshness:   freshness,
		Permission:  permission,
		Budget:      budget,
	}, nil
}

func readClaudePreToolUseInput(cmd *cobra.Command) (hookpreflight.ClaudePreToolUseInput, bool) {
	reader := cmd.InOrStdin()
	if file, ok := reader.(*os.File); ok {
		info, err := file.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return hookpreflight.ClaudePreToolUseInput{}, false
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return hookpreflight.ClaudePreToolUseInput{}, false
	}
	var payload hookpreflight.ClaudePreToolUseInput
	if err := json.Unmarshal(data, &payload); err != nil {
		return hookpreflight.ClaudePreToolUseInput{}, false
	}
	if payload.HookEventName != "PreToolUse" {
		return hookpreflight.ClaudePreToolUseInput{}, false
	}
	return payload, true
}
