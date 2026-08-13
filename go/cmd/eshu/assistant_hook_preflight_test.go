// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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
