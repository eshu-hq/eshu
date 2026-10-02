// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type terraformOnlyFailureQueryer struct {
	queries   []string
	fail      bool
	failOther bool
}

func (q *terraformOnlyFailureQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if q.failOther {
		return nil, errors.New("required status read unavailable")
	}
	if q.fail && strings.Contains(query, "WITH ranked_generations AS") {
		return nil, errors.New("terraform evidence unavailable")
	}
	return emptyStatusRows{}, nil
}

type emptyStatusRows struct{}

func (emptyStatusRows) Next() bool        { return false }
func (emptyStatusRows) Scan(...any) error { return errors.New("unexpected scan") }
func (emptyStatusRows) Err() error        { return nil }
func (emptyStatusRows) Close() error      { return nil }

func TestRepositoryDetailOmitsUnusedTerraformEvidenceRead(t *testing.T) {
	for _, route := range []string{
		"/api/v0/status/ingesters/repository",
		"/api/v0/ingesters/repository",
	} {
		t.Run(route, func(t *testing.T) {
			q := &terraformOnlyFailureQueryer{fail: true}
			h := &StatusHandler{StatusReader: storagepostgres.NewStatusStore(q)}
			mux := http.NewServeMux()
			h.Mount(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("detail HTTP status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			for _, query := range q.queries {
				if strings.Contains(query, "WITH ranked_generations AS") {
					t.Fatal("detail issued unused Terraform state evidence query")
				}
			}
			for _, field := range []string{`"health"`, `"queue"`, `"coordinator"`, `"scope_activity"`, `"stage_summaries"`, `"domain_backlogs"`} {
				if !strings.Contains(rec.Body.String(), field) {
					t.Fatalf("detail response missing %s: %s", field, rec.Body.String())
				}
			}
		})
	}
}

func TestFullAndIndexStatusKeepTerraformEvidenceFailures(t *testing.T) {
	for _, route := range []string{"/api/v0/status/pipeline", "/api/v0/status/index", "/api/v0/index-status"} {
		t.Run(route, func(t *testing.T) {
			q := &terraformOnlyFailureQueryer{fail: true}
			h := &StatusHandler{StatusReader: storagepostgres.NewStatusStore(q)}
			mux := http.NewServeMux()
			h.Mount(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 from required Terraform evidence: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRepositoryDetailPreservesRequiredReadFailure(t *testing.T) {
	q := &terraformOnlyFailureQueryer{failOther: true}
	h := &StatusHandler{StatusReader: storagepostgres.NewStatusStore(q)}
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/status/ingesters/repository", nil))
	if rec.Code != http.StatusInternalServerError || len(q.queries) != 1 {
		t.Fatalf("required-read failure: status=%d queries=%d body=%s", rec.Code, len(q.queries), rec.Body.String())
	}
}
