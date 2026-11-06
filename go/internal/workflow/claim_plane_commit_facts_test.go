// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// oversizedCommitResult builds a syntactically closed JSON document one byte
// past the carrier cap. Content is irrelevant: the client must reject on
// size before any byte reaches the wire.
func oversizedCommitResult() json.RawMessage {
	raw := []byte(`{"state":"complete","pad":"`)
	for len(raw) < MaxCommitClaimFactsBytes+1 {
		raw = append(raw, 'x')
	}
	return append(raw, []byte(`"}`)...)
}

// commitFactsStubPlane serves the commit-lane route the way the satellite
// must: strict-decode the body, hold the exact bytes it received, and ack.
// A body with any field outside the contract fails the request, proving the
// client sends exactly the known shape and nothing else.
type commitFactsStubPlane struct {
	t               *testing.T
	seenVersion     string
	idempotencyKey  string
	resultJSON      string
	workItemID      string
	fencingToken    float64
	ownerID         string
	leaseDurationNS float64
	scopeID         string
	sourceRunID     string
	generationID    string
	calls           int
}

// commitFactsMutation returns the fenced epoch with the planned IDs the
// server matches against the plane's item row: core never scrapes them out
// of the result echo.
func commitFactsMutation() ClaimMutation {
	mutation := testMutation()
	mutation.ScopeID = "scope-commit-lane"
	mutation.SourceRunID = "source-run-commit-lane"
	mutation.GenerationID = "gen-commit-lane"
	return mutation
}

func (p *commitFactsStubPlane) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/CommitClaimFacts", func(writer http.ResponseWriter, request *http.Request) {
		p.calls++
		p.seenVersion = request.Header.Get("Workflow-Protocol-Version")
		raw, err := io.ReadAll(io.LimitReader(request.Body, MaxCommitClaimFactsBytes+1))
		if err != nil {
			p.t.Errorf("read body: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			WorkItemID     string          `json:"work_item_id"`
			ClaimID        string          `json:"claim_id"`
			FencingToken   float64         `json:"fencing_token"`
			OwnerID        string          `json:"owner_id"`
			ObservedAt     string          `json:"observed_at"`
			LeaseDuration  float64         `json:"lease_duration_ns"`
			VisibleAt      string          `json:"visible_at,omitempty"`
			ScopeID        string          `json:"scope_id"`
			SourceRunID    string          `json:"source_run_id"`
			GenerationID   string          `json:"generation_id"`
			IdempotencyKey string          `json:"idempotency_key"`
			ResultJSON     json.RawMessage `json:"result_json"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"code":"invalid","message":"strict shape"}`))
			return
		}
		p.idempotencyKey = body.IdempotencyKey
		p.resultJSON = string(body.ResultJSON)
		p.scopeID = body.ScopeID
		p.sourceRunID = body.SourceRunID
		p.generationID = body.GenerationID
		p.workItemID = body.WorkItemID
		p.fencingToken = body.FencingToken
		p.ownerID = body.OwnerID
		p.leaseDurationNS = body.LeaseDuration
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{\"ok\":true}\n"))
	})
	return mux
}

