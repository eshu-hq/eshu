// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestDispatchToolHostedReadinessAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 6, 20, 0, 0, time.UTC)
	statusHandler := &query.StatusHandler{
		Neo4j: hostedReadinessMCPGraph{repositoryCount: 2},
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf:          now,
				ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     privateInstanceID,
						CollectorKind:  "aws",
						Mode:           "continuous",
						DisplayName:    privateDisplayName,
						Enabled:        true,
						ClaimsEnabled:  true,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
					CompletenessCounts: []statuspkg.NamedCount{{Name: "completed", Count: 1}},
				},
				QueueBlockages: []statuspkg.QueueBlockage{{
					Stage:       "collector",
					Domain:      "hosted_collection",
					ConflictKey: privateConflictKey,
					Blocked:     1,
				}},
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
		"get_hosted_readiness",
		map[string]any{},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(get_hosted_readiness) error = %v, want nil", err)
	}
	assertHostedReadinessDispatchResultRedacted(
		t,
		result,
		privateInstanceID,
		privateDisplayName,
		privateConflictKey,
	)
}

type hostedReadinessMCPGraph struct {
	repositoryCount int
}

func (g hostedReadinessMCPGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g hostedReadinessMCPGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"count": g.repositoryCount}, nil
}

func assertHostedReadinessDispatchResultRedacted(t *testing.T, result *dispatchResult, forbiddenValues ...string) {
	t.Helper()

	if result.IsError {
		t.Fatalf("dispatchTool() IsError = true, want false; envelope = %#v", result.Envelope)
	}
	rawBytes, err := json.Marshal(result.Value)
	if err != nil {
		t.Fatalf("json.Marshal(result.Value) error = %v, want nil", err)
	}
	raw := string(rawBytes)
	forbidden := append([]string{
		"collector_instances",
		"instance_id",
		"display_name",
		"conflict_key",
		"tenant-a",
		"workspace-a",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(raw, value) {
			t.Fatalf("hosted readiness dispatch leaked %q: %s", value, raw)
		}
	}
}
