package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
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

func TestWireAPIReturnsInvalidSemanticProviderProfilesBeforeConnectingDatastores(t *testing.T) {
	_, _, err := wireAPI(context.Background(), func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return `[{"profile_id":"semantic-docs-default","provider_kind":"deepseek","credential_source":{"kind":"environment_variable","handle":"sk-live-123"},"model_id":"deepseek-chat","source_classes":["documentation"],"source_policy_configured":true}]`
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load semantic provider profiles") {
		t.Fatalf("wireAPI() error = %q, want semantic provider profile context", err)
	}
}

func TestWireAPIReturnsInvalidSemanticPolicyBeforeConnectingDatastores(t *testing.T) {
	_, _, err := wireAPI(context.Background(), func(key string) string {
		if key == semanticpolicy.EnvPolicyJSON {
			return `{"enabled":true,"rules":[]}`
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "load semantic extraction policy") {
		t.Fatalf("wireAPI() error = %q, want semantic extraction policy context", err)
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

func TestNewRouterMountsPostgresBackedHandlers(t *testing.T) {
	t.Parallel()

	router, err := newRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	if router.SupplyChain == nil {
		t.Fatal("newRouter().SupplyChain = nil, want supply-chain route mounted")
	}
	if router.SupplyChain.ContainerImageIdentities == nil {
		t.Fatal("newRouter().SupplyChain.ContainerImageIdentities = nil, want Postgres read model store")
	}
	if router.SupplyChain.AdvisoryEvidence == nil {
		t.Fatal("newRouter().SupplyChain.AdvisoryEvidence = nil, want Postgres read model store")
	}
	if router.SupplyChain.ImpactFindings == nil {
		t.Fatal("newRouter().SupplyChain.ImpactFindings = nil, want Postgres read model store")
	}
	if router.SupplyChain.ImpactExplanations == nil {
		t.Fatal("newRouter().SupplyChain.ImpactExplanations = nil, want Postgres explain store")
	}
	if router.SupplyChain.Readiness == nil {
		t.Fatal("newRouter().SupplyChain.Readiness = nil, want Postgres readiness store")
	}
	if router.SupplyChain.SecurityAlerts == nil {
		t.Fatal("newRouter().SupplyChain.SecurityAlerts = nil, want Postgres security alert reconciliation store")
	}
	if router.Evidence == nil {
		t.Fatal("newRouter().Evidence = nil, want evidence routes mounted")
	}
	if router.Evidence.AdmissionDecisions == nil {
		t.Fatal("newRouter().Evidence.AdmissionDecisions = nil, want Postgres admission decision read model store")
	}
	if router.ServiceCatalog == nil {
		t.Fatal("newRouter().ServiceCatalog = nil, want service catalog route mounted")
	}
	if router.ServiceCatalog.Correlations == nil {
		t.Fatal("newRouter().ServiceCatalog.Correlations = nil, want Postgres read model store")
	}
	if router.ObservabilityCoverage == nil {
		t.Fatal("newRouter().ObservabilityCoverage = nil, want observability coverage route mounted")
	}
	if router.ObservabilityCoverage.Correlations == nil {
		t.Fatal("newRouter().ObservabilityCoverage.Correlations = nil, want Postgres read model store")
	}
	if router.SemanticEvidence == nil {
		t.Fatal("newRouter().SemanticEvidence = nil, want semantic evidence route mounted")
	}
	if router.SemanticSearch == nil {
		t.Fatal("newRouter().SemanticSearch = nil, want semantic search route mounted")
	}
	if router.SemanticSearch.Index == nil {
		t.Fatal("newRouter().SemanticSearch.Index = nil, want Postgres search-index store")
	}
}

func TestNewRouterUsesSuppliedStatusReader(t *testing.T) {
	t.Parallel()

	reader := staticStatusReader{}
	router, err := newRouter(
		nil,
		nil,
		nil,
		reader,
		nil,
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	if router.Status == nil {
		t.Fatal("newRouter().Status = nil, want status handler")
	}
	if router.Status.StatusReader != reader {
		t.Fatalf("newRouter().Status.StatusReader = %#v, want supplied reader", router.Status.StatusReader)
	}
}

func TestMetricsTimeSeriesSourceFromEnvUsesPrometheusMimirCollectorConfig(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuthorization string
	var gotTenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Scope-OrgID")
		query.WriteJSON(w, http.StatusOK, map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "matrix",
				"result": []map[string]any{{
					"metric": map[string]string{},
					"values": [][]any{{float64(1780300800), "3"}},
				}},
			},
		})
	}))
	defer server.Close()

	instancesJSON := fmt.Sprintf(`[{
		"instance_id": "prometheus-mimir-primary",
		"collector_kind": "prometheus_mimir",
		"mode": "continuous",
		"enabled": true,
		"claims_enabled": true,
		"configuration": {
			"targets": [{
				"provider": "mimir",
				"scope_id": "mimir:tenant:prod",
				"instance_id": "ops-prod",
				"base_url": %q,
				"path_prefix": "/prometheus",
				"token_env": "MIMIR_TOKEN",
				"tenant_id_env": "MIMIR_TENANT",
				"enabled": true
			}]
		}
	}]`, server.URL)
	env := map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON":               instancesJSON,
		"ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID": "prometheus-mimir-primary",
		"MIMIR_TOKEN":  "token-value",
		"MIMIR_TENANT": "tenant-prod",
	}

	source, err := metricsTimeSeriesSourceFromEnv(func(key string) string {
		return env[key]
	}, server.Client())
	if err != nil {
		t.Fatalf("metricsTimeSeriesSourceFromEnv() error = %v, want nil", err)
	}
	if source == nil {
		t.Fatal("metricsTimeSeriesSourceFromEnv() = nil, want configured source")
	}
	points, err := source.RangeQuery(context.Background(), query.MetricsRangeQuery{
		Metric: "queue_depth",
		Window: "1h",
		Step:   "30m",
	})
	if err != nil {
		t.Fatalf("RangeQuery() error = %v, want nil", err)
	}
	if got, want := gotPath, "/prometheus/api/v1/query_range"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotAuthorization, "Bearer token-value"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := gotTenant, "tenant-prod"; got != want {
		t.Fatalf("X-Scope-OrgID = %q, want %q", got, want)
	}
	if got, want := len(points), 1; got != want {
		t.Fatalf("len(points) = %d, want %d", got, want)
	}
}

func TestNewRouter_MountsAdminRoutes(t *testing.T) {
	t.Parallel()

	router, err := newRouter(
		nil,
		nil,
		nil,
		staticStatusReader{},
		nil,
		"production",
		"neo4j",
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
	)
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

func TestNewSupplyChainEvidenceSourceIsWired(t *testing.T) {
	t.Parallel()

	if source := newSupplyChainEvidenceSource(nil, nil); source == nil {
		t.Fatal("newSupplyChainEvidenceSource() = nil, want durable supply-chain evidence source")
	}
}

type staticStatusReader struct{}

func (staticStatusReader) ReadStatusSnapshot(context.Context, time.Time) (statuspkg.RawSnapshot, error) {
	return statuspkg.RawSnapshot{}, nil
}

func (staticStatusReader) ReadStatusSnapshotFiltered(
	context.Context,
	time.Time,
	statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	return statuspkg.RawSnapshot{}, nil
}
