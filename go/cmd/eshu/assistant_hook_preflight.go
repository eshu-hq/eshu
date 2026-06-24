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

	"github.com/spf13/cobra"
)

const (
	assistantHookSchema               = "assistant_fast_path_hook.v1"
	assistantHookDefaultBudget        = 200 * time.Millisecond
	assistantHookDefaultLimit         = 5
	assistantHookSupportedHostClaude  = "claude"
	assistantHookFreshnessFresh       = "fresh"
	assistantHookFreshnessStale       = "stale"
	assistantHookFreshnessBuilding    = "building"
	assistantHookFreshnessUnavailable = "unavailable"
	assistantHookPermissionAllowed    = "allowed"
	assistantHookPermissionDenied     = "denied"
)

const (
	assistantHookDecisionAdvise = "advise"
	assistantHookDecisionSkip   = "skip"
)

const (
	assistantHookReasonBoundedPreflight   = "bounded_preflight"
	assistantHookReasonBroadScope         = "broad_scope"
	assistantHookReasonDisabled           = "hook_disabled"
	assistantHookReasonDisallowedTrigger  = "disallowed_trigger"
	assistantHookReasonPermissionDenied   = "eshu_permission_denied"
	assistantHookReasonStaleIndex         = "stale_index"
	assistantHookReasonTimeout            = "eshu_hook_timeout"
	assistantHookReasonUnsupportedHost    = "unsupported_host"
	assistantHookReasonUnavailableContext = "eshu_mcp_unavailable"
)

type assistantHookPreflightInput struct {
	Host        string
	Enabled     bool
	Trigger     string
	Tool        string
	RepoPath    string
	EntityID    string
	Service     string
	Workload    string
	Environment string
	Resource    string
	Freshness   string
	Permission  string
	Budget      time.Duration
	Elapsed     time.Duration
}

type assistantHookPreflightOutput struct {
	Schema      string                    `json:"schema"`
	Decision    string                    `json:"decision"`
	Reason      string                    `json:"reason"`
	Message     string                    `json:"message"`
	Host        string                    `json:"host"`
	Trigger     string                    `json:"trigger"`
	Tool        string                    `json:"tool,omitempty"`
	Scope       *assistantHookScope       `json:"scope,omitempty"`
	PlannedCall *assistantHookPlannedCall `json:"planned_call,omitempty"`
	Truth       assistantHookTruth        `json:"truth"`
	BudgetMS    int64                     `json:"budget_ms"`
	ElapsedMS   int64                     `json:"elapsed_ms"`
}

type assistantHookScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type assistantHookPlannedCall struct {
	Tool      string            `json:"tool"`
	Arguments map[string]string `json:"arguments"`
	Limit     int               `json:"limit"`
	TimeoutMS int64             `json:"timeout_ms"`
}

type assistantHookTruth struct {
	Level          string `json:"level"`
	Profile        string `json:"profile"`
	FreshnessState string `json:"freshness_state"`
	Truncated      bool   `json:"truncated"`
}

