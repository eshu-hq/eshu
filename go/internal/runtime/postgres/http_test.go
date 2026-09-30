// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

type checkpointStub struct {
	calls int
	err   error
}

func (s *checkpointStub) ContextWithCheckpoint(ctx context.Context) (context.Context, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return context.WithValue(ctx, checkpointKey{}, "proof"), nil
}

func TestCheckpointHandlerSelectedDispatch(t *testing.T) {
	source := &checkpointStub{}
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/business" && r.Context().Value(checkpointKey{}) != "proof" {
			t.Error("business dispatch lacks checkpoint")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := WithCheckpoint(next, source, func(r *http.Request) bool { return r.URL.Path == "/business" })
	for _, path := range []string{"/pure", "/business"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("response %d", response.Code)
		}
	}
	if source.calls != 1 || calls != 2 {
		t.Fatalf("checkpoint=%d dispatch=%d", source.calls, calls)
	}
	source.err = errors.New("private DSN secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/business", nil))
	if response.Code != http.StatusServiceUnavailable || calls != 2 || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("failure: %d %q dispatch=%d", response.Code, response.Body, calls)
	}
}

func TestCheckpointHandlerCanceledAndMissingSource(t *testing.T) {
	for _, missing := range []bool{false, true} {
		source := &checkpointStub{}
		var checkpointSource CheckpointSource = source
		if missing {
			checkpointSource = nil
		}
		called := false
		handler := WithCheckpoint(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), checkpointSource, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/business", nil).WithContext(ctx))
		if response.Code != http.StatusServiceUnavailable || called {
			t.Fatal("failed checkpoint dispatched")
		}
	}
}

type guardedStatusStub struct {
	calls     int
	selection status.SnapshotSelection
}

func (s *guardedStatusStub) ReadStatusSnapshot(ctx context.Context, now time.Time) (status.RawSnapshot, error) {
	return s.ReadStatusSnapshotFiltered(ctx, now, status.FullSnapshotSelection())
}

func (s *guardedStatusStub) ReadStatusSnapshotFiltered(ctx context.Context, now time.Time, selection status.SnapshotSelection) (status.RawSnapshot, error) {
	if ctx.Value(checkpointKey{}) != "proof" {
		return status.RawSnapshot{}, ErrMissingCheckpoint
	}
	s.calls++
	s.selection = selection
	return status.RawSnapshot{AsOf: now}, nil
}

func (s *guardedStatusStub) CheckStatusReadiness(ctx context.Context) error {
	if ctx.Value(checkpointKey{}) != "proof" {
		return ErrMissingCheckpoint
	}
	s.calls++
	return nil
}

func TestTrustedStatusReaderCapturesWithinEveryMethod(t *testing.T) {
	source := &checkpointStub{}
	inner := &guardedStatusStub{}
	reader := NewTrustedStatusReader(inner, source)
	ctx := context.Background()
	now := time.Now()
	if _, err := reader.ReadStatusSnapshot(ctx, now); err != nil {
		t.Fatal(err)
	}
	selection := status.SnapshotSelection{IncludeRegistryCollectors: true}
	if _, err := reader.ReadStatusSnapshotFiltered(ctx, now, selection); err != nil {
		t.Fatal(err)
	}
	if inner.selection != selection {
		t.Fatal("selection changed")
	}
	checker, ok := reader.(status.ReadinessChecker)
	if !ok {
		t.Fatal("readiness contract missing")
	}
	if err := checker.CheckStatusReadiness(ctx); err != nil {
		t.Fatal(err)
	}
	if source.calls != 3 || inner.calls != 3 {
		t.Fatalf("checkpoint=%d business=%d", source.calls, inner.calls)
	}
	source.err = ErrWrongTopology
	if err := checker.CheckStatusReadiness(ctx); !errors.Is(err, ErrWrongTopology) || inner.calls != 3 {
		t.Fatal("topology failure escaped")
	}
	if _, err := reader.ReadStatusSnapshotFiltered(ctx, now, selection); !errors.Is(err, ErrWrongTopology) || inner.calls != 3 {
		t.Fatal("snapshot failure escaped")
	}
}

func TestFutureAuthBusinessRouteReplacesInheritedCheckpoint(t *testing.T) {
	source := &checkpointStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/auth/future/business", func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(checkpointKey{}) != "proof" {
			t.Error("inherited checkpoint was reused")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	dispatch := WithCheckpoint(mux, source, func(r *http.Request) bool { return RequiresCheckpoint(mux, r, false) })
	// Authentication precedes capture: denied callers cannot renew a checkpoint.
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer proof" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		dispatch.ServeHTTP(w, r)
	})
	inherited := context.WithValue(context.Background(), checkpointKey{}, "old")
	for _, authorized := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/api/v0/auth/future/business", nil).WithContext(inherited)
		if authorized {
			request.Header.Set("Authorization", "Bearer proof")
		}
		response := httptest.NewRecorder()
		authed.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if authorized {
			want = http.StatusNoContent
		}
		if response.Code != want {
			t.Fatalf("response=%d want=%d", response.Code, want)
		}
	}
	if source.calls != 1 {
		t.Fatalf("checkpoint calls=%d want=1", source.calls)
	}
}
