// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportProjectsTerraformStateSerials(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: observedAt,
		// Two locators with multiple generations each — the reader is expected
		// to surface only the latest serial per locator. The projection keeps
		// whatever the reader sent, sorted by safe_locator_hash.
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{
			{
				SafeLocatorHash: "hash-z",
				BackendKind:     "s3",
				Lineage:         "lineage-z",
				Serial:          42,
				GenerationID:    "terraform_state:state_snapshot:s3:hash-z:lineage-z:serial:42",
				ObservedAt:      observedAt,
			},
			{
				SafeLocatorHash: "hash-a",
				BackendKind:     "local",
				Lineage:         "lineage-a",
				Serial:          7,
				GenerationID:    "terraform_state:state_snapshot:local:hash-a:lineage-a:serial:7",
				ObservedAt:      observedAt,
			},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if len(report.TerraformState.LastSerials) != 2 {
		t.Fatalf("LastSerials = %d rows, want 2", len(report.TerraformState.LastSerials))
	}
	if got := report.TerraformState.LastSerials[0].SafeLocatorHash; got != "hash-a" {
		t.Fatalf("LastSerials sorted; first hash = %q, want %q", got, "hash-a")
	}
	if got := report.TerraformState.LastSerials[0].Serial; got != 7 {
		t.Fatalf("LastSerials[0].Serial = %d, want %d", got, 7)
	}
	if got := report.TerraformState.LastSerials[1].SafeLocatorHash; got != "hash-z" {
		t.Fatalf("LastSerials[1].SafeLocatorHash = %q, want %q", got, "hash-z")
	}
	if got := report.TerraformState.LastSerials[1].Serial; got != 42 {
		t.Fatalf("LastSerials[1].Serial = %d, want %d", got, 42)
	}
}

func TestBuildReportGroupsRecentWarningsByLocatorAndKind(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: base,
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "state_in_vcs", Reason: "approved_local", Source: "git_local_file", ObservedAt: base},
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "output_value_dropped", Reason: "sensitive_composite_output", Source: "outputs.x", ObservedAt: base.Add(time.Minute)},
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "state_in_vcs", Reason: "approved_local", Source: "git_local_file", ObservedAt: base.Add(2 * time.Minute)},
			{SafeLocatorHash: "hash-2", BackendKind: "local", WarningKind: "state_too_large", Reason: "exceeded ceiling", Source: "graph", ObservedAt: base.Add(3 * time.Minute)},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if got := len(report.TerraformState.RecentWarnings); got != 4 {
		t.Fatalf("RecentWarnings = %d rows, want 4", got)
	}
	hash1, ok := report.TerraformState.WarningsByKind["hash-1"]
	if !ok {
		t.Fatalf("WarningsByKind missing hash-1; got %v", report.TerraformState.WarningsByKind)
	}
	if got := len(hash1["state_in_vcs"]); got != 2 {
		t.Fatalf("hash-1 state_in_vcs = %d rows, want 2", got)
	}
	if got := len(hash1["output_value_dropped"]); got != 1 {
		t.Fatalf("hash-1 output_value_dropped = %d rows, want 1", got)
	}
	hash2, ok := report.TerraformState.WarningsByKind["hash-2"]
	if !ok {
		t.Fatalf("WarningsByKind missing hash-2; got %v", report.TerraformState.WarningsByKind)
	}
	if got := len(hash2["state_too_large"]); got != 1 {
		t.Fatalf("hash-2 state_too_large = %d rows, want 1", got)
	}
}

