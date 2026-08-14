// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAssistantHookPreflightUnsupportedHostSkips(t *testing.T) {
	out := Evaluate(Input{
		Host:     "codex",
		Enabled:  true,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonUnsupportedHost {
		t.Fatalf("decision=%s reason=%s, want skip unsupported_host", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("unsupported host planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightDisabledSkips(t *testing.T) {
	out := Evaluate(Input{
		Host:     "claude",
		Enabled:  false,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonDisabled {
		t.Fatalf("decision=%s reason=%s, want skip hook_disabled", out.Decision, out.Reason)
	}
}

func TestAssistantHookPreflightDisallowedTriggerSkips(t *testing.T) {
	out := Evaluate(Input{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "edit",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonDisallowedTrigger {
		t.Fatalf("decision=%s reason=%s, want skip disallowed_trigger", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("disallowed trigger planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightMissingScopeSkips(t *testing.T) {
	out := Evaluate(Input{
		Host:    "claude",
		Enabled: true,
		Trigger: "glob",
		Budget:  200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonBroadScope {
		t.Fatalf("decision=%s reason=%s, want skip broad_scope", out.Decision, out.Reason)
	}
	if !strings.Contains(out.Message, "narrower") {
		t.Fatalf("message %q does not ask for narrower scope", out.Message)
	}
}

func TestAssistantHookPreflightPlansBoundedReadForRepoPath(t *testing.T) {
	out := Evaluate(Input{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "read",
		Tool:     "Read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
	})

	if out.Decision != DecisionAdvise || out.Reason != reasonBoundedPreflight {
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
	if out.Truth.FreshnessState != FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh", out.Truth.FreshnessState)
	}
}

func TestAssistantHookPreflightSurfacesStaleFreshness(t *testing.T) {
	out := Evaluate(Input{
		Host:      "claude",
		Enabled:   true,
		Trigger:   "search",
		Service:   "checkout",
		Freshness: freshnessStale,
		Budget:    200 * time.Millisecond,
	})

	if out.Decision != DecisionAdvise || out.Reason != reasonStaleIndex {
		t.Fatalf("decision=%s reason=%s, want advise stale_index", out.Decision, out.Reason)
	}
	if out.Truth.FreshnessState != freshnessStale {
		t.Fatalf("freshness = %q, want stale", out.Truth.FreshnessState)
	}
	if out.PlannedCall == nil || out.PlannedCall.Tool != "get_service_story" {
		t.Fatalf("planned call = %+v, want service story", out.PlannedCall)
	}
}

func TestAssistantHookPreflightTimeoutFailsOpen(t *testing.T) {
	out := Evaluate(Input{
		Host:     "claude",
		Enabled:  true,
		Trigger:  "read",
		RepoPath: "services/api/handler.go",
		Budget:   200 * time.Millisecond,
		Elapsed:  201 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonTimeout {
		t.Fatalf("decision=%s reason=%s, want skip eshu_hook_timeout", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("timeout planned call: %+v", out.PlannedCall)
	}
}

func TestAssistantHookPreflightPermissionDeniedFailsOpen(t *testing.T) {
	out := Evaluate(Input{
		Host:       "claude",
		Enabled:    true,
		Trigger:    "read",
		RepoPath:   "services/api/handler.go",
		Permission: permissionDenied,
		Budget:     200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonPermissionDenied {
		t.Fatalf("decision=%s reason=%s, want skip eshu_permission_denied", out.Decision, out.Reason)
	}
	if strings.Contains(out.Message, "services/api/handler.go") {
		t.Fatalf("permission denial leaked scope in message: %q", out.Message)
	}
}

func TestAssistantHookPreflightUnsafeScopeIsRedacted(t *testing.T) {
	privatePath := "/Users/example/private/service.go"
	out := Evaluate(Input{
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
	if out.Decision != decisionSkip || out.Reason != reasonBroadScope {
		t.Fatalf("decision=%s reason=%s, want skip broad_scope", out.Decision, out.Reason)
	}
	if strings.Contains(string(data), privatePath) || strings.Contains(string(data), "/Users/example") {
		t.Fatalf("unsafe output leaked private path: %s", data)
	}
}

func TestAssistantHookPreflightNoQueryPathStaysWithinBudget(t *testing.T) {
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = Evaluate(Input{
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

func TestAssistantHookPreflightUnavailableContextSkips(t *testing.T) {
	out := Evaluate(Input{
		Host:      "claude",
		Enabled:   true,
		Trigger:   "read",
		RepoPath:  "services/api/handler.go",
		Freshness: freshnessUnavailable,
		Budget:    200 * time.Millisecond,
	})

	if out.Decision != decisionSkip || out.Reason != reasonUnavailableContext {
		t.Fatalf("decision=%s reason=%s, want skip eshu_mcp_unavailable", out.Decision, out.Reason)
	}
	if out.PlannedCall != nil {
		t.Fatalf("unavailable context planned call: %+v", out.PlannedCall)
	}
}
