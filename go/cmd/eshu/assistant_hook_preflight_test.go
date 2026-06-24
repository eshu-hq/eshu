// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestAssistantHookPreflightUnsupportedHostSkips(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "codex",
		Enabled:  true,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonUnsupportedHost {
		t.Fatalf("decision=%s reason=%s, want skip unsupported_host", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("unsupported host planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightDisabledSkips(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "claude",
		Enabled:  false,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonDisabled {
		t.Fatalf("decision=%s reason=%s, want skip hook_disabled", out.Decision, out.Reason)
	}
}

func TestAssistantHookPreflightDisallowedTriggerSkips(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "edit",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonDisallowedTrigger {
		t.Fatalf("decision=%s reason=%s, want skip disallowed_trigger", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("disallowed trigger planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightMissingScopeSkips(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:    "claude",
		Enabled: true,
		Trigger: "glob",
		Budget:  200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonBroadScope {
		t.Fatalf("decision=%s reason=%s, want skip broad_scope", out.Decision, out.Reason)
	}
	if !strings.Contains(out.Message, "narrower") {
		t.Fatalf("message %q does not ask for narrower scope", out.Message)
	}
}

func TestAssistantHookPreflightPlansBoundedReadForRepoPath(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "read",
		Tool:     "Read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionAdvise || out.Reason != assistantHookReasonBoundedPreflight {
		t.Fatalf("decision=%s reason=%s, want advise bounded_preflight", out.Decision, out.Reason)
	}
	if out.Scope == nil || out.Scope.Kind != "repo_path" || out.Scope.ID != "services/api/handler.go" {
		t.Fatalf("scope = %+v, want repo_path services/api/handler.go", out.Scope)
	}
	if out.PlannedCall == nil {
		t.Fatal("planned call is nil")
	}
	if out.PlannedCall.Tool != "get_code_relationship_story" || out.PlannedCall.Limit != 5 || out.PlannedCall.TimeoutMS != 200 {
		t.Fatalf("planned call = %+v, want bounded code relationship story", out.PlannedCall)
	}
	if out.PlannedCall.Arguments["repo_path"] != "services/api/handler.go" {
		t.Fatalf("planned call arguments = %+v, want repo_path", out.PlannedCall.Arguments)
	}
	if out.Truth.FreshnessState != assistantHookFreshnessFresh {
		t.Fatalf("freshness = %q, want fresh", out.Truth.FreshnessState)
	}
}

func TestAssistantHookPreflightSurfacesStaleFreshness(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:      "claude",
		Enabled:   true,
		Trigger:   "search",
		Service:   "checkout",
		Freshness: assistantHookFreshnessStale,
		Budget:    200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionAdvise || out.Reason != assistantHookReasonStaleIndex {
		t.Fatalf("decision=%s reason=%s, want advise stale_index", out.Decision, out.Reason)
	}
	if out.Truth.FreshnessState != assistantHookFreshnessStale {
		t.Fatalf("freshness = %q, want stale", out.Truth.FreshnessState)
	}
	if out.PlannedCall == nil || out.PlannedCall.Tool != "get_service_story" {
		t.Fatalf("planned call = %+v, want service story", out.PlannedCall)
	}
}

func TestAssistantHookPreflightTimeoutFailsOpen(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
		Elapsed:  201 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonTimeout {
		t.Fatalf("decision=%s reason=%s, want skip eshu_hook_timeout", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("timeout planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightPermissionDeniedFailsOpen(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:       "claude",
		Enabled:    true,
		Trigger:    "read",
		RepoPath:   "services/api/handler.go",
		Permission: assistantHookPermissionDenied,
		Budget:     200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonPermissionDenied {
		t.Fatalf("decision=%s reason=%s, want skip eshu_permission_denied", out.Decision, out.Reason)
	}
	if strings.Contains(out.Message, "services/api/handler.go") {
		t.Fatalf("permission denial leaked scope in message: %q", out.Message)
	}
}

func TestAssistantHookPreflightUnsafeScopeIsRedacted(t *testing.T) {
	privatePath := "/Users/example/private/service.go"
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "read",
		RepoPath: privatePath,
		Budget:   200 * time.Millisecond,
	})

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonBroadScope {
		t.Fatalf("decision=%s reason=%s, want skip broad_scope", out.Decision, out.Reason)
	}
	if strings.Contains(string(data), privatePath) || strings.Contains(string(data), "/Users/example") {
		t.Fatalf("unsafe output leaked private path: %s", data)
	}
}

func TestAssistantHookPreflightNoQueryPathStaysWithinBudget(t *testing.T) {
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = evaluateAssistantHookPreflight(assistantHookPreflightInput{
			Host:    "claude",
			Enabled: true,
			Trigger: "edit",
			Budget:  200 * time.Millisecond,
		})
	}
	elapsed := time.Since(start)
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("1000 no-query preflights took %s, want <200ms", elapsed)
	}
}

func TestAssistantHookPreflightCommandRegistered(t *testing.T) {
	hookCmd := assistantHookCommand()
	if hookCmd == nil {
		t.Fatal("assistant hook command missing")
	}
	var preflight *cobra.Command
	for _, cmd := range hookCmd.Commands() {
		if cmd.Name() == "preflight" {
			preflight = cmd
			break
		}
	}
	if preflight == nil {
		t.Fatal("assistant hook preflight command missing")
	}
	for _, name := range []string{"host", "enabled", "trigger", "repo-path", "service", "json"} {
		if flag := preflight.Flags().Lookup(name); flag == nil {
			t.Fatalf("assistant hook preflight missing --%s flag", name)
		}
	}
}

func TestAssistantHookPreflightCommandReadsClaudePreToolUseJSON(t *testing.T) {
	cmd := newAssistantHookPreflightCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(`{
		"hook_event_name": "PreToolUse",
		"cwd": "/repo",
		"tool_name": "Read",
		"tool_input": {"file_path": "/repo/services/api/handler.go"}
	}`))
	cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	var payload struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			AdditionalContext  string `json:"additionalContext"`
			PermissionDecision string `json:"permissionDecision"`
			UpdatedInput       any    `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not Claude hook JSON: %v\n%s", err, out.String())
	}
	if payload.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("hook event = %q, want PreToolUse", payload.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(payload.HookSpecificOutput.AdditionalContext, "get_code_relationship_story") ||
		!strings.Contains(payload.HookSpecificOutput.AdditionalContext, "services/api/handler.go") ||
		!strings.Contains(payload.HookSpecificOutput.AdditionalContext, "truth.level=advisory") {
		t.Fatalf("additional context missing bounded Eshu advisory:\n%s", payload.HookSpecificOutput.AdditionalContext)
	}
	if payload.HookSpecificOutput.PermissionDecision != "" || payload.HookSpecificOutput.UpdatedInput != nil {
		t.Fatalf("hook output must not set permission decision or updated input: %+v", payload.HookSpecificOutput)
	}
}

func TestAssistantHookPreflightCommandSkipsWithNoStdout(t *testing.T) {
	cmd := newAssistantHookPreflightCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(`{
		"hook_event_name": "PreToolUse",
		"cwd": "/repo",
		"tool_name": "Write",
		"tool_input": {"file_path": "/repo/services/api/handler.go", "content": "new"}
	}`))
	cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("skip should emit no stdout for hook fail-open, got:\n%s", out.String())
	}
}

func TestAssistantHookPreflightCommandMalformedJSONFailsOpen(t *testing.T) {
	cmd := newAssistantHookPreflightCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetIn(strings.NewReader(`not json`))
	cmd.SetArgs([]string{"--host", "claude", "--enabled", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("malformed hook input should emit no stdout, got:\n%s", out.String())
	}
}

func TestAssistantHookPreflightUnavailableContextSkips(t *testing.T) {
	out := evaluateAssistantHookPreflight(assistantHookPreflightInput{
		Host:      "claude",
		Enabled:   true,
		Trigger:   "read",
		RepoPath:  "services/api/handler.go",
		Freshness: assistantHookFreshnessUnavailable,
		Budget:    200 * time.Millisecond,
	})

	if out.Decision != assistantHookDecisionSkip || out.Reason != assistantHookReasonUnavailableContext {
		t.Fatalf("decision=%s reason=%s, want skip eshu_mcp_unavailable", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("unavailable context planned call: %+v", out.PlannedCall)
	}
}
