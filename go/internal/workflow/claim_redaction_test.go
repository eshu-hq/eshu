// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The failure-operation token is the only failure field with a bounded
// contract: producers sanitize it to [a-z0-9_] and both the in-tree store
// and the satellite document "never a URL, response body, or credential".
// These tests prove the claim path refuses hostile values fail-closed
// instead of persisting them to either store or logging them on mismatch.

func TestValidateClaimFailureOperationAcceptsBoundedTokens(t *testing.T) {
	for _, operation := range []string{
		"",
		"collect_failure",
		"fetch",
		"unknown_operation",
		strings.Repeat("a", 64),
	} {
		if err := ValidateClaimFailureOperation(operation); err != nil {
			t.Errorf("ValidateClaimFailureOperation(%q) error = %v, want nil", operation, err)
		}
	}
}

func TestValidateClaimFailureOperationRejectsLeakCarriers(t *testing.T) {
	for _, operation := range []string{
		"https://hooks.example.com/deploy?token=secret",
		"Bearer abc123",
		"collect failure",
		"Collect_Failure",
		"aws_secret_access_key=wJalrXUtnFEMI",
		"op;DROP TABLE workflow_claims",
		"fetch-items",
		strings.Repeat("a", 65),
	} {
		if err := ValidateClaimFailureOperation(operation); err == nil {
			t.Errorf("ValidateClaimFailureOperation(%q) error = nil, want rejection", operation)
		} else if !strings.Contains(err.Error(), "failure_operation") {
			t.Errorf("ValidateClaimFailureOperation(%q) error = %q, want failure_operation", operation, err)
		}
	}
}

func TestClaimPlaneClientRefusesUnboundedFailureOperation(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	mutation := testMutation()
	mutation.FailureOperation = "https://hooks.example.com/deploy?token=secret"
	if err := client.HeartbeatClaim(context.Background(), mutation); err == nil {
		t.Fatal("HeartbeatClaim with URL operation succeeded, want rejection")
	} else if !strings.Contains(err.Error(), "failure_operation") {
		t.Fatalf("HeartbeatClaim err = %q, want failure_operation", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0: the hostile mutation must never reach the wire", requests)
	}
}

func TestClaimPlaneClientSendsSanitizedFailureOperation(t *testing.T) {
	var got struct {
		ClaimID          string `json:"claim_id"`
		FailureOperation string `json:"failure_operation"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Errorf("decode mutation: %v", err)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()
	client := NewClaimPlaneClient(server.URL, server.Client())

	mutation := testMutation()
	mutation.FailureClass = "auth_denied"
	mutation.FailureMessage = "provider 401: bad credentials"
	mutation.FailureOperation = "list_org_alerts"
	if err := client.FailClaimTerminal(context.Background(), mutation); err != nil {
		t.Fatalf("FailClaimTerminal: %v", err)
	}
	if got.FailureOperation != "list_org_alerts" {
		t.Fatalf("wire failure_operation = %q, want list_org_alerts", got.FailureOperation)
	}
}
