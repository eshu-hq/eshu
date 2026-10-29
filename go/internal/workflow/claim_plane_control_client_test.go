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

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// stubControlPlane serves the six workflow.v1 control routes
// (CreateRun, EnqueueWorkItems, ReconcileCollectorInstances,
// UpsertCompletenessStates, ReconcileWorkflowRuns, ReapExpiredClaims)
// with protocol-accurate bodies. It records every request body by
// operation so tests can assert the translation field-for-field.
type stubControlPlane struct {
	t        testing.TB
	requests map[string][]map[string]any
	invalid  string
}

func (s *stubControlPlane) write(writer http.ResponseWriter, status int, value any) {
	s.t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		s.t.Fatalf("encode stub response: %v", err)
	}
}

func (s *stubControlPlane) record(operation string, request *http.Request) {
	s.t.Helper()
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		s.t.Fatalf("decode %s request: %v", operation, err)
	}
	if s.requests == nil {
		s.requests = map[string][]map[string]any{}
	}
	s.requests[operation] = append(s.requests[operation], body)
}

func (s *stubControlPlane) handler() http.Handler {
	mux := http.NewServeMux()
	envelope := func(code, message string) map[string]any {
		return map[string]any{"code": code, "message": message}
	}
	mux.HandleFunc("/workflow.v1/CreateRun", func(writer http.ResponseWriter, request *http.Request) {
		if s.invalid == "CreateRun" {
			s.write(writer, http.StatusBadRequest, envelope("invalid", "run_id must not be blank"))
			return
		}
		s.record("CreateRun", request)
		run := s.requests["CreateRun"][len(s.requests["CreateRun"])-1]["run"].(map[string]any)
		s.write(writer, http.StatusOK, map[string]any{"run_id": run["run_id"]})
	})
	mux.HandleFunc("/workflow.v1/EnqueueWorkItems", func(writer http.ResponseWriter, request *http.Request) {
		s.record("EnqueueWorkItems", request)
		items := s.requests["EnqueueWorkItems"][len(s.requests["EnqueueWorkItems"])-1]["items"].([]any)
		s.write(writer, http.StatusOK, map[string]any{
			"accepted": len(items), "inserted_work_items": len(items),
		})
	})
	ack := func(operation string) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			s.record(operation, request)
			s.write(writer, http.StatusOK, map[string]any{"ok": true})
		}
	}
	mux.HandleFunc("/workflow.v1/ReconcileCollectorInstances", ack("ReconcileCollectorInstances"))
	mux.HandleFunc("/workflow.v1/UpsertCompletenessStates", ack("UpsertCompletenessStates"))
	mux.HandleFunc("/workflow.v1/ReconcileWorkflowRuns", func(writer http.ResponseWriter, request *http.Request) {
		s.record("ReconcileWorkflowRuns", request)
		s.write(writer, http.StatusOK, map[string]any{"transitioned": 2})
	})
	mux.HandleFunc("/workflow.v1/ReapExpiredClaims", func(writer http.ResponseWriter, request *http.Request) {
		if s.invalid == "ReapExpiredClaims" {
			s.write(writer, http.StatusConflict, envelope("stale_fence", "epoch superseded"))
			return
		}
		s.record("ReapExpiredClaims", request)
		now := time.Now().UTC()
		s.write(writer, http.StatusOK, map[string]any{
			"claims": []any{map[string]any{
				"claim_id": "claim-9", "work_item_id": "item-9",
				"fencing_token": 9, "owner_id": "owner-9", "status": "expired",
				"claimed_at": now.Add(-time.Hour), "heartbeat_at": now.Add(-time.Hour),
				"lease_expires_at": now.Add(-time.Minute),
				"created_at":       now.Add(-time.Hour), "updated_at": now,
			}},
		})
	})
	return mux
}

func testControlRun() Run {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return Run{
		RunID: "run-1", TriggerKind: TriggerKindSchedule,
		Status: RunStatusCollectionActive, CreatedAt: now, UpdatedAt: now,
	}
}

