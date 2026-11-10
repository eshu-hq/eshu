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
// rule). Open enums such as CollectorKind stay open on decode. The pacing
// signal is the one advisory exception (#10377, arbiter ruling): a tolerant
// reader widens it to the protective global default with a conservative
// directive and a bounded count, because a hard error there would starve
// claims on upgrade skew.

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

// requireInvalidNotRejected asserts the oldest/newest fail-closed contract:
// a closed enum carrying a value this major never defined is a newer major
// speaking, so the call must report invalid — never a fence rejection, and
// never silent success with the future value decoded.
func requireInvalidNotRejected(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s decoded as success, want invalid", what)
	}
	if errors.Is(err, ErrClaimRejected) {
		t.Fatalf("%s = %v, must not read as a fence rejection", what, err)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("%s err = %v, want invalid mapping", what, err)
	}
}

// TestClaimPlaneClientNewestServerUnknownEnumValue is the oldest/newest
// fail-closed proof for run status: GetRunStatus must report invalid.
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

	_, err := client.GetRunStatus(context.Background(), "run-1")
	requireInvalidNotRejected(t, "unknown run status", err)
}

// TestClaimPlaneClientNewestServerUnknownTriggerKind covers the second
// closed enum on the run wire shape: an unknown trigger kind fails closed.
func TestClaimPlaneClientNewestServerUnknownTriggerKind(t *testing.T) {
	server := compatStatusMux(t,
		map[string]any{"trigger_kind": "FUTURE_99_XYZ"},
		[]map[string]any{{
			"collector_kind":   "jira",
			"total_work_items": 2, "pending_work_items": 2,
		}},
	)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	_, err := client.GetRunStatus(context.Background(), "run-1")
	requireInvalidNotRejected(t, "unknown run trigger kind", err)
}

// compatGrantStub serves one ClaimNext grant with the given work-item and
// claim statuses through the shared lenient-field mux.
func compatGrantStub(t *testing.T, itemStatus, claimStatus string) *httptest.Server {
	t.Helper()
	now := time.Now().UTC()
	return compatGrantMux(t, map[string]any{
		"issued": true,
		"grant": map[string]any{
			"work_item": map[string]any{
				"work_item_id": "item-1", "run_id": "run-1",
				"collector_kind": "jira", "collector_instance_id": "jira-primary",
				"source_system": "jira", "scope_id": "scope-1",
				"status":        itemStatus,
				"attempt_count": 1,
				"created_at":    now, "updated_at": now,
			},
			"claim": map[string]any{
				"claim_id": "claim-1", "work_item_id": "item-1",
				"fencing_token": 1, "owner_id": "owner-1",
				"status":     claimStatus,
				"claimed_at": now, "heartbeat_at": now,
				"created_at": now, "updated_at": now,
			},
		},
	})
}

func compatClaimSelector() ClaimSelector {
	return ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		OwnerID:             "owner-1",
		ClaimID:             "claim-1",
	}
}

// TestClaimPlaneClientNewestServerUnknownGrantStatuses covers the claim
// path pair: an unknown work-item status and an unknown claim status each
// fail closed on ClaimNextEligible. The reap path delegates to the same
// claim converter, so covering it here guards both callers.
func TestClaimPlaneClientNewestServerUnknownGrantStatuses(t *testing.T) {
	now := time.Now().UTC()
	for _, grant := range []struct {
		name        string
		itemStatus  string
		claimStatus string
	}{
		{"work-item status", "FUTURE_99_XYZ", "active"},
		{"claim status", "claimed", "FUTURE_99_XYZ"},
	} {
		t.Run(grant.name, func(t *testing.T) {
			server := compatGrantStub(t, grant.itemStatus, grant.claimStatus)
			defer server.Close()
			client := NewClaimPlaneClient(server.URL, server.Client())

			_, _, _, err := client.ClaimNextEligible(
				context.Background(), compatClaimSelector(), now, time.Minute)
			requireInvalidNotRejected(t, "unknown "+grant.name, err)
		})
	}
}

