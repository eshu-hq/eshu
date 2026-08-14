// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/hookpreflight"
)

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