func testControlWorkItem() WorkItem {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return WorkItem{
		WorkItemID: "item-1", RunID: "run-1",
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		SourceSystem:        "jira", ScopeID: "scope-1",
		AcceptanceUnitID: "au-1", SourceRunID: "sr-1", GenerationID: "gen-1",
		Status: WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
	}
}

func testControlInstance() DesiredCollectorInstance {
	return DesiredCollectorInstance{
		InstanceID: "jira-primary", CollectorKind: scope.CollectorKind("jira"),
		Mode: CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration: "{}",
	}
}

func testControlCompleteness() CompletenessState {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return CompletenessState{
		RunID: "run-1", CollectorKind: scope.CollectorKind("jira"),
		Keyspace: gpphase.Keyspace("facts"), PhaseName: "collect",
		Status: "complete", ObservedAt: now, UpdatedAt: now,
	}
}

func TestClaimPlaneClientControlPlane(t *testing.T) {
	stub := &stubControlPlane{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()
	now := time.Now().UTC()

	if err := client.CreateRun(ctx, testControlRun()); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	got := stub.requests["CreateRun"][0]["run"].(map[string]any)
	if got["run_id"] != "run-1" || got["trigger_kind"] != "schedule" || got["status"] != "collection_active" {
		t.Fatalf("CreateRun run = %v", got)
	}

	if err := client.EnqueueWorkItems(ctx, []WorkItem{testControlWorkItem()}); err != nil {
		t.Fatalf("EnqueueWorkItems: %v", err)
	}
	items := stub.requests["EnqueueWorkItems"][0]["items"].([]any)
	first := items[0].(map[string]any)
	if first["work_item_id"] != "item-1" || first["generation_id"] != "gen-1" {
		t.Fatalf("EnqueueWorkItems item = %v", first)
	}

	before := len(stub.requests["EnqueueWorkItems"])
	if err := client.EnqueueWorkItems(ctx, nil); err != nil {
		t.Fatalf("EnqueueWorkItems empty: %v", err)
	}
	if len(stub.requests["EnqueueWorkItems"]) != before {
		t.Fatalf("empty enqueue hit the server")
	}

	if err := client.ReconcileCollectorInstances(ctx, now, []DesiredCollectorInstance{testControlInstance()}); err != nil {
		t.Fatalf("ReconcileCollectorInstances: %v", err)
	}

	if err := client.UpsertCompletenessStates(ctx, []CompletenessState{testControlCompleteness()}); err != nil {
		t.Fatalf("UpsertCompletenessStates: %v", err)
	}
	beforeAck := len(stub.requests["UpsertCompletenessStates"])
	if err := client.UpsertCompletenessStates(ctx, nil); err != nil {
		t.Fatalf("UpsertCompletenessStates empty: %v", err)
	}
	if len(stub.requests["UpsertCompletenessStates"]) != beforeAck {
		t.Fatalf("empty upsert hit the server")
	}

	transitioned, err := client.ReconcileWorkflowRuns(ctx, now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns: %v", err)
	}
	if transitioned != 2 {
		t.Fatalf("transitioned = %d, want 2", transitioned)
	}

	claims, err := client.ReapExpiredClaims(ctx, now, 10, time.Second)
	if err != nil {
		t.Fatalf("ReapExpiredClaims: %v", err)
	}
	if len(claims) != 1 || claims[0].ClaimID != "claim-9" || claims[0].FencingToken != 9 {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestClaimPlaneClientControlPlaneErrors(t *testing.T) {
	stub := &stubControlPlane{t: t, invalid: "CreateRun"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()

	if err := client.CreateRun(ctx, Run{}); err == nil {
		t.Fatalf("CreateRun invalid: want error")
	} else if errors.Is(err, ErrClaimRejected) {
		t.Fatalf("CreateRun invalid err = %v, must not map to ErrClaimRejected", err)
	}

	stub.invalid = "ReapExpiredClaims"
	_, err := client.ReapExpiredClaims(ctx, time.Now().UTC(), 10, time.Second)
	if !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("ReapExpiredClaims stale err = %v, want ErrClaimRejected", err)
	}
}
