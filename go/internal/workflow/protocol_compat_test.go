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

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Oldest/newest compatibility for the workflow.v1 claim plane (#9376).
// v1 is both the oldest and the newest supported major, so compatibility
// across minor skew must hold in both directions through the shared post
// path: an oldest server that omits every optional field still yields a
// usable grant, and a newest server that adds unknown fields (including
// unknown error-envelope fields) must not break this client. These tests
// pin the lenient field decode; a strict field decoder is a protocol break.
// Unknown fields stay lenient, but an unknown value for a CLOSED enum is a
// newer major speaking: the client must fail closed as invalid, never as a
// fence rejection and never as silent success (version-matrix same-major
// rule). Open enums such as CollectorKind stay open on decode.

func compatGrantMux(t *testing.T, body map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(body); err != nil {
			t.Errorf("encode compat body: %v", err)
		}
	}))
}

func TestClaimPlaneClientOldestServerSparseGrant(t *testing.T) {
	now := time.Now().UTC()
	server := compatGrantMux(t, map[string]any{
		"issued": true,
		"grant": map[string]any{
			"work_item": map[string]any{
				"work_item_id": "item-1", "run_id": "run-1",
				"collector_kind": "jira", "collector_instance_id": "jira-primary",
				"source_system": "jira", "scope_id": "scope-1",
			},
			"claim": map[string]any{
				"claim_id": "claim-1", "work_item_id": "item-1",
				"fencing_token": 1, "owner_id": "owner-1",
			},
		},
	})
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	item, claim, issued, err := client.ClaimNextEligible(context.Background(), ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		OwnerID:             "owner-1",
		ClaimID:             "claim-1",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("sparse grant: %v", err)
	}
	if !issued || item.WorkItemID != "item-1" || claim.FencingToken != 1 {
		t.Fatalf("sparse grant = %+v %+v %v", item, claim, issued)
	}
}

func TestClaimPlaneClientNewestServerUnknownFields(t *testing.T) {
	now := time.Now().UTC()
	server := compatGrantMux(t, map[string]any{
		"issued": true,
		// Unknown top-level and nested fields simulate a newer minor.
		"protocol_generation": 7,
		"grant": map[string]any{
			"scheduler_pool": "eu-west",
			"work_item": map[string]any{
				"work_item_id": "item-1", "run_id": "run-1",
				"collector_kind": "jira", "collector_instance_id": "jira-primary",
				"source_system": "jira", "scope_id": "scope-1",
				"scheduler_pool":   "eu-west",
				"future_quota_key": "q-9",
			},
			"claim": map[string]any{
				"claim_id": "claim-1", "work_item_id": "item-1",
				"fencing_token": 1, "owner_id": "owner-1",
				"status":     "active",
				"claimed_at": now, "heartbeat_at": now,
				"epoch_generation": 7,
			},
		},
	})
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	item, claim, issued, err := client.ClaimNextEligible(context.Background(), ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		OwnerID:             "owner-1",
		ClaimID:             "claim-1",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("unknown fields: %v", err)
	}
	if !issued || item.WorkItemID != "item-1" || claim.FencingToken != 1 {
		t.Fatalf("unknown fields grant = %+v %+v %v", item, claim, issued)
	}
}

func TestClaimPlaneClientNewestServerUnknownErrorFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": "stale_fence", "message": "epoch superseded",
			"epoch_generation": 7, "retry_after_ns": int64(time.Second),
		})
	}))
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	err := client.HeartbeatClaim(context.Background(), ClaimMutation{
		WorkItemID: "item-1", ClaimID: "claim-1",
		FencingToken: 1, OwnerID: "owner-1",
		ObservedAt: time.Now().UTC(), LeaseDuration: time.Minute,
	})
	if !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("unknown error fields: want ErrClaimRejected, got %v", err)
	}
}

func compatStatusMux(t *testing.T, run map[string]any, progress []map[string]any) *httptest.Server {
	t.Helper()
	now := time.Now().UTC()
	if run == nil {
		run = map[string]any{}
	}
	for key, value := range map[string]any{
		"run_id": "run-1", "trigger_kind": "schedule",
		"status":     "collection_active",
		"created_at": now, "updated_at": now,
	} {
		if _, ok := run[key]; !ok {
			run[key] = value
		}
	}
	return compatGrantMux(t, map[string]any{
		"run": run, "progress": progress, "as_of": now,
	})
}

// TestClaimPlaneClientNewestServerUnknownEnumValue is the oldest/newest
// fail-closed proof: a closed enum carrying a value this major never defined
// is a newer major speaking, so GetRunStatus must report invalid — never a
// fence rejection, and never silent success with the future value decoded.
func TestClaimPlaneClientNewestServerUnknownEnumValue(t *testing.T) {
	server := compatStatusMux(t,
		map[string]any{"status": "FUTURE_99_XYZ"},
		[]map[string]any{{
			"collector_kind":   "jira",
			"total_work_items": 2, "pending_work_items": 2,
		}},
	)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	report, err := client.GetRunStatus(context.Background(), "run-1")
	if err == nil {
		t.Fatalf("unknown run status decoded as success: %+v, want invalid", report.Run.Status)
	}
	if errors.Is(err, ErrClaimRejected) {
		t.Fatalf("unknown run status = %v, must not read as a fence rejection", err)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unknown run status err = %v, want invalid mapping", err)
	}
}

// TestClaimPlaneClientNewestServerOpenKindStaysOpen guards the exemption:
// CollectorKind is an open enum, so a kind this major never defined must
// still decode on the read plane.
func TestClaimPlaneClientNewestServerOpenKindStaysOpen(t *testing.T) {
	server := compatStatusMux(t, nil,
		[]map[string]any{{
			"collector_kind":   "future-kind-xyz",
			"total_work_items": 2, "pending_work_items": 2,
		}},
	)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	report, err := client.GetRunStatus(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("future collector kind: %v, want success", err)
	}
	if len(report.Progress) != 1 || string(report.Progress[0].CollectorKind) != "future-kind-xyz" {
		t.Fatalf("progress = %+v, want the future kind carried through", report.Progress)
	}
}
