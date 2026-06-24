// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDispatchToolIncidentContextAllowsScopedRoute proves the incident-context
// MCP tool dispatches through the shared scoped route gate with the AuthContext
// attached, so a scoped hosted token reaches the handler's durable-edge
// authorization rather than being denied at the gate (#2144).
func TestDispatchToolIncidentContextAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	authorizer := &allowingIncidentRepositoryAuthorizer{repositories: []string{"repo-team-a"}}
	mux := http.NewServeMux()
	handler := &query.IncidentHandler{
		Context:    &fakeIncidentContextStore{},
		Authorizer: authorizer,
		Profile:    query.ProfileProduction,
	}
	handler.Mount(mux)

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:                 query.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	authedHandler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		authedHandler,
		"get_incident_context",
		map[string]any{
			"provider":             "pagerduty",
			"provider_incident_id": "PABC123",
			"limit":                float64(5),
		},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope == nil || result.Envelope.Truth == nil {
		t.Fatalf("envelope = %#v, want incident-context truth envelope", result.Envelope)
	}
	if !authorizer.called {
		t.Fatal("durable-edge authorizer not consulted for scoped incident-context tool")
	}
}

// TestDispatchToolIncidentContextScopedOutOfGrantIsNotFound proves a scoped
// token whose grant does not include the incident's durable owning repository
// receives a fail-closed not-found from the handler, with no incident-context
// payload served (#2144).
func TestDispatchToolIncidentContextScopedOutOfGrantIsNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeIncidentContextStore{}
	authorizer := &allowingIncidentRepositoryAuthorizer{repositories: []string{"repo-owner-x"}}
	mux := http.NewServeMux()
	handler := &query.IncidentHandler{
		Context:    store,
		Authorizer: authorizer,
		Profile:    query.ProfileProduction,
	}
	handler.Mount(mux)

	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:                 query.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	authedHandler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		authedHandler,
		"get_incident_context",
		map[string]any{
			"provider":             "pagerduty",
			"provider_incident_id": "PABC123",
			"limit":                float64(5),
		},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if !result.IsError {
		t.Fatalf("dispatchTool() IsError = false, want true for out-of-grant incident; envelope = %#v", result.Envelope)
	}
	if store.filter.ProviderIncidentID != "" {
		t.Fatal("incident-context store read for out-of-grant scoped token")
	}
}

type allowingIncidentRepositoryAuthorizer struct {
	repositories []string
	called       bool
}

func (a *allowingIncidentRepositoryAuthorizer) ResolveDurableIncidentRepositories(
	context.Context,
	string,
	string,
	string,
) ([]string, error) {
	a.called = true
	return a.repositories, nil
}