type claudePreToolUseInput struct {
	HookEventName string         `json:"hook_event_name"`
	CWD           string         `json:"cwd"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

type claudePreToolUseOutput struct {
	HookSpecificOutput claudePreToolUseSpecificOutput `json:"hookSpecificOutput"`
}

type claudePreToolUseSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
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
	cmd.Flags().String("freshness", assistantHookFreshnessFresh, "Freshness state: fresh, stale, building, or unavailable")
	cmd.Flags().String("permission", assistantHookPermissionAllowed, "Permission state: allowed or denied")
	cmd.Flags().Duration("budget", assistantHookDefaultBudget, "Hook preflight wall-time budget")
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
		mergeClaudePreToolUseInput(&input, hookPayload)
	}
	input.Elapsed = time.Since(start)
	output := evaluateAssistantHookPreflight(input)
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		if output.Decision != assistantHookDecisionAdvise {
			return nil
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(claudePreToolUseOutputForPreflight(output))
	}
	renderAssistantHookPreflightText(cmd.OutOrStdout(), output)
	return nil
}

func assistantHookInputFromCommand(cmd *cobra.Command) (assistantHookPreflightInput, error) {
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
		return assistantHookPreflightInput{}, fmt.Errorf("budget must be greater than zero")
	}
	return assistantHookPreflightInput{
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

func readClaudePreToolUseInput(cmd *cobra.Command) (claudePreToolUseInput, bool) {
	reader := cmd.InOrStdin()
	if file, ok := reader.(*os.File); ok {
		info, err := file.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return claudePreToolUseInput{}, false
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return claudePreToolUseInput{}, false
	}
	var payload claudePreToolUseInput
	if err := json.Unmarshal(data, &payload); err != nil {
		return claudePreToolUseInput{}, false
	}
	if payload.HookEventName != "PreToolUse" {
		return claudePreToolUseInput{}, false
	}
	return payload, true
}

func mergeClaudePreToolUseInput(input *assistantHookPreflightInput, payload claudePreToolUseInput) {
	input.Tool = payload.ToolName
	if input.Trigger == "" {
		input.Trigger = assistantHookTriggerFromClaudeTool(payload.ToolName)
	}
	if input.RepoPath == "" {
		input.RepoPath = assistantHookRepoPathFromClaudeToolInput(payload.CWD, payload.ToolInput)
	}
}

func assistantHookTriggerFromClaudeTool(tool string) string {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "read":
		return "read"
	case "grep":
		return "search"
	case "glob", "ls":
		return "glob"
	case "find_symbol", "symbol":
		return "symbol"
	case "write", "edit", "multiedit", "bash":
		return "edit"
	default:
		return strings.ToLower(strings.TrimSpace(tool))
	}
}

func assistantHookRepoPathFromClaudeToolInput(cwd string, toolInput map[string]any) string {
	for _, key := range []string{"file_path", "path"} {
		value, _ := toolInput[key].(string)
		if rel := assistantHookRepoRelativePath(cwd, value); rel != "" {
			return rel
		}
	}
	return ""
}

func assistantHookRepoRelativePath(cwd, rawPath string) string {
	pathValue := strings.TrimSpace(rawPath)
	if pathValue == "" {
		return ""
	}
	if filepath.IsAbs(pathValue) {
		root := strings.TrimSpace(cwd)
		if root == "" {
			return ""
		}
		rel, err := filepath.Rel(root, pathValue)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return ""
		}
		pathValue = rel
	}
	clean := filepath.ToSlash(filepath.Clean(pathValue))
	if clean == "." {
		return ""
	}
	return clean
}

func evaluateAssistantHookPreflight(input assistantHookPreflightInput) assistantHookPreflightOutput {
	normalized := normalizeAssistantHookInput(input)
	out := assistantHookBaseOutput(normalized)
	switch {
	case normalized.Elapsed > normalized.Budget:
		return assistantHookSkip(out, assistantHookReasonTimeout, "hook budget expired; original host action should continue")
	case normalized.Host != assistantHookSupportedHostClaude:
		return assistantHookSkip(out, assistantHookReasonUnsupportedHost, "host is guidance-only; no hook output")
	case !normalized.Enabled:
		return assistantHookSkip(out, assistantHookReasonDisabled, "hook is not explicitly enabled")
	case !assistantHookTriggerAllowed(normalized.Trigger):
		return assistantHookSkip(out, assistantHookReasonDisallowedTrigger, "trigger class is not eligible for hook output")
	case normalized.Permission == assistantHookPermissionDenied:
		return assistantHookSkip(out, assistantHookReasonPermissionDenied, "permission denied; no scoped context emitted")
	}
	scope, ok := assistantHookScopeFromInput(normalized)
	if !ok {
		return assistantHookSkip(out, assistantHookReasonBroadScope, "provide a narrower repo, file, symbol, service, workload, environment, or resource scope")
	}
	out.Scope = &scope
	if normalized.Freshness == assistantHookFreshnessUnavailable {
		return assistantHookSkip(out, assistantHookReasonUnavailableContext, "Eshu context is unavailable; original host action should continue")
	}
	out.Decision = assistantHookDecisionAdvise
	out.Reason = assistantHookReasonBoundedPreflight
	out.Message = "bounded Eshu preflight available"
	if normalized.Freshness == assistantHookFreshnessStale || normalized.Freshness == assistantHookFreshnessBuilding {
		out.Reason = assistantHookReasonStaleIndex
		out.Message = "bounded Eshu preflight available with degraded freshness"
	}
	out.PlannedCall = assistantHookPlannedCallForScope(scope, normalized.Budget)
	return out
}

func claudePreToolUseOutputForPreflight(out assistantHookPreflightOutput) claudePreToolUseOutput {
	return claudePreToolUseOutput{
		HookSpecificOutput: claudePreToolUseSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: assistantHookAdditionalContext(out),
		},
	}
}

func assistantHookAdditionalContext(out assistantHookPreflightOutput) string {
	if out.PlannedCall == nil || out.Scope == nil {
		return ""
	}
	return fmt.Sprintf(
		"Eshu fast-path advisory: before broad source exploration, prefer %s with %s=%s limit=%d timeout_ms=%d. truth.level=%s truth.profile=%s truth.freshness.state=%s truncated=%v. This is advisory context only; do not treat it as canonical graph truth.",
		out.PlannedCall.Tool,
		out.Scope.Kind,
		out.Scope.ID,
		out.PlannedCall.Limit,
		out.PlannedCall.TimeoutMS,
		out.Truth.Level,
		out.Truth.Profile,
		out.Truth.FreshnessState,
		out.Truth.Truncated,
	)
}

func normalizeAssistantHookInput(input assistantHookPreflightInput) assistantHookPreflightInput {
	input.Host = strings.ToLower(strings.TrimSpace(input.Host))
	input.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))
	input.Tool = strings.TrimSpace(input.Tool)
	input.Freshness = strings.ToLower(strings.TrimSpace(input.Freshness))
	if input.Freshness == "" {
		input.Freshness = assistantHookFreshnessFresh
	}
	input.Permission = strings.ToLower(strings.TrimSpace(input.Permission))
	if input.Permission == "" {
		input.Permission = assistantHookPermissionAllowed
	}
	if input.Budget <= 0 {
		input.Budget = assistantHookDefaultBudget
	}
	input.RepoPath = strings.TrimSpace(input.RepoPath)
	input.EntityID = strings.TrimSpace(input.EntityID)
	input.Service = strings.TrimSpace(input.Service)
	input.Workload = strings.TrimSpace(input.Workload)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Resource = strings.TrimSpace(input.Resource)
	return input
}

func assistantHookBaseOutput(input assistantHookPreflightInput) assistantHookPreflightOutput {
	return assistantHookPreflightOutput{
		Schema:    assistantHookSchema,
		Decision:  assistantHookDecisionSkip,
		Reason:    assistantHookReasonBroadScope,
		Host:      input.Host,
		Trigger:   input.Trigger,
		Tool:      input.Tool,
		BudgetMS:  input.Budget.Milliseconds(),
		ElapsedMS: input.Elapsed.Milliseconds(),
		Truth: assistantHookTruth{
			Level:          "advisory",
			Profile:        "local_preflight",
			FreshnessState: input.Freshness,
			Truncated:      false,
		},
	}
}

func assistantHookSkip(out assistantHookPreflightOutput, reason, message string) assistantHookPreflightOutput {
	out.Decision = assistantHookDecisionSkip
	out.Reason = reason
	out.Message = message
	out.Scope = nil
	out.PlannedCall = nil
	return out
}

func assistantHookTriggerAllowed(trigger string) bool {
	switch trigger {
	case "read", "search", "grep", "glob", "symbol", "prompt":
		return true
	default:
		return false
	}
}

func assistantHookScopeFromInput(input assistantHookPreflightInput) (assistantHookScope, bool) {
	candidates := []assistantHookScope{
		{Kind: "repo_path", ID: input.RepoPath},
		{Kind: "entity_id", ID: input.EntityID},
		{Kind: "service", ID: input.Service},
		{Kind: "workload", ID: input.Workload},
		{Kind: "environment", ID: input.Environment},
		{Kind: "resource", ID: input.Resource},
	}
	for _, scope := range candidates {
		if scope.ID == "" {
			continue
		}
		if assistantHookScopeSafe(scope) {
			return scope, true
		}
		return assistantHookScope{}, false
	}
	return assistantHookScope{}, false
}

func assistantHookScopeSafe(scope assistantHookScope) bool {
	if strings.Contains(scope.ID, "://") || strings.HasPrefix(scope.ID, "/") ||
		strings.HasPrefix(scope.ID, "~") || strings.Contains(scope.ID, "\\") ||
		strings.Contains(scope.ID, "..") || strings.TrimSpace(scope.ID) == "" {
		return false
	}
	for _, r := range scope.ID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-', '/', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func assistantHookPlannedCallForScope(scope assistantHookScope, budget time.Duration) *assistantHookPlannedCall {
	tool := "get_code_relationship_story"
	argKey := scope.Kind
	switch scope.Kind {
	case "service":
		tool = "get_service_story"
	case "workload", "environment":
		tool = "trace_deployment_chain"
	case "resource":
		tool = "investigate_resource"
		argKey = "resource_handle"
	}
	return &assistantHookPlannedCall{
		Tool:      tool,
		Arguments: map[string]string{argKey: scope.ID},
		Limit:     assistantHookDefaultLimit,
		TimeoutMS: budget.Milliseconds(),
	}
}

func renderAssistantHookPreflightText(w io.Writer, out assistantHookPreflightOutput) {
	_, _ = fmt.Fprintf(w, "Assistant hook preflight: %s (%s)\n", out.Decision, out.Reason)
	if out.Message != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", out.Message)
	}
	if out.Scope != nil {
		_, _ = fmt.Fprintf(w, "  scope: %s=%s\n", out.Scope.Kind, out.Scope.ID)
	}
	if out.PlannedCall != nil {
		_, _ = fmt.Fprintf(w, "  next: %s limit=%d timeout_ms=%d\n", out.PlannedCall.Tool, out.PlannedCall.Limit, out.PlannedCall.TimeoutMS)
	}
	_, _ = fmt.Fprintf(w, "  truth: level=%s freshness=%s truncated=%v\n", out.Truth.Level, out.Truth.FreshnessState, out.Truth.Truncated)
}
