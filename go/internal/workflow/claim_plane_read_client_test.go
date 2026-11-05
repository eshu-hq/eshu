// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// readReportStub serves the five read/report-plane routes with
// satellite-shaped bodies so the compat tests prove the client decodes
// those bodies into the in-tree types. The bodies are hand-written, so they
// do not prove the live wire shapes: that proof lives only in
// TestReadReportPlaneLiveCompat, which runs against the bound satellite
// build when the satellite leg is configured and skips otherwise.
// It is distinct from versionRecordingPlane: that pin proves every route
// carries major "1"; this stub proves the five uncalled routes decode into
// the in-tree types.
type readReportStub struct {
	t      testing.TB
	seen   []string
	status int
	body   map[string]any
}

func (s *readReportStub) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(writer http.ResponseWriter, route string, value any) {
		s.t.Helper()
		s.seen = append(s.seen, route)
		writer.Header().Set("Content-Type", "application/json")
		if s.status != 0 {
			writer.WriteHeader(s.status)
		}
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			s.t.Fatalf("encode stub response: %v", err)
		}
	}
	now := time.Now().UTC()
	run := map[string]any{
		"run_id": "run-1", "trigger_kind": "schedule",
		"status":     "collection_active",
		"created_at": now, "updated_at": now,
	}
	progress := []map[string]any{{
		"collector_kind":   "jira",
		"total_work_items": 4, "pending_work_items": 1,
		"claimed_work_items": 1, "completed_work_items": 2,
		"failed_terminal_items": 0, "factless_completed_items": 0,
		"published_phase_counts":      map[string]int{"graph/canonical": 2},
		"terminal_dead_letter_counts": map[string]int{},
	}}
	states := []map[string]any{{
		"run_id": "run-1", "collector_kind": "jira",
		"keyspace": "graph", "phase_name": "canonical",
		"required": true, "status": "pending",
		"observed_at": now, "updated_at": now,
	}}
	mux.HandleFunc("/workflow.v1/GetRunStatus", func(writer http.ResponseWriter, _ *http.Request) {
		if s.body != nil {
			write(writer, "GetRunStatus", s.body)
			return
		}
		write(writer, "GetRunStatus", map[string]any{
			"run": run, "progress": progress,
			"completeness": states, "as_of": now,
		})
	})
	mux.HandleFunc("/workflow.v1/ListRuns", func(writer http.ResponseWriter, _ *http.Request) {
		if s.body != nil {
			write(writer, "ListRuns", s.body)
			return
		}
		write(writer, "ListRuns", map[string]any{
			"runs": []map[string]any{run}, "as_of": now,
		})
	})
	mux.HandleFunc("/workflow.v1/GetRunCompleteness", func(writer http.ResponseWriter, _ *http.Request) {
		if s.body != nil {
			write(writer, "GetRunCompleteness", s.body)
			return
		}
		write(writer, "GetRunCompleteness", map[string]any{
			"run_id": "run-1", "states": states, "as_of": now,
		})
	})
	mux.HandleFunc("/workflow.v1/QueueDepthSnapshot", func(writer http.ResponseWriter, _ *http.Request) {
		if s.body != nil {
			write(writer, "QueueDepthSnapshot", s.body)
			return
		}
		write(writer, "QueueDepthSnapshot", map[string]any{
			"families": []map[string]any{
				{"collector_kind": "jira", "pending": 1, "claimed": 1},
			},
			"global_pending": 1, "global_claimed": 1, "as_of": now,
		})
	})
	mux.HandleFunc("/workflow.v1/ReportRunProgress", func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, "ReportRunProgress", map[string]any{"ok": true})
	})
	return mux
}

func testReportProgress() []CollectorRunProgress {
	return []CollectorRunProgress{{
		CollectorKind:       scope.CollectorKind("jira"),
		TotalWorkItems:      4,
		PendingWorkItems:    1,
		ClaimedWorkItems:    1,
		CompletedWorkItems:  2,
		FailedTerminalItems: 0,
		PublishedPhaseCounts: map[PhasePublicationKey]int{
			{Keyspace: "graph", PhaseName: "canonical"}: 2,
		},
		TerminalDeadLetterCounts: map[PhasePublicationKey]int{},
	}}
}

