// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestNewSatelliteBackendRoutesThroughTheClient: the exported constructor
// wraps the caller's client, so a fenced-away epoch on the wire still
// classifies as CodeRejected through the leg wrapper.
func TestNewSatelliteBackendRoutesThroughTheClient(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"stale_fence","message":"superseded"}`))
	}))
	defer server.Close()
	backend := NewSatelliteBackend(workflow.NewClaimPlaneClient(server.URL, server.Client()))
	if backend.Name() != "satellite" {
		t.Fatalf("Name = %q, want satellite", backend.Name())
	}
	err := backend.HeartbeatClaim(context.Background(), workflow.ClaimMutation{WorkItemID: "wi", ClaimID: "c", FencingToken: 1, OwnerID: "o"})
	if Classify(err) != CodeRejected {
		t.Fatalf("Classify(%v) = %s, want %s", err, Classify(err), CodeRejected)
	}
	if path != "/workflow.v1/HeartbeatClaim" || !strings.Contains(err.Error(), "parity satellite HeartbeatClaim") {
		t.Fatalf("path %q err %v: call did not route through the satellite leg", path, err)
	}
}

// TestNewInTreeBackendWrapsTheStore: the exported constructor wraps the
// caller's store and keeps the leg error context.
func TestNewInTreeBackendWrapsTheStore(t *testing.T) {
	backend := NewInTreeBackend(postgres.NewWorkflowControlStore(nil))
	if backend.Name() != "in-tree" {
		t.Fatalf("Name = %q, want in-tree", backend.Name())
	}
	err := backend.CreateRun(context.Background(), workflow.Run{})
	if err == nil || !strings.Contains(err.Error(), "parity in-tree CreateRun") {
		t.Fatalf("CreateRun on a store without a database: err = %v, want the in-tree leg wrapper", err)
	}
	if _, err := backend.PinForCommit(context.Background(), "c", 1, "s", "sr", "g", time.Now()); Classify(err) != CodePinUnsupported {
		t.Fatalf("PinForCommit: %v, want %s", err, CodePinUnsupported)
	}
}