// TestClaimPlaneClientNewestServerUnknownCollectorMode covers the last
// validated closed enum: an unknown instance mode fails closed on
// ListCollectorInstances.
func TestClaimPlaneClientNewestServerUnknownCollectorMode(t *testing.T) {
	server := compatGrantMux(t, map[string]any{
		"instances": []map[string]any{{
			"instance_id": "i-1", "collector_kind": "jira",
			"mode":           "FUTURE_99_XYZ",
			"enabled":        true,
			"claims_enabled": true,
			"created_at":     time.Now().UTC(), "updated_at": time.Now().UTC(),
		}},
	})
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	_, err := client.ListCollectorInstances(context.Background())
	requireInvalidNotRejected(t, "unknown collector mode", err)
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

// TestClaimPlaneClientNewestServerUnknownPacingSignal is the advisory
// exception to the fail-closed enum rule (#10377, arbiter ruling): an
// unknown pacing signal still validates as invalid at the boundary, but
// the client remaps it to the protective global default with the
// directive preserved and counted, instead of erroring and starving
// claims on upgrade skew.
func TestClaimPlaneClientNewestServerUnknownPacingSignal(t *testing.T) {
	newRecorder := func() (*[]ClaimPlaneOutcome, ClaimPlaneObserver) {
		var outcomes []ClaimPlaneOutcome
		return &outcomes, func(_ context.Context, _ string, outcome ClaimPlaneOutcome, _ time.Duration) {
			outcomes = append(outcomes, outcome)
		}
	}
	requireUnknownOutcome := func(t *testing.T, what string, outcomes *[]ClaimPlaneOutcome) {
		t.Helper()
		if len(*outcomes) != 1 || (*outcomes)[0] != ClaimPlaneOutcomePacingSignalUnknown {
			t.Fatalf("%s outcomes = %v, want exactly one pacing_signal_unknown", what, *outcomes)
		}
	}

	t.Run("claim_next_remaps", func(t *testing.T) {
		server := compatGrantMux(t, map[string]any{
			"issued": false,
			"deferral": map[string]any{
				"signal": "FUTURE_99_XYZ", "domain": "x/y",
				"retry_after_ns": int64(5 * time.Second), "stuck": true,
			},
		})
		defer server.Close()
		client := NewClaimPlaneClient(server.URL, server.Client())
		outcomes, observe := newRecorder()
		client.Observe = observe

		_, _, issued, err := client.ClaimNextEligible(context.Background(), testSelector(), time.Now().UTC(), time.Minute)
		if issued {
			t.Fatalf("unknown signal issued a grant")
		}
		var deferral *ClaimPacingDeferral
		if !errors.As(err, &deferral) {
			t.Fatalf("unknown signal err = %v (%T), want *ClaimPacingDeferral", err, err)
		}
		if errors.Is(err, ErrClaimRejected) {
			t.Fatalf("unknown signal = %v, must not read as a fence rejection", err)
		}
		if deferral.Signal != ClaimPacingSignalGlobal || deferral.Domain != "" {
			t.Fatalf("unknown signal deferral = %+v, want signal=global domain cleared", deferral)
		}
		if deferral.RetryAfterDelay() != 5*time.Second || !deferral.Stuck {
			t.Fatalf("unknown signal deferral = %+v, want retry_after=5s stuck=true", deferral)
		}
		requireUnknownOutcome(t, "claim_next", outcomes)
	})

	t.Run("check_pacing_paced", func(t *testing.T) {
		server := compatGrantMux(t, map[string]any{
			"paced": true, "signal": "FUTURE_99_XYZ",
			"retry_after_ns": int64(3 * time.Second),
		})
		defer server.Close()
		client := NewClaimPlaneClient(server.URL, server.Client())
		outcomes, observe := newRecorder()
		client.Observe = observe

		decision, err := client.CheckClaimPacing(context.Background(), testSelector(), ClaimPacingConfig{}, time.Now().UTC())
		if err != nil {
			t.Fatalf("unknown paced signal err = %v, want success with remap", err)
		}
		if !decision.Paced || decision.Signal != ClaimPacingSignalGlobal || decision.RetryAfter != 3*time.Second {
			t.Fatalf("unknown paced decision = %+v, want paced global 3s", decision)
		}
		requireUnknownOutcome(t, "check_pacing", outcomes)
	})

	t.Run("check_pacing_unpaced", func(t *testing.T) {
		server := compatGrantMux(t, map[string]any{
			"paced": false, "signal": "FUTURE_99_XYZ",
		})
		defer server.Close()
		client := NewClaimPlaneClient(server.URL, server.Client())
		outcomes, observe := newRecorder()
		client.Observe = observe

		decision, err := client.CheckClaimPacing(context.Background(), testSelector(), ClaimPacingConfig{}, time.Now().UTC())
		if err != nil {
			t.Fatalf("unknown unpaced signal err = %v, want success", err)
		}
		if decision.Paced || decision.Signal != "" {
			t.Fatalf("unknown unpaced decision = %+v, want unpaced with signal dropped", decision)
		}
		requireUnknownOutcome(t, "check_pacing_unpaced", outcomes)
	})
}