func TestBuildReportSummarizesTerraformStateWarnings(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: base,
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{
			{SafeLocatorHash: "hash-1", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source", ObservedAt: base},
			{SafeLocatorHash: "hash-2", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source", ObservedAt: base.Add(time.Minute)},
			{SafeLocatorHash: "hash-3", BackendKind: "local", WarningKind: "state_missing", Reason: "path_not_found", Source: "source", ObservedAt: base.Add(2 * time.Minute)},
			{SafeLocatorHash: "hash-4", BackendKind: "", WarningKind: "state_too_large", Reason: "size_limit", Source: "source", ObservedAt: base.Add(3 * time.Minute)},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if got := len(report.TerraformState.WarningSummary); got != 3 {
		t.Fatalf("WarningSummary = %d rows, want 3: %+v", got, report.TerraformState.WarningSummary)
	}
	first := report.TerraformState.WarningSummary[0]
	if first.WarningKind != "state_missing" ||
		first.Reason != "path_not_found" ||
		first.ScopeClass != "local" ||
		first.Severity != "blocking" ||
		first.Actionability != "blocking_evidence" ||
		first.Count != 1 {
		t.Fatalf("WarningSummary[0] = %+v, want local state_missing/path_not_found count=1", first)
	}
	second := report.TerraformState.WarningSummary[1]
	if second.WarningKind != "state_missing" ||
		second.Reason != "s3_not_found" ||
		second.ScopeClass != "s3" ||
		second.Severity != "blocking" ||
		second.Actionability != "blocking_evidence" ||
		second.Count != 2 {
		t.Fatalf("WarningSummary[1] = %+v, want s3 state_missing/s3_not_found count=2", second)
	}
	third := report.TerraformState.WarningSummary[2]
	if third.WarningKind != "state_too_large" ||
		third.Reason != "size_limit" ||
		third.ScopeClass != "unknown" ||
		third.Severity != "blocking" ||
		third.Actionability != "blocking_evidence" ||
		third.Count != 1 {
		t.Fatalf("WarningSummary[2] = %+v, want unknown state_too_large/size_limit count=1", third)
	}
}

func TestBuildReportSummarizesUnresolvedBackendExpressionWarnings(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 13, 15, 30, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: base,
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{
			{
				SafeLocatorHash: "repository:r_12345678:env/backend.tf",
				BackendKind:     "git",
				WarningKind:     "unresolved_backend_expression",
				Reason:          "missing_variable_default",
				Source:          "terraform_backend",
				SourceHandle:    "env/backend.tf",
				ObservedAt:      base,
			},
		},
	}

	report := status.BuildReport(raw, status.DefaultOptions())

	if got := len(report.TerraformState.WarningSummary); got != 1 {
		t.Fatalf("WarningSummary = %d rows, want 1: %+v", got, report.TerraformState.WarningSummary)
	}
	summary := report.TerraformState.WarningSummary[0]
	if summary.WarningKind != "unresolved_backend_expression" ||
		summary.Reason != "missing_variable_default" ||
		summary.ScopeClass != "git" ||
		summary.Severity != "blocking" ||
		summary.Actionability != "blocking_evidence" ||
		summary.Count != 1 {
		t.Fatalf("WarningSummary[0] = %+v, want Git unresolved backend expression count=1", summary)
	}
	recent := report.TerraformState.RecentWarnings[0]
	if recent.Severity != "blocking" || recent.Actionability != "blocking_evidence" {
		t.Fatalf("RecentWarnings[0] classification = %q/%q, want blocking/blocking_evidence", recent.Severity, recent.Actionability)
	}
}

func TestRenderJSONIncludesTerraformStateSection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	raw := status.RawSnapshot{
		AsOf: observedAt,
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{{
			SafeLocatorHash: "abc123",
			BackendKind:     "s3",
			Lineage:         "lineage-1",
			Serial:          5,
			GenerationID:    "terraform_state:state_snapshot:s3:abc123:lineage-1:serial:5",
			ObservedAt:      observedAt,
		}},
		TerraformStateRecentWarnings: []status.TerraformStateLocatorWarning{{
			SafeLocatorHash: "abc123",
			BackendKind:     "s3",
			WarningKind:     "state_in_vcs",
			Reason:          "approved_local",
			Source:          "git_local_file",
			GenerationID:    "terraform_state:state_snapshot:s3:abc123:lineage-1:serial:5",
			ObservedAt:      observedAt,
		}},
	}
	report := status.BuildReport(raw, status.DefaultOptions())
	body, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if !strings.Contains(string(body), `"terraform_state"`) {
		t.Fatalf("RenderJSON() missing terraform_state section: %s", body)
	}

	var decoded struct {
		TerraformState struct {
			LastSerials []struct {
				SafeLocatorHash string `json:"safe_locator_hash"`
				Serial          int64  `json:"serial"`
			} `json:"last_serials"`
			WarningSummary []struct {
				WarningKind   string `json:"warning_kind"`
				Reason        string `json:"reason"`
				ScopeClass    string `json:"scope_class"`
				Severity      string `json:"severity"`
				Actionability string `json:"actionability"`
				Count         int    `json:"count"`
			} `json:"warning_summary"`
			WarningsByKind map[string]map[string][]struct {
				WarningKind   string `json:"warning_kind"`
				Severity      string `json:"severity"`
				Actionability string `json:"actionability"`
			} `json:"warnings_by_kind"`
		} `json:"terraform_state"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v, body=%s", err, body)
	}
	if len(decoded.TerraformState.LastSerials) != 1 ||
		decoded.TerraformState.LastSerials[0].SafeLocatorHash != "abc123" ||
		decoded.TerraformState.LastSerials[0].Serial != 5 {
		t.Fatalf("decoded last_serials = %+v", decoded.TerraformState.LastSerials)
	}
	if len(decoded.TerraformState.WarningsByKind["abc123"]["state_in_vcs"]) != 1 {
		t.Fatalf("decoded warnings_by_kind = %+v", decoded.TerraformState.WarningsByKind)
	}
	if len(decoded.TerraformState.WarningSummary) != 1 ||
		decoded.TerraformState.WarningSummary[0].WarningKind != "state_in_vcs" ||
		decoded.TerraformState.WarningSummary[0].Reason != "approved_local" ||
		decoded.TerraformState.WarningSummary[0].ScopeClass != "s3" ||
		decoded.TerraformState.WarningSummary[0].Severity != "info" ||
		decoded.TerraformState.WarningSummary[0].Actionability != "accepted_guardrail" ||
		decoded.TerraformState.WarningSummary[0].Count != 1 {
		t.Fatalf("decoded warning_summary = %+v", decoded.TerraformState.WarningSummary)
	}
}

func TestRenderJSONIncludesTerraformStateSummaryOnly(t *testing.T) {
	t.Parallel()

	report := status.Report{
		AsOf: time.Date(2026, 5, 3, 11, 0, 0, 0, time.UTC),
		TerraformState: status.TerraformStateReport{
			WarningSummary: []status.TerraformStateWarningSummary{{
				WarningKind:   "state_missing",
				Reason:        "s3_not_found",
				ScopeClass:    "s3",
				Severity:      "blocking",
				Actionability: "blocking_evidence",
				Count:         2,
			}},
		},
	}
	body, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if !strings.Contains(string(body), `"terraform_state"`) {
		t.Fatalf("RenderJSON() missing summary-only terraform_state section: %s", body)
	}
}

func TestRenderJSONOmitsTerraformStateWhenEmpty(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		AsOf: time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC),
	}, status.DefaultOptions())
	body, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if strings.Contains(string(body), `"terraform_state"`) {
		t.Fatalf("RenderJSON() unexpectedly included terraform_state: %s", body)
	}
}

// TestLoadReportSurfacesTerraformStateThroughReader proves the LoadReport path
// passes raw tfstate evidence into the projection unchanged before sorting.
func TestLoadReportSurfacesTerraformStateThroughReader(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)
	reader := &fakeReader{snapshot: status.RawSnapshot{
		AsOf: observedAt,
		TerraformStateLastSerials: []status.TerraformStateLocatorSerial{{
			SafeLocatorHash: "hash-zzz", Serial: 1,
		}, {
			SafeLocatorHash: "hash-aaa", Serial: 99,
		}},
	}}
	report, err := status.LoadReport(context.Background(), reader, observedAt, status.DefaultOptions())
	if err != nil {
		t.Fatalf("LoadReport() error = %v, want nil", err)
	}
	if got := report.TerraformState.LastSerials[0].SafeLocatorHash; got != "hash-aaa" {
		t.Fatalf("LoadReport() LastSerials[0] = %q, want %q", got, "hash-aaa")
	}
}
