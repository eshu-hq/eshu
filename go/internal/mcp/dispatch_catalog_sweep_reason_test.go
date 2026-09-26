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

// TestCatalogSweepLedgerLabelProblem is the seeded RED/GREEN pair for the guard
// that stops a case labelled as a ledger row from outliving its ledger. The RED
// case is the state #7183 left behind: a _pending_ledger label whose route now
// derives as allowlisted.
func TestCatalogSweepLedgerLabelProblem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		label   string
		class   string
		wantErr bool
	}{
		{"RED ledger label on a promoted allowlisted route", "who_modifies_pending_ledger", catalogSweepClassAllowlisted, true},
		{"GREEN ledger label on a pending-row-filtering route", "trace_pending_ledger", catalogSweepClassPendingFiltered, false},
		{"GREEN ledger label on a shared-key-only route", "trace_pending_ledger", catalogSweepClassSharedKeyOnly, false},
		{"GREEN unlabelled case on an allowlisted route", "who_modifies", catalogSweepClassAllowlisted, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalogSweepLedgerLabelProblem(tt.label, tt.class)
			if (got != "") != tt.wantErr {
				t.Fatalf("catalogSweepLedgerLabelProblem(%q, %q) = %q, wantErr %v", tt.label, tt.class, got, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(got, "stale") {
				t.Fatalf("problem %q does not say the label is stale", got)
			}
		})
	}
}

// TestCatalogSweepClassProblem is the seeded RED/GREEN pair for the guard that
// makes a route promotion fail the Go suite. The RED cases are the two drifts
// #7193 and #7191 caused: a case written for a ledger class whose route now
// derives as allowlisted, and a case with no checked-in expectation.
func TestCatalogSweepClassProblem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		expected    string
		hasExpected bool
		derived     string
		wantContain string
	}{
		{"RED promoted off the pending ledger", catalogSweepClassPendingFiltered, true, catalogSweepClassAllowlisted, "now derives as allowlisted"},
		{"RED demoted onto the pending ledger", catalogSweepClassAllowlisted, true, catalogSweepClassPendingFiltered, "now derives as pending_row_filtering"},
		{"RED no checked-in expectation", "", false, catalogSweepClassAllowlisted, "has no entry"},
		{"GREEN allowlisted stays allowlisted", catalogSweepClassAllowlisted, true, catalogSweepClassAllowlisted, ""},
		{"GREEN shared-key-only stays shared-key-only", catalogSweepClassSharedKeyOnly, true, catalogSweepClassSharedKeyOnly, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalogSweepClassProblem(tt.expected, tt.hasExpected, tt.derived)
			if tt.wantContain == "" {
				if got != "" {
					t.Fatalf("catalogSweepClassProblem(%q, %v, %q) = %q, want no problem", tt.expected, tt.hasExpected, tt.derived, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Fatalf("catalogSweepClassProblem(%q, %v, %q) = %q, want it to contain %q", tt.expected, tt.hasExpected, tt.derived, got, tt.wantContain)
			}
		})
	}
}
