// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClaimPlaneErrorCodeReportsPinEnvelopeCode proves a caller outside the
// package can branch on the protocol code of a PinClaimForCommit refusal (the
// #9376 commit fence classifies stale_fence and not_found from it) and that a
// transport failure reports no code.
func TestClaimPlaneErrorCodeReportsPinEnvelopeCode(t *testing.T) {
	for _, code := range []string{"stale_fence", "not_found", "internal"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(writer).Encode(map[string]any{"code": code, "message": "pin refused"})
			}))
			defer server.Close()
			client := NewClaimPlaneClient(server.URL, server.Client())
			_, err := client.PinClaimForCommit(context.Background(), "claim-1", 2, "scope-1", "run-1", "gen-1",
				time.Now().Add(time.Minute))
			if err == nil {
				t.Fatal("PinClaimForCommit() error = nil, want refusal")
			}
			wrapped := fmt.Errorf("commit: %w", err)
			if got := ClaimPlaneErrorCode(wrapped); got != code {
				t.Fatalf("ClaimPlaneErrorCode() = %q, want %q", got, code)
			}
		})
	}
	if got := ClaimPlaneErrorCode(errors.New("dial tcp: refused")); got != "" {
		t.Fatalf("ClaimPlaneErrorCode(transport) = %q, want empty", got)
	}
	if got := ClaimPlaneErrorCode(nil); got != "" {
		t.Fatalf("ClaimPlaneErrorCode(nil) = %q, want empty", got)
	}
}

// TestClaimMutationCarriesPlannedIdentity pins the planned item identity
// fields the claim-plane pin needs (#9376 D8). They are distinct from the
// Resolved* fields terraform-state uses for completion.
func TestClaimMutationCarriesPlannedIdentity(t *testing.T) {
	mutation := ClaimMutation{
		ScopeID:              "scope-planned",
		SourceRunID:          "run-planned",
		GenerationID:         "gen-planned",
		ResolvedGenerationID: "gen-resolved",
	}
	if mutation.ScopeID != "scope-planned" || mutation.SourceRunID != "run-planned" ||
		mutation.GenerationID != "gen-planned" || mutation.ResolvedGenerationID != "gen-resolved" {
		t.Fatalf("mutation identity = %+v", mutation)
	}
}
