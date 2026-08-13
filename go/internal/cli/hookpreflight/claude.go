// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ClaudePreToolUseInput is the subset of Claude Code's PreToolUse hook
// payload go/cmd/eshu's wrapper reads from stdin. HookEventName must equal
// "PreToolUse" for the wrapper to treat the payload as usable; anything
// else (or malformed JSON) is a fail-open no-op, decided entirely by the
// wrapper before it ever calls MergeClaudePreToolUseInput.
type ClaudePreToolUseInput struct {
	HookEventName string         `json:"hook_event_name"`
	CWD           string         `json:"cwd"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

// ClaudePreToolUseOutput is the Claude Code PreToolUse hook JSON shape the
// CLI emits with --json. It carries no permissionDecision or updatedInput:
// this hook is advisory-only and never blocks or rewrites the tool call.
type ClaudePreToolUseOutput struct {
	HookSpecificOutput ClaudePreToolUseSpecificOutput `json:"hookSpecificOutput"`
}

// ClaudePreToolUseSpecificOutput is the hookSpecificOutput body of
// ClaudePreToolUseOutput.
type ClaudePreToolUseSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// MergeClaudePreToolUseInput folds a decoded Claude PreToolUse payload into
// input: it always sets Tool from payload.ToolName, and fills Trigger and
// RepoPath from the payload only when the caller (the CLI's flags) left
// them unset, so an explicit --trigger or --repo-path always wins.
func MergeClaudePreToolUseInput(input *Input, payload ClaudePreToolUseInput) {
	input.Tool = payload.ToolName
	if input.Trigger == "" {
		input.Trigger = triggerFromClaudeTool(payload.ToolName)
	}
	if input.RepoPath == "" {
		input.RepoPath = repoPathFromClaudeToolInput(payload.CWD, payload.ToolInput)
	}
}

func triggerFromClaudeTool(tool string) string {
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

func repoPathFromClaudeToolInput(cwd string, toolInput map[string]any) string {
	for _, key := range []string{"file_path", "path"} {
		value, _ := toolInput[key].(string)
		if rel := repoRelativePath(cwd, value); rel != "" {
			return rel
		}
	}
	return ""
}

func repoRelativePath(cwd, rawPath string) string {
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

// ClaudePreToolUseOutputForPreflight converts an Evaluate result into the
// Claude PreToolUse hook JSON shape. Call it only for a DecisionAdvise
// Output; a skip Output has no Scope/PlannedCall, so its AdditionalContext
// comes back empty.
func ClaudePreToolUseOutputForPreflight(out Output) ClaudePreToolUseOutput {
	return ClaudePreToolUseOutput{
		HookSpecificOutput: ClaudePreToolUseSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: additionalContext(out),
		},
	}
}

func additionalContext(out Output) string {
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
