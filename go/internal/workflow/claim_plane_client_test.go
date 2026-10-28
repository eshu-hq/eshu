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

// stubClaimPlane serves the workflow.v1 claim routes from scripted state
// for client tests.
type stubClaimPlane struct {
	t           testing.TB
	grant       bool
	paced       bool
	staleOp     string
	instances   []map[string]any
	pinCommitID string
}

func (s *stubClaimPlane) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(writer http.ResponseWriter, status int, value any) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			s.t.Fatalf("encode stub response: %v", err)
		}
	}
	envelope := func(code, message string) map[string]any {
		return map[string]any{"code": code, "message": message}
	}
	mux.HandleFunc("/workflow.v1/ClaimNext", func(writer http.ResponseWriter, request *http.Request) {
		if s.staleOp == "ClaimNext" {
			write(writer, http.StatusConflict, envelope("stale_fence", "epoch superseded"))
			return
		}
		if s.paced {
			write(writer, http.StatusOK, map[string]any{
				"issued": false,
				"deferral": map[string]any{
					"signal": "domain", "domain": "jira/jira-primary",
					"retry_after_ns": int64(5 * time.Second),
				},
			})
			return
		}
		if !s.grant {
			write(writer, http.StatusNotFound, envelope("not_found", "no eligible work"))
			return
		}
		now := time.Now().UTC()
		write(writer, http.StatusOK, map[string]any{
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
	})
	mutate := func(op string) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if s.staleOp == op {
				write(writer, http.StatusConflict, envelope("stale_fence", "epoch superseded"))
				return
			}
			write(writer, http.StatusOK, map[string]any{"ok": true})
		}
	}
	mux.HandleFunc("/workflow.v1/HeartbeatClaim", mutate("HeartbeatClaim"))
	mux.HandleFunc("/workflow.v1/CompleteClaim", mutate("CompleteClaim"))
	mux.HandleFunc("/workflow.v1/ReleaseClaim", mutate("ReleaseClaim"))
	mux.HandleFunc("/workflow.v1/FailClaimRetryable", mutate("FailClaimRetryable"))
	mux.HandleFunc("/workflow.v1/FailClaimTerminal", mutate("FailClaimTerminal"))
	mux.HandleFunc("/workflow.v1/ValidateAndPinClaimForCommit",
		func(writer http.ResponseWriter, request *http.Request) {
			if s.staleOp == "ValidateAndPinClaimForCommit" {
				write(writer, http.StatusConflict, envelope("stale_fence", "epoch superseded"))
				return
			}
			write(writer, http.StatusOK, map[string]any{
				"pinned": true, "commit_id": s.pinCommitID,
			})
		})
	mux.HandleFunc("/workflow.v1/CheckClaimPacing",
		func(writer http.ResponseWriter, request *http.Request) {
			write(writer, http.StatusOK, map[string]any{"paced": false})
		})
	mux.HandleFunc("/workflow.v1/ListCollectorInstances",
		func(writer http.ResponseWriter, request *http.Request) {
			instances := s.instances
			if instances == nil {
				instances = []map[string]any{}
			}
			write(writer, http.StatusOK, map[string]any{
				"instances": instances,
				"as_of":     time.Now().UTC(),
			})
		})
	return mux
}

func testSelector() ClaimSelector {
	return ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		OwnerID:             "owner-1",
		ClaimID:             "claim-1",
	}
}

func testMutation() ClaimMutation {
	return ClaimMutation{
		WorkItemID: "item-1", ClaimID: "claim-1",
		FencingToken: 1, OwnerID: "owner-1",
		ObservedAt: time.Now().UTC(), LeaseDuration: time.Minute,
	}
}

func TestClaimPlaneClientClaimLifecycle(t *testing.T) {
	stub := &stubClaimPlane{t: t, grant: true, pinCommitID: "commit-claim-1-1"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()

	item, claim, issued, err := client.ClaimNextEligible(ctx, testSelector(), time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !issued || item.WorkItemID != "item-1" || claim.FencingToken != 1 {
		t.Fatalf("grant = %+v %+v %v", item, claim, issued)
	}
	if err := client.HeartbeatClaim(ctx, testMutation()); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	commitID, err := client.PinClaimForCommit(ctx, "claim-1", 1, "scope-1", "sr-1", "gen-1",
		time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if commitID != "commit-claim-1-1" {
		t.Fatalf("commit id = %q", commitID)
	}
	if err := client.CompleteClaim(ctx, testMutation()); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := client.ReleaseClaim(ctx, testMutation()); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := client.FailClaimRetryable(ctx, testMutation()); err != nil {
		t.Fatalf("fail retryable: %v", err)
	}
	if err := client.FailClaimTerminal(ctx, testMutation()); err != nil {
		t.Fatalf("fail terminal: %v", err)
	}
	decision, err := client.CheckClaimPacing(ctx, testSelector(), ClaimPacingConfig{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("pacing: %v", err)
	}
	if decision.Paced {
		t.Fatalf("pacing = %+v, want unpaced", decision)
	}
}

func TestClaimPlaneClientStaleMapsToRejected(t *testing.T) {
	stub := &stubClaimPlane{t: t, grant: true, staleOp: "HeartbeatClaim"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	err := client.HeartbeatClaim(context.Background(), testMutation())
	if !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("stale heartbeat err = %v, want ErrClaimRejected", err)
	}
}

func TestClaimPlaneClientIdleAndPaced(t *testing.T) {
	stub := &stubClaimPlane{t: t}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()

	_, _, issued, err := client.ClaimNextEligible(ctx, testSelector(), time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatalf("idle claim err = %v", err)
	}
	if issued {
		t.Fatalf("idle claim issued a grant")
	}

	stub.paced = true
	_, _, _, err = client.ClaimNextEligible(ctx, testSelector(), time.Now().UTC(), time.Minute)
	paced, ok := err.(*ClaimPacedError)
	if !ok {
		t.Fatalf("paced claim err = %v (%T), want *ClaimPacedError", err, err)
	}
	if paced.RetryAfter != 5*time.Second {
		t.Fatalf("paced retry_after = %s", paced.RetryAfter)
	}
}

// BenchmarkClaimPlaneClientClaimNext bounds the per-claim HTTP overhead
// the seam adds on the collector claim path: one grant round trip over
// loopback against a stub server. Evidence numbers live in
// claim-plane-client-evidence.md beside this file.
func BenchmarkClaimPlaneClientClaimNext(b *testing.B) {
	stub := &stubClaimPlane{t: b, grant: true, pinCommitID: "commit-claim-1-1"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	ctx := context.Background()
	selector := testSelector()
	now := time.Now().UTC()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := client.ClaimNextEligible(ctx, selector, now, time.Minute); err != nil {
			b.Fatalf("claim: %v", err)
		}
	}
}
