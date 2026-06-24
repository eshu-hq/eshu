// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// openAICompatOKBody is a minimal valid chat-completions response used by the
// auth-header tests, which only care about request headers.
const openAICompatOKBody = `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{}}`

// anthropicOKBody is a minimal valid messages response for the auth-header tests.
const anthropicOKBody = `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

func TestOpenAICompatOmitsAuthHeaderWhenCredentialEmpty(t *testing.T) {
	t.Parallel()
	var authPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, authPresent = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAICompatOKBody))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "", "model", srv.Client())
	if _, err := adapter.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil); err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if authPresent {
		t.Fatal("Authorization header must be absent when the credential is empty (workload identity)")
	}
}

func TestOpenAICompatSendsBearerWhenCredentialPresent(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAICompatOKBody))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "k", "model", srv.Client())
	if _, err := adapter.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil); err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer k")
	}
}

func TestAnthropicOmitsAPIKeyHeaderWhenCredentialEmpty(t *testing.T) {
	t.Parallel()
	var keyPresent, versionPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, keyPresent = r.Header["X-Api-Key"]
		_, versionPresent = r.Header["Anthropic-Version"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicOKBody))
	}))
	defer srv.Close()

	adapter := newAnthropicAdapter(srv.URL, "", "model", srv.Client())
	if _, err := adapter.Complete(context.Background(), []Message{{Role: RoleUser, Text: "hi"}}, nil); err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if keyPresent {
		t.Fatal("x-api-key header must be absent when the credential is empty (workload identity)")
	}
	if !versionPresent {
		t.Fatal("anthropic-version header must still be present")
	}
}
