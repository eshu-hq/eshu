// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestAuthMiddlewareWithScopedTokensAllowsHostedReadinessRoute(t *testing.T) {
	t.Parallel()

	const (
		privateInstanceID  = "collector-private-tenant"
		privateDisplayName = "private tenant collector"
		privateConflictKey = "repo://private/repository"
	)
	now := time.Date(2026, 6, 10, 6, 15, 0, 0, time.UTC)
	statusHandler := &StatusHandler{
		Neo4j: hostedReadinessGraph{repositoryCount: 2},
		StatusReader: fakeStatusReader{
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
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	statusHandler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:        AuthModeScoped,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	assertHostedReadinessResponseRedacted(
		t,
		rec.Body.String(),
		privateInstanceID,
		privateDisplayName,
		privateConflictKey,
	)

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	coordinator, ok := payload["coordinator"].(map[string]any)
	if !ok {
		t.Fatalf("coordinator = %#v, want object", payload["coordinator"])
	}
	if got, want := coordinator["collector_instance_count"], float64(1); got != want {
		t.Fatalf("collector_instance_count = %#v, want %#v", got, want)
	}
	if _, leaked := coordinator["collector_instances"]; leaked {
		t.Fatalf("coordinator exposed collector_instances: %#v", coordinator)
	}
}

func assertHostedReadinessResponseRedacted(t *testing.T, body string, forbiddenValues ...string) {
	t.Helper()

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
		if strings.Contains(body, value) {
			t.Fatalf("hosted readiness leaked %q: %s", value, body)
		}
	}
}
