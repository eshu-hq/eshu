// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestInvestigateServiceRequiresServiceName pins the investigations route to
// its missing-selector contract. It lives in the handler's own directory so
// verify-route-coverage.sh sees a Test*InvestigateService* test beside the
// Mount registration; the graph-error sweep stays in root beside the shared
// sweep helpers it drives.
func TestInvestigateServiceRequiresServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/", nil)
	rec := httptest.NewRecorder()

	handler.InvestigateService(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
}
