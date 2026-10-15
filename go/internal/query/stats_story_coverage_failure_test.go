// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// repositoryCoverageCanary stands in for backend error text in a failed
// content-store coverage read: a host, a relation, and a credential-shaped
// token (#7674).
const repositoryCoverageCanary = `host=db.internal relation "content_files" password=hunter2`

// repositoryCoverageFailingStore fails only the coverage read.
type repositoryCoverageFailingStore struct {
	fakePortContentStore
	err error
}

func (s repositoryCoverageFailingStore) RepositoryCoverage(context.Context, string) (RepositoryContentCoverage, error) {
	return RepositoryContentCoverage{}, s.err
}

func repositoryCoverageFailureHandler(err error) *RepositoryHandler {
	return &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": testutil.RepositoryStatsGraphRow(),
			},
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil },
		},
		Content: repositoryCoverageFailingStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
			},
			err: err,
		},
	}
}

// serveRepositoryCoverageFailure serves path on a recorded span, canceling
// the request context first when cancel is set.
func serveRepositoryCoverageFailure(t *testing.T, err error, path string, cancel bool) (*httptest.ResponseRecorder, sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("repository-coverage-failure-test").Start(context.Background(), "http")
	ctx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	if cancel {
		cancelCtx()
	}

	mux := http.NewServeMux()
	repositoryCoverageFailureHandler(err).Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	for _, leaked := range []string{"hunter2", "db.internal", "content_files", "private"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("body leaked backend error text %q: %s", leaked, rec.Body.String())
		}
	}
	return rec, ended[0]
}

// repositoryCoverageRoutes names the stats and story coverage fields that
// carry last_error.
var repositoryCoverageRoutes = []struct {
	name  string
	path  string
	field string
}{
	{"stats", "/api/v0/repositories/repo-1/stats", "coverage"},
	{"story", "/api/v0/repositories/repo-1/story", "coverage_summary"},
}

// TestRepositoryCoverageErrorAnswersFixedLastError is the #7674 Fork 1
// regression: a failed coverage read copied err.Error() into
// coverage.last_error (stats) and coverage_summary.last_error (story). The
// field is now a closed set of fixed strings, and a backend failure still
// degrades to the 200 partial with missing_evidence naming the error class.
func TestRepositoryCoverageErrorAnswersFixedLastError(t *testing.T) {
	t.Parallel()

	for _, route := range repositoryCoverageRoutes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New("private pq: " + repositoryCoverageCanary)
			rec, _ := serveRepositoryCoverageFailure(t, err, route.path, false)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			coverage := testutil.MustMapField(t, testutil.DecodeResponseBody(t, rec), route.field)
			if got, want := coverage["last_error"], "content store coverage read failed"; got != want {
				t.Fatalf("%s.last_error = %#v, want %#v", route.field, got, want)
			}
			testutil.RequireStringSlice(t, coverage, "missing_evidence", []string{"content_store_coverage_error"})
		})
	}
}

// TestRepositoryCoverageReaderFenceAnswersRetryable503 pins Fork 1 rule (i):
// a reader fence on the coverage read answers the retryable 503 with
// Retry-After before the route degrades to a 200 partial.
func TestRepositoryCoverageReaderFenceAnswersRetryable503(t *testing.T) {
	t.Parallel()

	for _, route := range repositoryCoverageRoutes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("private read store %s: %w", repositoryCoverageCanary, db.ErrReaderStale)
			rec, _ := serveRepositoryCoverageFailure(t, err, route.path, false)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Fatal("reader fence 503 is missing Retry-After")
			}
		})
	}
}

// TestRepositoryCoverageClientCancelAnswers499 pins Fork 1 rule (ii) and the
// story-route cancel observation: a client cancel during the coverage read
// answers 499 with the route's fixed message, an Unset span status, the
// client-cancel event, and no exception event.
func TestRepositoryCoverageClientCancelAnswers499(t *testing.T) {
	t.Parallel()

	messages := map[string]string{
		"stats": "repository stats content coverage read failed",
		"story": "repository story content coverage read failed",
	}
	for _, route := range repositoryCoverageRoutes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			err := fmt.Errorf("private read store %s: %w", repositoryCoverageCanary, context.Canceled)
			rec, span := serveRepositoryCoverageFailure(t, err, route.path, true)
			if rec.Code != querycontract.StatusClientClosedRequest {
				t.Fatalf("status = %d, want 499; body = %s", rec.Code, rec.Body.String())
			}
			if got := testutil.DecodeResponseBody(t, rec)["detail"]; got != messages[route.name] {
				t.Fatalf("detail = %#v, want %q", got, messages[route.name])
			}
			if span.Status().Code != codes.Unset {
				t.Fatalf("span status = %v (%q), want Unset", span.Status().Code, span.Status().Description)
			}
			canceled := false
			for _, event := range span.Events() {
				if event.Name == "exception" {
					t.Fatal("client cancel recorded an exception event")
				}
				canceled = canceled || event.Name == tracing.ClientCanceledEvent
			}
			if !canceled {
				t.Fatalf("span lacks the %s event", tracing.ClientCanceledEvent)
			}
		})
	}
}

// TestRepositoryCoverageCancelNeedsBothHalves pins the client-cancel
// conjunction on the coverage read: only a context.Canceled error on a
// canceled request is a client cancel. A store fault on a canceled request,
// and a context.Canceled from an inner context on a live request, are both
// coverage faults that degrade to the 200 partial with the fixed last_error,
// never 499.
func TestRepositoryCoverageCancelNeedsBothHalves(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		cancel bool
	}{
		{"canceled request, store fault", errors.New("private pq: " + repositoryCoverageCanary), true},
		{"live request, inner cancel", fmt.Errorf("private read store %s: %w", repositoryCoverageCanary, context.Canceled), false},
	}
	for _, route := range repositoryCoverageRoutes {
		for _, tc := range cases {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				rec, span := serveRepositoryCoverageFailure(t, tc.err, route.path, tc.cancel)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
				}
				coverage := testutil.MustMapField(t, testutil.DecodeResponseBody(t, rec), route.field)
				if got, want := coverage["last_error"], "content store coverage read failed"; got != want {
					t.Fatalf("%s.last_error = %#v, want %#v", route.field, got, want)
				}
				testutil.RequireStringSlice(t, coverage, "missing_evidence", []string{"content_store_coverage_error"})
				for _, event := range span.Events() {
					if event.Name == tracing.ClientCanceledEvent {
						t.Fatalf("span carries %s for a coverage fault", tracing.ClientCanceledEvent)
					}
				}
			})
		}
	}
}
