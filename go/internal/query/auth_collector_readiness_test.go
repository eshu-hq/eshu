// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestAuthMiddlewareWithScopedTokensAllowsCollectorReadinessRoute proves a
// scoped token can reach the collector readiness route (and its alias) so the
// handler's per-instance redaction actually applies instead of the request being
// rejected before the handler runs.
func TestAuthMiddlewareWithScopedTokensAllowsCollectorReadinessRoute(t *testing.T) {
	t.Parallel()

	const privateInstanceID = "collector-private-tenant"
	now := time.Date(2026, 6, 16, 6, 15, 0, 0, time.UTC)

	for _, path := range []string{"/api/v0/status/collector-readiness", "/api/v0/collector-readiness"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			statusHandler := &StatusHandler{
				StatusReader: fakeStatusReader{
					snapshot: statuspkg.RawSnapshot{
						AsOf: now,
						Coordinator: &statuspkg.CoordinatorSnapshot{
							CollectorInstances: []statuspkg.CollectorInstanceSummary{{
								InstanceID:     privateInstanceID,
								CollectorKind:  "aws",
								Enabled:        true,
								ClaimsEnabled:  true,
								LastObservedAt: now,
								UpdatedAt:      now,
							}},
						},
					},
				},
				Profile: ProfileProduction,
			}
			mux := http.NewServeMux()
			statusHandler.Mount(mux)
			resolver := &fakeScopedTokenResolver{
				context: AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
				ok:      true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("scoped %s status = %d, want %d; body=%s", path, got, want, rec.Body.String())
			}
			// Scoped readiness must redact the per-instance identity.
			if strings.Contains(rec.Body.String(), privateInstanceID) {
				t.Fatalf("scoped collector readiness leaked instance id: %s", rec.Body.String())
			}
		})
	}
}
