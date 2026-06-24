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

func TestDispatchToolIngesterStatusAllowsScopedRoutes(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 5, 5, 0, 0, time.UTC)
	statusHandler := &query.StatusHandler{
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     privateInstanceID,
						CollectorKind:  "git",
						Mode:           "continuous",
						DisplayName:    privateDisplayName,
						Enabled:        true,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
					ActiveClaims: 1,
				},
				QueueBlockages: []statuspkg.QueueBlockage{{
					Stage:       "reducer",
					Domain:      "repository_projection",
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	list, err := dispatchTool(
		context.Background(),
		handler,
		"list_ingesters",
		map[string]any{},
		"Bearer scoped-token",
		logger,
	)
	if err != nil {
		t.Fatalf("dispatchTool(list_ingesters) error = %v, want nil", err)
	}
	assertIngesterDispatchResultRedacted(t, list, privateInstanceID, privateDisplayName, privateConflictKey)

	detail, err := dispatchTool(
		context.Background(),
		handler,
		"get_ingester_status",
		map[string]any{"ingester": "repository"},
		"Bearer scoped-token",
		logger,
	)
	if err != nil {
		t.Fatalf("dispatchTool(get_ingester_status) error = %v, want nil", err)
	}
	assertIngesterDispatchResultRedacted(t, detail, privateInstanceID, privateDisplayName, privateConflictKey)
}

func assertIngesterDispatchResultRedacted(t *testing.T, result *dispatchResult, forbiddenValues ...string) {
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
		"conflict_key",
		"tenant-a",
		"workspace-a",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(raw, value) {
			t.Fatalf("ingester dispatch leaked %q: %s", value, raw)
		}
	}
}
