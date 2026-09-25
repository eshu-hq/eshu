// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

// TestAcceptReasonProblem is the seeded RED/GREEN pair for the guard that
// stops a per-row template from justifying a tolerant catalog-sweep row. Each
// RED case is a reason a template could produce; the GREEN cases are real
// rows from the checked-in argument table.
func TestAcceptReasonProblem(t *testing.T) {
	t.Parallel()
	notFound := []string{"ok", "not_found", "scope_not_found", "service_not_found"}
	tests := []struct {
		name    string
		tool    string
		c       catalogSweepCase
		wantErr string // substring of the problem; "" means the reason is acceptable
	}{
		{
			name:    "RED tool-name template",
			tool:    "get_service_story",
			c:       catalogSweepCase{Accept: notFound, Arguments: map[string]any{"workload_id": "sweep-seed-missing"}, AcceptReason: "get_service_story: the fixture is not fully seeded"},
			wantErr: "quotes none",
		},
		{
			name:    "RED placeholder for a seeded subject",
			tool:    "get_repo_summary",
			c:       catalogSweepCase{Accept: []string{"ok", "not_found"}, Arguments: map[string]any{"repo_id": "$REPO"}, AcceptReason: "repo_id $REPO is probably fine"},
			wantErr: "seeded-subject placeholder",
		},
		{
			name:    "RED placeholder beside a real unseeded id",
			tool:    "get_file_content",
			c:       catalogSweepCase{Accept: notFound, Arguments: map[string]any{"repo_id": "$REPO", "relative_path": "README.md"}, AcceptReason: "repo_id $REPO and relative_path README.md are not seeded"},
			wantErr: "seeded-subject placeholder",
		},
		{
			name:    "RED literal seeded id",
			tool:    "get_repo_summary",
			c:       catalogSweepCase{Accept: []string{"ok", "not_found"}, Arguments: map[string]any{"repo_id": "e2e-seed-repo-default"}, AcceptReason: "repo_id e2e-seed-repo-default is not seeded"},
			wantErr: "quotes none",
		},
		{
			name:    "RED capability row names no gate",
			tool:    "find_code_divergence",
			c:       catalogSweepCase{Accept: []string{"ok", "unsupported_capability"}, Arguments: map[string]any{"limit": 5.0}, AcceptReason: "find_code_divergence: the stack is fine"},
			wantErr: "names no capability gate",
		},
		{
			name:    "RED empty reason",
			tool:    "get_service_story",
			c:       catalogSweepCase{Accept: notFound, Arguments: map[string]any{"workload_id": "sweep-seed-missing"}},
			wantErr: "no acceptReason",
		},
		{
			name:    "RED unknown outcome",
			tool:    "get_service_story",
			c:       catalogSweepCase{Accept: []string{"ok", "whatever"}, Arguments: map[string]any{"workload_id": "sweep-seed-missing"}, AcceptReason: "workload_id sweep-seed-missing is not seeded"},
			wantErr: "unknown outcome",
		},
		{
			name: "GREEN real not-found row",
			tool: "get_workload_story",
			c:    catalogSweepCase{Accept: notFound, Arguments: map[string]any{"workload_id": "sweep-seed-missing"}, AcceptReason: "workload_id sweep-seed-missing is not a seeded workload"},
		},
		{
			name: "GREEN real capability row",
			tool: "ask",
			c:    catalogSweepCase{Accept: []string{"ok", "ask_default_off"}, Arguments: map[string]any{"question": "how many repositories are indexed?"}, AcceptReason: "the sweep stack does not set ESHU_ASK_ENABLED, so ask is default-off"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := acceptReasonProblem(tc.tool, tc.c)
			if tc.wantErr == "" {
				if got != "" {
					t.Fatalf("acceptReasonProblem = %q, want acceptable", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantErr) {
				t.Fatalf("acceptReasonProblem = %q, want a problem containing %q", got, tc.wantErr)
			}
		})
	}
}
