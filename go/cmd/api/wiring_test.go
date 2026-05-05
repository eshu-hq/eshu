package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWireAPIReturnsResolveAPIKeyErrorBeforeConnectingDatastores(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "true")
	t.Setenv("ESHU_HOME", "/dev/null/eshu")

	_, _, err := wireAPI(context.Background(), func(key string) string {
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
}

func TestWireAPIReturnsInvalidQueryProfileErrorBeforeConnectingDatastores(t *testing.T) {
	_, _, err := wireAPI(context.Background(), func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "not-a-real-profile"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load query profile") {
		t.Fatalf("wireAPI() error = %q, want load query profile context", err)
	}
}

func TestWireAPIReturnsInvalidGraphBackendErrorBeforeConnectingDatastores(t *testing.T) {
	_, _, err := wireAPI(context.Background(), func(key string) string {
		if key == "ESHU_GRAPH_BACKEND" {
			return "not-a-real-backend"
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load graph backend") {
		t.Fatalf("wireAPI() error = %q, want load graph backend context", err)
	}
}

func TestOpenQueryGraphAcceptsNornicDBOnSharedBoltPath(t *testing.T) {
	t.Parallel()

	_, _, err := openQueryGraph(context.Background(), func(key string) string {
		switch key {
		case "ESHU_GRAPH_BACKEND":
			return "nornicdb"
		case "ESHU_QUERY_PROFILE":
			return "production"
		default:
			return ""
		}
	}, "production", nil)
	if err == nil {
		t.Fatal("openQueryGraph() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_URI") && !strings.Contains(err.Error(), "NEO4J_URI") {
		t.Fatalf("openQueryGraph() error = %q, want shared bolt config context", err)
	}
}

func TestOpenQueryGraphDefaultsLocalLightweightDatabaseToNornic(t *testing.T) {
	t.Parallel()

	driver, databaseName, err := openQueryGraph(context.Background(), func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "local_lightweight"
		}
		return ""
	}, "local_lightweight", nil)
	if err != nil {
		t.Fatalf("openQueryGraph() error = %v, want nil", err)
	}
	if driver != nil {
		t.Fatal("openQueryGraph() driver != nil, want nil for local_lightweight")
	}
	if databaseName != "nornic" {
		t.Fatalf("openQueryGraph() database = %q, want nornic", databaseName)
	}
}

func TestLoadGraphBackendDefaultsToNornicDB(t *testing.T) {
	t.Parallel()

	got, err := loadGraphBackend(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadGraphBackend() error = %v, want nil", err)
	}
	if got != "nornicdb" {
		t.Fatalf("loadGraphBackend() = %q, want nornicdb", got)
	}
}

func TestNewRouter_MountsAdminRoutes(t *testing.T) {
	t.Parallel()

	router, err := newRouter(nil, nil, nil, "production", "neo4j", nil)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}

	mux := http.NewServeMux()
	router.Mount(mux)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "reindex is mounted",
			method:     http.MethodPost,
			path:       "/api/v0/admin/reindex",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "work item query is mounted",
			method:     http.MethodPost,
			path:       "/api/v0/admin/work-items/query",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "tuning report is mounted",
			method:     http.MethodGet,
			path:       "/api/v0/admin/shared-projection/tuning-report",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, tt.wantStatus; got != want {
				t.Fatalf("%s %s status = %d, want %d; body: %s", tt.method, tt.path, got, want, rec.Body.String())
			}
		})
	}
}
