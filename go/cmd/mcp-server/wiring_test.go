package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestWireAPIReturnsResolveAPIKeyErrorBeforeConnectingDatastores(t *testing.T) {
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "true")
	t.Setenv("ESHU_HOME", "/dev/null/eshu")

	_, _, _, err := wireAPI(context.Background(), func(string) string {
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
}

func TestWireAPIReturnsInvalidQueryProfileErrorBeforeConnectingDatastores(t *testing.T) {
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
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
	_, _, _, err := wireAPI(context.Background(), func(key string) string {
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

func TestNewMCPQueryRouterMountsMCPBackedHandlers(t *testing.T) {
	t.Parallel()

	router := newMCPQueryRouter(
		nil,
		nil,
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
	)

	if router.IaC == nil {
		t.Fatal("newMCPQueryRouter().IaC = nil, want MCP find_dead_iac route mounted")
	}
	if router.IaC.Reachability == nil {
		t.Fatal("newMCPQueryRouter().IaC.Reachability = nil, want materialized reachability store")
	}
	if router.Evidence == nil {
		t.Fatal("newMCPQueryRouter().Evidence = nil, want relationship evidence drilldown route mounted")
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
	}, query.ProfileLocalLightweight, nil)
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
	if got != query.GraphBackendNornicDB {
		t.Fatalf("loadGraphBackend() = %q, want %q", got, query.GraphBackendNornicDB)
	}
}
