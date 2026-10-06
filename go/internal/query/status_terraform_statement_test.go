// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// terraformStatementMarkers identify the two Terraform-state evidence
// statements in the status snapshot: the last-serial read (#24) and the
// recent-warning read (#25).
var terraformStatementMarkers = []string{"WITH ranked_generations AS", "WITH raw_warning_rows AS"}

// countTerraformStatements returns how many recorded statements are
// Terraform-state evidence reads.
func countTerraformStatements(calls []fake.QueryCall) int {
	count := 0
	for _, call := range calls {
		for _, marker := range terraformStatementMarkers {
			if strings.Contains(call.Query, marker) {
				count++
			}
		}
	}
	return count
}

// serveStatusRouteOverStore runs one GET through a StatusHandler backed by the
// production Postgres StatusStore over a recording queryer. When failTerraform
// is set, the recent-warning statement fails.
func serveStatusRouteOverStore(t *testing.T, path string, failTerraform bool) (*httptest.ResponseRecorder, *fake.ExecQueryer) {
	t.Helper()
	queryer := &fake.ExecQueryer{Routes: []fake.Route{func(query string, _ []any) (*fake.Rows, bool) {
		if failTerraform && strings.Contains(query, "WITH raw_warning_rows AS") {
			return &fake.Rows{FailWith: errors.New("terraform warning read failed")}, true
		}
		return &fake.Rows{}, true
	}}}
	h := &StatusHandler{StatusReader: postgres.NewStatusStore(queryer), LiveActivity: &fakeLiveActivityReader{}}
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec, queryer
}

// TestStatusRoutesTerraformStatementInventory proves, through the production
// status store, that each Terraform-free route issues neither Terraform-state
// statement and survives a failing warning read, while the routes that render
// terraform_state issue both and still propagate that failure.
func TestStatusRoutesTerraformStatementInventory(t *testing.T) {
	t.Parallel()
	for _, route := range terraformFreeStatusRoutes {
		t.Run("skip"+route.path, func(t *testing.T) {
			t.Parallel()
			rec, queryer := serveStatusRouteOverStore(t, route.path, false)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if len(queryer.Queries) == 0 {
				t.Fatal("route issued no status statements")
			}
			if got := countTerraformStatements(queryer.Queries); got != 0 {
				t.Fatalf("Terraform statements = %d, want 0", got)
			}
			failed, _ := serveStatusRouteOverStore(t, route.path, true)
			if failed.Code != http.StatusOK {
				t.Fatalf("failing Terraform read changed status to %d: %s", failed.Code, failed.Body.String())
			}
		})
	}
	for _, route := range terraformRenderingStatusRoutes {
		t.Run("keep"+route.path, func(t *testing.T) {
			t.Parallel()
			rec, queryer := serveStatusRouteOverStore(t, route.path, false)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if got := countTerraformStatements(queryer.Queries); got != 2 {
				t.Fatalf("Terraform statements = %d, want 2", got)
			}
			failed, _ := serveStatusRouteOverStore(t, route.path, true)
			if failed.Code != http.StatusInternalServerError {
				t.Fatalf("failing Terraform read status = %d, want 500", failed.Code)
			}
		})
	}
}