func TestClaimPlaneClientCommitClaimFactsRoundTripsBytes(t *testing.T) {
	t.Parallel()

	plane := &commitFactsStubPlane{t: t}
	server := httptest.NewServer(plane.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	result := json.RawMessage(`{"protocol_version":"collector-sdk/v1alpha1","state":"complete","facts":[]}`)
	if err := client.CommitClaimFacts(context.Background(), commitFactsMutation(), "commit-lane-key-1", result); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if plane.calls != 1 {
		t.Fatalf("plane calls = %d, want 1", plane.calls)
	}
	if plane.seenVersion != "1" {
		t.Fatalf("protocol version = %q, want \"1\"", plane.seenVersion)
	}
	if plane.idempotencyKey != "commit-lane-key-1" {
		t.Fatalf("idempotency key = %q, want commit-lane-key-1", plane.idempotencyKey)
	}
	if plane.resultJSON != string(result) {
		t.Fatalf("result bytes = %q, want verbatim producer bytes", plane.resultJSON)
	}
	if plane.workItemID != "item-1" || plane.fencingToken != 1 {
		t.Fatalf("fenced identity = %q/%v, want item-1/1", plane.workItemID, plane.fencingToken)
	}
	if plane.ownerID != "owner-1" || plane.leaseDurationNS != float64(60000000000) {
		t.Fatalf("fence owner/lease = %q/%v, want owner-1/60000000000", plane.ownerID, plane.leaseDurationNS)
	}
	if plane.scopeID != "scope-commit-lane" || plane.sourceRunID != "source-run-commit-lane" || plane.generationID != "gen-commit-lane" {
		t.Fatalf("planned ids = %q/%q/%q, want scope-commit-lane/source-run-commit-lane/gen-commit-lane",
			plane.scopeID, plane.sourceRunID, plane.generationID)
	}
}

func TestClaimPlaneClientCommitClaimFactsRejectsOversizedResult(t *testing.T) {
	t.Parallel()

	plane := &commitFactsStubPlane{t: t}
	server := httptest.NewServer(plane.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	// commitFactsMutation carries the planned IDs so the size check is the
	// only failure: deleting it must turn this test red via the wire call.
	err := client.CommitClaimFacts(context.Background(), commitFactsMutation(), "commit-lane-key-big", oversizedCommitResult())
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want oversized rejection")
	}
	if plane.calls != 0 {
		t.Fatalf("plane calls = %d, want 0: oversized bytes never reach the wire", plane.calls)
	}
}

func TestClaimPlaneClientCommitClaimFactsRefusesNegativeAck(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/workflow.v1/CommitClaimFacts", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{\"ok\":false}\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	err := client.CommitClaimFacts(
		context.Background(),
		commitFactsMutation(),
		"commit-lane-key-nack",
		json.RawMessage(`{"state":"complete"}`),
	)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want ok=false rejection")
	}
}

func TestClaimPlaneClientCommitClaimFactsRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()

	plane := &commitFactsStubPlane{t: t}
	server := httptest.NewServer(plane.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	err := client.CommitClaimFacts(
		context.Background(),
		testMutation(),
		"",
		json.RawMessage(`{"state":"complete"}`),
	)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want missing key rejection")
	}
	if plane.calls != 0 {
		t.Fatalf("plane calls = %d, want 0 for a keyless carrier", plane.calls)
	}
}

// TestClaimPlaneClientCommitClaimFactsRequiresPlannedIDs pins decision 4:
// the planned IDs travel beside the result so core never scrapes its target
// out of the echo. A carrier missing them fails before any byte is sent.
func TestClaimPlaneClientCommitClaimFactsRequiresPlannedIDs(t *testing.T) {
	t.Parallel()

	plane := &commitFactsStubPlane{t: t}
	server := httptest.NewServer(plane.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	mutation := commitFactsMutation()
	mutation.GenerationID = ""
	err := client.CommitClaimFacts(
		context.Background(),
		mutation,
		"commit-lane-key-no-planned",
		json.RawMessage(`{"state":"complete"}`),
	)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want missing planned-id rejection")
	}
	if plane.calls != 0 {
		t.Fatalf("plane calls = %d, want 0 for a carrier missing planned ids", plane.calls)
	}
}

// TestClaimPlaneClientCommitClaimFactsRejectsBodyOverCap pins the total-body
// bound: a result under its own cap but an envelope past the body cap fails
// before any byte reaches the wire.
func TestClaimPlaneClientCommitClaimFactsRejectsBodyOverCap(t *testing.T) {
	t.Parallel()

	plane := &commitFactsStubPlane{t: t}
	server := httptest.NewServer(plane.handler())
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	mutation := commitFactsMutation()
	mutation.ScopeID = strings.Repeat("s", MaxCommitClaimFactsBodyBytes)
	err := client.CommitClaimFacts(
		context.Background(),
		mutation,
		"commit-lane-key-big-body",
		json.RawMessage(`{"state":"complete"}`),
	)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want body-cap rejection")
	}
	if plane.calls != 0 {
		t.Fatalf("plane calls = %d, want 0: an over-cap body never reaches the wire", plane.calls)
	}
}
