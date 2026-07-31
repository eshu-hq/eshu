// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

// TestComputeLocalBackendScopeIDMatchesScopeLocatorHash pins the
// gate-local hash formula against the REAL production formula
// (terraformstate.ScopeLocatorHash), imported here only for the test (the
// production gate binary does not depend on go/internal/collector — see
// computeLocalBackendScopeID's doc comment). If either formula changes in a
// way that breaks the join, this test catches it locally, without needing a
// live gate run.
func TestComputeLocalBackendScopeIDMatchesScopeLocatorHash(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/terraform_local_backend_demo")
	wantLocator := filepath.Join(repoLocalPath, "terraform.tfstate")
	wantHash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, wantLocator)
	wantScopeID := "state_snapshot:local:" + wantHash

	got, err := computeLocalBackendScopeID(repoLocalPath)
	if err != nil {
		t.Fatalf("computeLocalBackendScopeID() error = %v, want nil", err)
	}
	if got != wantScopeID {
		t.Fatalf("computeLocalBackendScopeID(%q) = %q, want %q", repoLocalPath, got, wantScopeID)
	}
}

// TestComputeLocalBackendScopeIDRejectsBlankPath proves the function fails
// loudly rather than silently hashing an empty/garbage locator when the
// orchestrator's Postgres lookup for the fixture's local_path comes back
// empty (e.g. bootstrap-index has not yet committed the repository fact).
func TestComputeLocalBackendScopeIDRejectsBlankPath(t *testing.T) {
	t.Parallel()

	if _, err := computeLocalBackendScopeID(""); err == nil {
		t.Fatal("computeLocalBackendScopeID(\"\") error = nil, want non-nil")
	}
}

// TestSubstituteLocalBackendScopeIDReplacesSentinel proves the sentinel is
// replaced across RequestBody (HTTP), Arguments (MCP), and
// RequiredJSONValues, and that non-sentinel values are left untouched.
func TestSubstituteLocalBackendScopeIDReplacesSentinel(t *testing.T) {
	t.Parallel()

	snap := Snapshot{
		QueryShapes: QueryShapes{
			HTTP: map[string]QueryShape{
				"POST /api/v0/terraform/config-state-drift/findings": {
					RequestBody: map[string]any{
						"scope_id": localBackendScopeIDSentinel,
						"limit":    25,
					},
					RequiredJSONValues: map[string]any{
						"data.scope_id": localBackendScopeIDSentinel,
						"data.outcome":  "exact",
					},
				},
			},
			MCP: map[string]QueryShape{
				"list_terraform_config_state_drift_findings": {
					Arguments: map[string]any{
						"scope_id": localBackendScopeIDSentinel,
						"limit":    25,
					},
				},
				"other_tool_unaffected": {
					Arguments: map[string]any{
						"scope_id": "state_snapshot:s3:unrelated-hash",
					},
				},
			},
		},
	}

	const wantScopeID = "state_snapshot:local:deadbeef"
	substituteLocalBackendScopeID(&snap, wantScopeID)

	httpShape := snap.QueryShapes.HTTP["POST /api/v0/terraform/config-state-drift/findings"]
	if got := httpShape.RequestBody["scope_id"]; got != wantScopeID {
		t.Fatalf("HTTP RequestBody[scope_id] = %#v, want %q", got, wantScopeID)
	}
	if got := httpShape.RequestBody["limit"]; got != 25 {
		t.Fatalf("HTTP RequestBody[limit] = %#v, want unchanged 25", got)
	}
	if got := httpShape.RequiredJSONValues["data.scope_id"]; got != wantScopeID {
		t.Fatalf("HTTP RequiredJSONValues[data.scope_id] = %#v, want %q", got, wantScopeID)
	}
	if got := httpShape.RequiredJSONValues["data.outcome"]; got != "exact" {
		t.Fatalf("HTTP RequiredJSONValues[data.outcome] = %#v, want unchanged \"exact\"", got)
	}

	mcpShape := snap.QueryShapes.MCP["list_terraform_config_state_drift_findings"]
	if got := mcpShape.Arguments["scope_id"]; got != wantScopeID {
		t.Fatalf("MCP Arguments[scope_id] = %#v, want %q", got, wantScopeID)
	}

	otherShape := snap.QueryShapes.MCP["other_tool_unaffected"]
	if got := otherShape.Arguments["scope_id"]; got != "state_snapshot:s3:unrelated-hash" {
		t.Fatalf("unrelated MCP shape's scope_id = %#v, want untouched", got)
	}
}

// TestSubstituteLocalBackendScopeIDNoOpWhenEmpty proves the substitution is a
// true no-op when scopeID is empty, so every existing invocation that does
// not pass -local-backend-scope-id is byte-for-byte unaffected.
func TestSubstituteLocalBackendScopeIDNoOpWhenEmpty(t *testing.T) {
	t.Parallel()

	snap := Snapshot{
		QueryShapes: QueryShapes{
			HTTP: map[string]QueryShape{
				"POST /api/v0/terraform/config-state-drift/findings": {
					RequestBody: map[string]any{"scope_id": localBackendScopeIDSentinel},
				},
			},
		},
	}
	substituteLocalBackendScopeID(&snap, "")

	got := snap.QueryShapes.HTTP["POST /api/v0/terraform/config-state-drift/findings"].RequestBody["scope_id"]
	if got != localBackendScopeIDSentinel {
		t.Fatalf("RequestBody[scope_id] = %#v, want sentinel left untouched when scopeID is empty", got)
	}
}
