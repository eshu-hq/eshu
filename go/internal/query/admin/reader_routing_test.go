// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

type inspectionQueries struct {
	calls int
	err   error
}

func (q *inspectionQueries) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	q.calls++
	if q.err != nil {
		return nil, q.err
	}
	return &fake.Rows{}, nil
}

type mutationStore struct {
	admin.Store
	reads  int
	writes int
}

func (s *mutationStore) ListWorkItems(context.Context, admin.WorkItemFilter) ([]admin.WorkItem, error) {
	s.reads++
	return nil, nil
}

func (s *mutationStore) RequestBackfill(context.Context, admin.BackfillInput) (*admin.BackfillRequest, error) {
	s.writes++
	return &admin.BackfillRequest{BackfillRequestID: "request", CreatedAt: time.Unix(0, 0)}, nil
}

func TestAdminInspectionsUseReaderAndMutationsUseWriter(t *testing.T) {
	t.Parallel()
	queries := &inspectionQueries{}
	writer := &mutationStore{}
	handler := &admin.Handler{ReadStore: store.NewReadStore(queries), Store: writer}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := func(path, body string) int {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return response.Code
	}
	if code := request("/api/v0/admin/work-items/query", "{}"); code != http.StatusOK {
		t.Fatalf("read HTTP %d", code)
	}
	if queries.calls != 1 || writer.reads != 0 {
		t.Fatalf("read calls: reader=%d writer=%d", queries.calls, writer.reads)
	}
	if code := request("/api/v0/admin/backfill", `{"scope_id":"scope"}`); code != http.StatusOK {
		t.Fatalf("mutation HTTP %d", code)
	}
	if queries.calls != 1 || writer.writes != 1 {
		t.Fatalf("mutation calls: reader=%d writer=%d", queries.calls, writer.writes)
	}
	queries.err = errors.New("reader unavailable")
	if code := request("/api/v0/admin/work-items/query", "{}"); code != http.StatusInternalServerError {
		t.Fatalf("failed read HTTP %d", code)
	}
	if writer.reads != 0 {
		t.Fatal("failed read fell back to writer")
	}
}

func TestAdminLegacyStoreConstructionPreservesInspection(t *testing.T) {
	t.Parallel()
	writer := &mutationStore{}
	mux := http.NewServeMux()
	(&admin.Handler{Store: writer}).Mount(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v0/admin/work-items/query", strings.NewReader("{}")))
	if response.Code != http.StatusOK || writer.reads != 1 {
		t.Fatalf("legacy read: HTTP %d calls %d", response.Code, writer.reads)
	}
}