func TestClaimPlaneClientGetRunStatus(t *testing.T) {
	stub := &readReportStub{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	report, err := client.GetRunStatus(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get run status: %v", err)
	}
	if report.Run.RunID != "run-1" || report.Run.Status != RunStatusCollectionActive {
		t.Fatalf("run = %+v, want run-1/collection_active", report.Run)
	}
	if len(report.Progress) != 1 || report.Progress[0].CompletedWorkItems != 2 {
		t.Fatalf("progress = %+v, want one family with 2 completed", report.Progress)
	}
	if got := report.Progress[0].PublishedPhaseCounts[PhasePublicationKey{Keyspace: "graph", PhaseName: "canonical"}]; got != 2 {
		t.Fatalf("published phase count = %d, want 2", got)
	}
	if len(report.Completeness) != 1 || report.Completeness[0].PhaseName != "canonical" {
		t.Fatalf("completeness = %+v, want one canonical checkpoint", report.Completeness)
	}
	if report.AsOf.IsZero() {
		t.Fatal("as_of is zero")
	}
}

func TestClaimPlaneClientListRuns(t *testing.T) {
	stub := &readReportStub{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	page, err := client.ListRuns(context.Background(), RunStatusCollectionActive, 10, "")
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(page.Runs) != 1 || page.Runs[0].RunID != "run-1" {
		t.Fatalf("runs = %+v, want one run-1", page.Runs)
	}
	if page.AsOf.IsZero() {
		t.Fatal("as_of is zero")
	}
}

func TestClaimPlaneClientGetRunCompleteness(t *testing.T) {
	stub := &readReportStub{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	got, err := client.GetRunCompleteness(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get run completeness: %v", err)
	}
	if got.RunID != "run-1" || len(got.States) != 1 {
		t.Fatalf("completeness = %+v, want run-1 with one state", got)
	}
	if got.AsOf.IsZero() {
		t.Fatal("as_of is zero")
	}
}

func TestClaimPlaneClientQueueDepthSnapshot(t *testing.T) {
	stub := &readReportStub{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	snapshot, err := client.QueueDepthSnapshot(context.Background())
	if err != nil {
		t.Fatalf("queue depth snapshot: %v", err)
	}
	if snapshot.GlobalPending != 1 || snapshot.GlobalClaimed != 1 {
		t.Fatalf("snapshot = %+v, want 1 pending 1 claimed", snapshot)
	}
	if len(snapshot.Families) != 1 || string(snapshot.Families[0].CollectorKind) != "jira" {
		t.Fatalf("families = %+v, want one jira family", snapshot.Families)
	}
	if snapshot.AsOf.IsZero() {
		t.Fatal("as_of is zero")
	}
}

func TestClaimPlaneClientReportRunProgress(t *testing.T) {
	stub := &readReportStub{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	if err := client.ReportRunProgress(context.Background(), "run-1", time.Now().UTC(), testReportProgress()); err != nil {
		t.Fatalf("report run progress: %v", err)
	}
	if len(stub.seen) != 1 || stub.seen[0] != "ReportRunProgress" {
		t.Fatalf("seen = %v, want one ReportRunProgress", stub.seen)
	}
}

func TestClaimPlaneClientReadRoutesMapNotFound(t *testing.T) {
	stub := &readReportStub{
		t:      t,
		status: http.StatusNotFound,
		body:   map[string]any{"code": "not_found", "message": "run is gone"},
	}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	_, err := client.GetRunStatus(context.Background(), "run-1")
	if err == nil {
		t.Fatal("get run status succeeded, want not-found error")
	}
	if errors.Is(err, ErrClaimRejected) {
		t.Fatalf("not-found err = %v, must not read as a fence rejection", err)
	}
}
