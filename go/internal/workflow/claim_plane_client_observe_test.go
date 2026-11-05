// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// observation is one recorded Observe call.
type observation struct {
	operation string
	outcome   ClaimPlaneOutcome
	elapsed   time.Duration
}

// observationLog collects Observe calls; the client may be used from
// several goroutines, so appends are locked.
type observationLog struct {
	mu   sync.Mutex
	seen []observation
}

func (l *observationLog) observe(_ context.Context, operation string, outcome ClaimPlaneOutcome, elapsed time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, observation{operation: operation, outcome: outcome, elapsed: elapsed})
}

func (l *observationLog) only(t *testing.T) observation {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) != 1 {
		t.Fatalf("observations = %d (%+v), want exactly 1", len(l.seen), l.seen)
	}
	return l.seen[0]
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	}
}

func TestClaimPlaneClientObserveReportsOutcomePerProtocolResult(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  int
		body    string
		call    func(*ClaimPlaneClient) error
		wantOp  string
		wantOut ClaimPlaneOutcome
	}{
		{
			name: "heartbeat ok", status: http.StatusOK, body: `{"ok":true}`,
			call:   func(c *ClaimPlaneClient) error { return c.HeartbeatClaim(context.Background(), ClaimMutation{}) },
			wantOp: "HeartbeatClaim", wantOut: ClaimPlaneOutcomeOK,
		},
		{
			name: "stale fence is rejected", status: http.StatusConflict,
			body:   `{"code":"stale_fence","message":"epoch superseded"}`,
			call:   func(c *ClaimPlaneClient) error { return c.CompleteClaim(context.Background(), ClaimMutation{}) },
			wantOp: "CompleteClaim", wantOut: ClaimPlaneOutcomeRejected,
		},
		{
			name: "idle claim next is not found", status: http.StatusNotFound,
			body: `{"code":"not_found","message":"no eligible work"}`,
			call: func(c *ClaimPlaneClient) error {
				_, _, _, err := c.ClaimNextEligible(context.Background(), ClaimSelector{}, time.Now(), time.Minute)
				return err
			},
			wantOp: "ClaimNext", wantOut: ClaimPlaneOutcomeNotFound,
		},
		{
			name: "version mismatch is invalid", status: http.StatusBadRequest,
			body:   `{"code":"version_mismatch","message":"want 1"}`,
			call:   func(c *ClaimPlaneClient) error { return c.ReleaseClaim(context.Background(), ClaimMutation{}) },
			wantOp: "ReleaseClaim", wantOut: ClaimPlaneOutcomeInvalid,
		},
		{
			name: "paced envelope is paced", status: http.StatusTooManyRequests,
			body:   `{"code":"paced","message":"slow down"}`,
			call:   func(c *ClaimPlaneClient) error { return c.FailClaimRetryable(context.Background(), ClaimMutation{}) },
			wantOp: "FailClaimRetryable", wantOut: ClaimPlaneOutcomePaced,
		},
		{
			name: "undecodable 502 is server error", status: http.StatusBadGateway,
			body:   `<html>bad gateway</html>`,
			call:   func(c *ClaimPlaneClient) error { return c.FailClaimTerminal(context.Background(), ClaimMutation{}) },
			wantOp: "FailClaimTerminal", wantOut: ClaimPlaneOutcomeServerError,
		},
		{
			name: "garbled 200 is decode error", status: http.StatusOK, body: `{"instances":`,
			call: func(c *ClaimPlaneClient) error {
				_, err := c.ListCollectorInstances(context.Background())
				return err
			},
			wantOp: "ListCollectorInstances", wantOut: ClaimPlaneOutcomeDecodeError,
		},
		{
			name: "create run ok", status: http.StatusOK, body: `{"run_id":"run-1"}`,
			call:   func(c *ClaimPlaneClient) error { return c.CreateRun(context.Background(), Run{RunID: "run-1"}) },
			wantOp: "CreateRun", wantOut: ClaimPlaneOutcomeOK,
		},
		{
			name: "enqueue work items ok", status: http.StatusOK, body: `{"accepted":1,"inserted_work_items":1}`,
			call: func(c *ClaimPlaneClient) error {
				return c.EnqueueWorkItems(context.Background(), []WorkItem{{WorkItemID: "item-1"}})
			},
			wantOp: "EnqueueWorkItems", wantOut: ClaimPlaneOutcomeOK,
		},
		{
			name: "reconcile instances ok", status: http.StatusOK, body: `{"ok":true}`,
			call: func(c *ClaimPlaneClient) error {
				return c.ReconcileCollectorInstances(context.Background(), time.Now(), nil)
			},
			wantOp: "ReconcileCollectorInstances", wantOut: ClaimPlaneOutcomeOK,
		},
		{
			name: "upsert completeness version mismatch is invalid", status: http.StatusBadRequest,
			body: `{"code":"version_mismatch","message":"want 1"}`,
			call: func(c *ClaimPlaneClient) error {
				return c.UpsertCompletenessStates(context.Background(), []CompletenessState{{RunID: "run-1"}})
			},
			wantOp: "UpsertCompletenessStates", wantOut: ClaimPlaneOutcomeInvalid,
		},
		{
			name: "reconcile runs ok", status: http.StatusOK, body: `{"transitioned":0}`,
			call: func(c *ClaimPlaneClient) error {
				_, err := c.ReconcileWorkflowRuns(context.Background(), time.Now())
				return err
			},
			wantOp: "ReconcileWorkflowRuns", wantOut: ClaimPlaneOutcomeOK,
		},
		{
			name: "reap expired claims ok", status: http.StatusOK, body: `{"claims":[]}`,
			call: func(c *ClaimPlaneClient) error {
				_, err := c.ReapExpiredClaims(context.Background(), time.Now(), 10, time.Minute)
				return err
			},
			wantOp: "ReapExpiredClaims", wantOut: ClaimPlaneOutcomeOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(respondWith(tc.status, tc.body))
			defer server.Close()
			log := &observationLog{}
			client := NewClaimPlaneClient(server.URL, server.Client())
			client.Observe = log.observe
			err := tc.call(client)
			if tc.wantOut == ClaimPlaneOutcomeOK && err != nil {
				t.Fatalf("call error = %v, want nil", err)
			}
			got := log.only(t)
			if got.operation != tc.wantOp || got.outcome != tc.wantOut {
				t.Fatalf("observed (%s, %s), want (%s, %s)", got.operation, got.outcome, tc.wantOp, tc.wantOut)
			}
			if got.elapsed < 0 {
				t.Fatalf("elapsed = %v, want non-negative", got.elapsed)
			}
		})
	}
}

func TestClaimPlaneClientObserveReportsTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	httpClient := server.Client()
	httpClient.Timeout = 50 * time.Millisecond
	log := &observationLog{}
	client := NewClaimPlaneClient(server.URL, httpClient)
	client.Observe = log.observe
	if err := client.HeartbeatClaim(context.Background(), ClaimMutation{}); err == nil {
		t.Fatal("HeartbeatClaim error = nil, want timeout")
	}
	if got := log.only(t); got.outcome != ClaimPlaneOutcomeTimeout {
		t.Fatalf("outcome = %s, want %s", got.outcome, ClaimPlaneOutcomeTimeout)
	}
}

func TestClaimPlaneClientObserveReportsTransportError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(respondWith(http.StatusOK, `{}`))
	baseURL := server.URL
	server.Close() // nothing listens on baseURL any more
	log := &observationLog{}
	client := NewClaimPlaneClient(baseURL, nil)
	client.Observe = log.observe
	if err := client.HeartbeatClaim(context.Background(), ClaimMutation{}); err == nil {
		t.Fatal("HeartbeatClaim error = nil, want transport error")
	}
	if got := log.only(t); got.outcome != ClaimPlaneOutcomeTransportError {
		t.Fatalf("outcome = %s, want %s", got.outcome, ClaimPlaneOutcomeTransportError)
	}
}

// TestClaimPlaneClientObserveReportsCanceled proves a request cut short by
// the caller's context cancellation (a SIGTERM during a rollout) reports
// canceled, not transport_error, so a rollout does not look like an
// unreachable claim plane on the outage dashboard.
func TestClaimPlaneClientObserveReportsCanceled(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	arrived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(arrived)
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()
	log := &observationLog{}
	client := NewClaimPlaneClient(server.URL, server.Client())
	client.Observe = log.observe
	if err := client.HeartbeatClaim(ctx, ClaimMutation{}); err == nil {
		t.Fatal("HeartbeatClaim error = nil, want cancellation")
	}
	if got := log.only(t); got.outcome != ClaimPlaneOutcomeCanceled {
		t.Fatalf("outcome = %s, want %s", got.outcome, ClaimPlaneOutcomeCanceled)
	}
}

func TestClaimPlaneClientWithoutObserveStillServes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(respondWith(http.StatusOK, `{"ok":true}`))
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())
	if err := client.HeartbeatClaim(context.Background(), ClaimMutation{}); err != nil {
		t.Fatalf("HeartbeatClaim error = %v, want nil with no observer", err)
	}
}
