// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCombineReadinessProbesReturnsNilWhenAllReady(t *testing.T) {
	t.Parallel()

	check := combineReadinessProbes([]ReadinessProbe{
		{Name: "postgres", Check: func(context.Context) error { return nil }},
		{Name: "graph", Check: func(context.Context) error { return nil }},
	})

	if err := check(); err != nil {
		t.Fatalf("combineReadinessProbes() error = %v, want nil", err)
	}
}

func TestCombineReadinessProbesAggregatesCausesDeterministically(t *testing.T) {
	t.Parallel()

	check := combineReadinessProbes([]ReadinessProbe{
		{Name: "postgres", Check: func(context.Context) error { return errors.New("pool exhausted") }},
		{Name: "graph", Check: func(context.Context) error { return errors.New("bolt refused") }},
		{Name: "status_snapshot", Check: func(context.Context) error { return nil }},
	})

	err := check()
	if err == nil {
		t.Fatal("combineReadinessProbes() error = nil, want aggregated failure")
	}
	// Causes are sorted, so graph (g) precedes postgres (p) regardless of
	// goroutine completion order.
	want := "graph: bolt refused; postgres: pool exhausted"
	if got := err.Error(); got != want {
		t.Fatalf("combineReadinessProbes() error = %q, want %q", got, want)
	}
}

func TestCombineReadinessProbesEmptyIsReady(t *testing.T) {
	t.Parallel()

	if err := combineReadinessProbes(nil)(); err != nil {
		t.Fatalf("combineReadinessProbes(nil) error = %v, want nil", err)
	}
}

func TestRunReadinessProbeBoundsSlowDependency(t *testing.T) {
	t.Parallel()

	started := time.Now()
	cause := runReadinessProbe(ReadinessProbe{
		Name:    "graph",
		Timeout: 50 * time.Millisecond,
		Check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	elapsed := time.Since(started)

	if cause == "" {
		t.Fatal("runReadinessProbe() cause = empty, want bounded timeout failure")
	}
	if !strings.HasPrefix(cause, "graph: ") {
		t.Fatalf("runReadinessProbe() cause = %q, want graph-prefixed cause", cause)
	}
	if elapsed > time.Second {
		t.Fatalf("runReadinessProbe() elapsed = %s, want bounded near probe timeout", elapsed)
	}
}

func TestRunReadinessProbeRecoversFromPanic(t *testing.T) {
	t.Parallel()

	cause := runReadinessProbe(ReadinessProbe{
		Name:  "graph",
		Check: func(context.Context) error { panic("boom") },
	})
	if !strings.Contains(cause, "panic") {
		t.Fatalf("runReadinessProbe() cause = %q, want panic cause", cause)
	}
}

func TestRunReadinessProbeMissingCheckFails(t *testing.T) {
	t.Parallel()

	if cause := runReadinessProbe(ReadinessProbe{Name: "postgres"}); cause == "" {
		t.Fatal("runReadinessProbe() with nil Check should report a failure cause")
	}
}

func TestPostgresReadinessProbeNilHandleFails(t *testing.T) {
	t.Parallel()

	probe := PostgresReadinessProbe(nil, time.Second)
	if probe.Name != "postgres" {
		t.Fatalf("PostgresReadinessProbe Name = %q, want postgres", probe.Name)
	}
	if err := probe.Check(context.Background()); err == nil {
		t.Fatal("PostgresReadinessProbe with nil handle should fail")
	}
}

func TestPostgresReadinessProbeReportsPingError(t *testing.T) {
	t.Parallel()

	// A pool pointed at an unroutable DSN fails PingContext under a bounded
	// timeout, proving connectivity failures surface as a readiness cause.
	db, err := sql.Open("pgx", "postgres://127.0.0.1:1/eshu?connect_timeout=1")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	probe := PostgresReadinessProbe(db, 500*time.Millisecond)
	if err := probe.Check(context.Background()); err == nil {
		t.Fatal("PostgresReadinessProbe should fail against an unreachable database")
	}
}

func TestGraphReadinessProbeNilDriverIsReady(t *testing.T) {
	t.Parallel()

	probe := GraphReadinessProbe(nil, time.Second)
	if probe.Name != "graph" {
		t.Fatalf("GraphReadinessProbe Name = %q, want graph", probe.Name)
	}
	// A disabled graph (local lightweight profile) must not gate readiness.
	if err := probe.Check(context.Background()); err != nil {
		t.Fatalf("GraphReadinessProbe with nil driver error = %v, want nil", err)
	}
}

func TestNewStatusAdminMuxReadyzReflectsDependencyFailure(t *testing.T) {
	t.Parallel()

	mux, err := NewStatusAdminMux(
		"eshu-api",
		&fakeStatusReader{},
		nil,
		WithReadinessProbes(ReadinessProbe{
			Name:  "graph",
			Check: func(context.Context) error { return errors.New("bolt connection refused") },
		}),
	)
	if err != nil {
		t.Fatalf("NewStatusAdminMux() error = %v", err)
	}

	// Readiness reflects the failing graph dependency with a cause body.
	readyRec := httptest.NewRecorder()
	mux.ServeHTTP(readyRec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if got, want := readyRec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}
	if body := readyRec.Body.String(); !strings.Contains(body, "graph") ||
		!strings.Contains(body, "bolt connection refused") {
		t.Fatalf("GET /readyz body = %q, want graph cause", body)
	}

	// Liveness stays dependency-free and healthy even while readiness fails.
	liveRec := httptest.NewRecorder()
	mux.ServeHTTP(liveRec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got, want := liveRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}
}

func TestNewStatusAdminMuxReadyzHealthyWithDependencyProbes(t *testing.T) {
	t.Parallel()

	mux, err := NewStatusAdminMux(
		"eshu-api",
		&fakeStatusReader{},
		nil,
		WithReadinessProbes(
			ReadinessProbe{Name: "graph", Check: func(context.Context) error { return nil }},
		),
	)
	if err != nil {
		t.Fatalf("NewStatusAdminMux() error = %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /readyz status = %d, want %d", got, want)
	}
}
