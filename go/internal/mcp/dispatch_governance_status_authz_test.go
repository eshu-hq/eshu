package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type fakeMCPStatusReader struct {
	snapshot statuspkg.RawSnapshot
}

func (f fakeMCPStatusReader) ReadStatusSnapshot(context.Context, time.Time) (statuspkg.RawSnapshot, error) {
	return f.snapshot, nil
}

func (f fakeMCPStatusReader) ReadStatusSnapshotFiltered(
	context.Context,
	time.Time,
	statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	return f.snapshot, nil
}

func TestDispatchToolGovernanceStatusAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	statusHandler := &query.StatusHandler{
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 10, 4, 5, 0, 0, time.UTC),
			},
		},
		Profile: query.ProfileProduction,
		Governance: query.GovernanceStatusConfig{
			Mode:          "hosted_multi_tenant",
			State:         "enforcing",
			AuthMode:      "scoped_token",
			TenantMode:    "multi_tenant",
			WorkspaceMode: "multi_workspace",
			EgressMode:    "restricted",
		},
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_hosted_governance_status",
		map[string]any{},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want governance status envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "hosted_governance.status" {
		t.Fatalf("truth = %#v, want governance status truth", result.Envelope.Truth)
	}
}

func TestDispatchToolSemanticExtractionStatusAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	statusHandler := &query.StatusHandler{
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 10, 4, 35, 0, 0, time.UTC),
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					State:              statuspkg.SemanticExtractionAvailable,
					ProviderConfigured: true,
				},
			},
		},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &mcpScopedTokenResolver{
		auth: query.AuthContext{
			Mode:        query.AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := query.AuthMiddlewareWithScopedTokens("", resolver, mux)

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_semantic_capability_status",
		map[string]any{},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want semantic extraction status envelope")
	}
	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "semantic_extraction.status" {
		t.Fatalf("truth = %#v, want semantic extraction status truth", result.Envelope.Truth)
	}
}
