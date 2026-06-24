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

func TestDispatchToolCollectorStatusAllowsScopedRoute(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateSource      = "source-system-private"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 5, 35, 0, 0, time.UTC)
	statusHandler := &query.StatusHandler{
		StatusReader: fakeMCPStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
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
				},
				CollectorFactEvidence: []statuspkg.CollectorFactEvidence{{
					InstanceID:       privateInstanceID,
					CollectorKind:    "aws",
					EvidenceSource:   "collector_fact",
					SourceSystems:    []string{privateSource},
					ObservationCount: 7,
					LastObservedAt:   now,
					UpdatedAt:        now,
				}},
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
		"list_collectors",
		map[string]any{},
		"Bearer scoped-token",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(list_collectors) error = %v, want nil", err)
	}
	assertCollectorDispatchResultRedacted(t, result, privateInstanceID, privateDisplayName, privateSource, privateConflictKey)
}

func assertCollectorDispatchResultRedacted(t *testing.T, result *dispatchResult, forbiddenValues ...string) {
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
		"instance_id",
		"display_name",
		"source_systems",
		"detail",
		"conflict_key",
		"tenant-a",
		"workspace-a",
	}, forbiddenValues...)
	for _, value := range forbidden {
		if value == "" {
			continue
		}
		if strings.Contains(raw, value) {
			t.Fatalf("collector dispatch leaked %q: %s", value, raw)
		}
	}
}
