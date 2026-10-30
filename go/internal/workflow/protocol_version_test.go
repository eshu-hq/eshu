// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// versionRecordingPlane records the Workflow-Protocol-Version header of every
// request while serving minimal valid workflow.v1 bodies. It proves the
// client pins one protocol major across all routes through the shared post
// path: oldest and newest supported server is v1 until a new major lands
// with its own compat test.
type versionRecordingPlane struct {
	t       testing.TB
	seen    []string
	handler http.Handler
}

func newVersionRecordingPlane(t testing.TB) *versionRecordingPlane {
	p := &versionRecordingPlane{t: t}
	mux := http.NewServeMux()
	write := func(writer http.ResponseWriter, value any) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			p.t.Fatalf("encode stub response: %v", err)
		}
	}
	record := func(next http.HandlerFunc) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			p.seen = append(p.seen, request.Header.Get("Workflow-Protocol-Version"))
			next(writer, request)
		}
	}
	ok := map[string]any{"ok": true}
	now := time.Now().UTC()
	mux.HandleFunc("/workflow.v1/ClaimNext", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{
			"issued": true,
			"grant": map[string]any{
				"work_item": map[string]any{
					"work_item_id": "item-1", "run_id": "run-1",
					"collector_kind": "jira", "collector_instance_id": "jira-primary",
					"source_system": "jira", "scope_id": "scope-1",
					"acceptance_unit_id": "au-1", "source_run_id": "sr-1",
					"generation_id": "gen-1", "status": "claimed",
					"attempt_count": 1, "current_claim_id": "claim-1",
					"current_fencing_token": 1, "current_owner_id": "owner-1",
					"created_at": now, "updated_at": now,
				},
				"claim": map[string]any{
					"claim_id": "claim-1", "work_item_id": "item-1",
					"fencing_token": 1, "owner_id": "owner-1", "status": "active",
					"claimed_at": now, "heartbeat_at": now,
					"lease_expires_at": now.Add(time.Minute),
					"created_at":       now, "updated_at": now,
				},
			},
		})
	}))
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/CompleteClaim", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/ReleaseClaim", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/FailClaimRetryable", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/FailClaimTerminal", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/ValidateAndPinClaimForCommit", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"pinned": true, "commit_id": "commit-1"})
	}))
	mux.HandleFunc("/workflow.v1/CheckClaimPacing", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"paced": false})
	}))
	mux.HandleFunc("/workflow.v1/ListCollectorInstances", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"instances": []map[string]any{}, "as_of": now})
	}))
	mux.HandleFunc("/workflow.v1/CreateRun", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"run_id": "run-1"})
	}))
	mux.HandleFunc("/workflow.v1/EnqueueWorkItems", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"accepted": 1, "inserted_work_items": 1})
	}))
	mux.HandleFunc("/workflow.v1/ReconcileCollectorInstances", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/UpsertCompletenessStates", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, ok)
	}))
	mux.HandleFunc("/workflow.v1/ReconcileWorkflowRuns", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{"transitioned": 0})
	}))
	mux.HandleFunc("/workflow.v1/ReapExpiredClaims", record(func(writer http.ResponseWriter, _ *http.Request) {
		write(writer, map[string]any{})
	}))
	p.handler = mux
	return p
}

func TestClaimPlaneClientPinsProtocolMajorOne(t *testing.T) {
	plane := newVersionRecordingPlane(t)
	server := httptest.NewServer(plane.handler)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, _, err := client.ClaimNextEligible(ctx, testSelector(), now, time.Minute); err != nil {
		t.Fatalf("claim next: %v", err)
	}
	for _, op := range []struct {
		name string
		call func() error
	}{
		{"heartbeat", func() error { return client.HeartbeatClaim(ctx, testMutation()) }},
		{"complete", func() error { return client.CompleteClaim(ctx, testMutation()) }},
		{"release", func() error { return client.ReleaseClaim(ctx, testMutation()) }},
		{"fail retryable", func() error { return client.FailClaimRetryable(ctx, testMutation()) }},
		{"fail terminal", func() error { return client.FailClaimTerminal(ctx, testMutation()) }},
	} {
		if err := op.call(); err != nil {
			t.Fatalf("%s: %v", op.name, err)
		}
	}
	if _, err := client.PinClaimForCommit(ctx, "claim-1", 1, "scope-1", "sr-1", "gen-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if _, err := client.CheckClaimPacing(ctx, testSelector(), ClaimPacingConfig{}, now); err != nil {
		t.Fatalf("pacing: %v", err)
	}
	if _, err := client.ListCollectorInstances(ctx); err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if err := client.CreateRun(ctx, Run{}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := client.EnqueueWorkItems(ctx, []WorkItem{{}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := client.ReconcileCollectorInstances(ctx, now, nil); err != nil {
		t.Fatalf("reconcile instances: %v", err)
	}
	if err := client.UpsertCompletenessStates(ctx, []CompletenessState{{}}); err != nil {
		t.Fatalf("upsert completeness: %v", err)
	}
	if _, err := client.ReconcileWorkflowRuns(ctx, now); err != nil {
		t.Fatalf("reconcile runs: %v", err)
	}
	if _, err := client.ReapExpiredClaims(ctx, now, 10, time.Minute); err != nil {
		t.Fatalf("reap: %v", err)
	}

	const wantRoutes = 15
	if len(plane.seen) != wantRoutes {
		t.Fatalf("recorded %d versioned requests, want %d", len(plane.seen), wantRoutes)
	}
	for i, got := range plane.seen {
		if got != "1" {
			t.Fatalf("request %d protocol version = %q, want \"1\"", i, got)
		}
	}
}

func TestClaimPlaneClientVersionMismatchIsInvalidNotRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": "version_mismatch", "message": "server requires major 2",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	err := client.HeartbeatClaim(context.Background(), testMutation())
	if err == nil {
		t.Fatal("mismatch heartbeat succeeded, want invalid error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("mismatch err = %v, want invalid mapping", err)
	}
	if errors.Is(err, ErrClaimRejected) {
		t.Fatalf("mismatch err = %v, must not read as a fence rejection", err)
	}
}
