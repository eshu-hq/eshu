// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveRouteMapsQualifiedServiceIDToServicePath(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_context", map[string]any{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/services/sample-service-api/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["environment"], "prod"; got != want {
		t.Fatalf("route.query[environment] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsServiceStoryQualifiedIDToExactSelector(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_story", map[string]any{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/services/sample-service-api/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["service_id"], "workload:sample-service-api"; got != want {
		t.Fatalf("route.query[service_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["environment"], "prod"; got != want {
		t.Fatalf("route.query[environment] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsServiceStoryCatalogIDAsNameSelector(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_story", map[string]any{
		"workload_id": "service:sample-service-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/services/sample-service-api/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got := route.query["service_id"]; got != "" {
		t.Fatalf("route.query[service_id] = %#v, want empty for catalog service id", got)
	}
}

func TestResolveRouteMapsServiceStoryRepositoryScopedServiceName(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_story", map[string]any{
		"service_name":  "sample-service-api",
		"repository_id": "repository:r_sample",
		"environment":   "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/services/sample-service-api/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["repo"], "repository:r_sample"; got != want {
		t.Fatalf("route.query[repo] = %#v, want %#v", got, want)
	}
	if got, want := route.query["environment"], "prod"; got != want {
		t.Fatalf("route.query[environment] = %#v, want %#v", got, want)
	}
	if got := route.query["service_id"]; got != "" {
		t.Fatalf("route.query[service_id] = %#v, want empty for service-name selector", got)
	}
}

func TestDispatchToolServiceContextRejectsServiceNameSelector(t *testing.T) {
	t.Parallel()

	result, err := dispatchTool(
		context.Background(),
		http.NewServeMux(),
		"get_service_context",
		map[string]any{"service_name": "sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err == nil {
		t.Fatalf("dispatchTool() error = nil, result = %#v; want workload_id selector error", result)
	}
	want := "get_service_context requires workload_id"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("dispatchTool() error = %q, want %q", err, want)
	}
}

func TestDispatchToolServiceStoryRepositoryScopedServiceName(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/sample-service-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("repo"), "repository:r_sample"; got != want {
			t.Fatalf("repo query = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("environment"), "prod"; got != want {
			t.Fatalf("environment query = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"service_identity": map[string]any{
					"service_id": "workload:sample-service-api",
					"repo_id":    "repository:r_sample",
				},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "platform_impact.context_overview",
				"profile":    "production",
				"basis":      "hybrid",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{
			"service_name":  "sample-service-api",
			"repository_id": "repository:r_sample",
			"environment":   "prod",
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured service story envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	identity := mcpMapValue(data, "service_identity")
	if got, want := query.StringVal(identity, "repo_id"), "repository:r_sample"; got != want {
		t.Fatalf("service_identity.repo_id = %q, want %q", got, want)
	}
}

func TestDispatchToolServiceStoryPreservesSpecCountConsistency(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/sample-service-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("service_id"), "workload:sample-service-api"; got != want {
			t.Fatalf("service_id query = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"api_surface":      map[string]any{"spec_count": 2, "spec_paths": []string{"openapi.yaml", "admin.yaml"}},
				"deployment_lanes": []map[string]any{{"lane_type": "k8s_gitops"}},
				"evidence_graph":   map[string]any{"edges": []map[string]any{{"resolved_id": "resolved-gitops"}}},
				"service_identity": map[string]any{"service_id": "workload:sample-service-api"},
				"support_overview": map[string]any{"spec_count": 2},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "platform_impact.context_overview",
				"profile":    "production",
				"basis":      "hybrid",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured service story envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	apiSurface := mcpMapValue(data, "api_surface")
	supportOverview := mcpMapValue(data, "support_overview")
	if got, want := query.IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got, want := query.IntVal(supportOverview, "spec_count"), query.IntVal(apiSurface, "spec_count"); got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
}

func TestDispatchToolServiceStorySpecCountsMatchQueryReadback(t *testing.T) {
	t.Parallel()

	handler := &query.EntityHandler{
		Neo4j:   mcpServiceStorySpecCountGraphReader{t: t},
		Content: mcpNoopContentStore{},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured service story envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	apiSurface := mcpMapValue(data, "api_surface")
	supportOverview := mcpMapValue(data, "support_overview")
	if got, want := query.IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got, want := query.IntVal(supportOverview, "spec_count"), 2; got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
}

type mcpServiceStorySpecCountGraphReader struct {
	t *testing.T
}

func (g mcpServiceStorySpecCountGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
		return []map[string]any{{
			"repo_id": "repo-sample-service-api", "repo_name": "sample-service-api",
		}}, nil
	case strings.Contains(cypher, "w.id = $service_id"):
		if got, want := params["service_id"], "workload:sample-service-api"; got != want {
			g.t.Fatalf("params[service_id] = %#v, want %q", got, want)
		}
		return []map[string]any{{
			"id":        "workload:sample-service-api",
			"name":      "sample-service-api",
			"kind":      "service",
			"repo_id":   "repo-sample-service-api",
			"repo_name": "sample-service-api",
		}}, nil
	case strings.Contains(cypher, "w.name = $service_name"):
		if got, want := params["service_name"], "sample-service-api"; got != want {
			g.t.Fatalf("params[service_name] = %#v, want %q", got, want)
		}
		return []map[string]any{{
			"id":        "workload:sample-service-api",
			"name":      "sample-service-api",
			"kind":      "service",
			"repo_id":   "repo-sample-service-api",
			"repo_name": "sample-service-api",
		}}, nil
	case strings.Contains(cypher, "RETURN count(endpoint) AS endpoint_count"):
		return []map[string]any{{"endpoint_count": 2}}, nil
	case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
		if got, want := params["limit"], 50; got != want {
			g.t.Fatalf("params[limit] = %#v, want %d", got, want)
		}
		return []map[string]any{
			{
				"endpoint_id":     "endpoint:public",
				"path":            "/v1/orders",
				"methods":         []string{"GET"},
				"operation_ids":   []string{"listOrders"},
				"source_kinds":    []string{"openapi"},
				"source_paths":    []string{"openapi/public.yaml"},
				"spec_versions":   []string{"3.0.3"},
				"api_versions":    []string{"v1"},
				"evidence_source": "graph",
				"workload_id":     "workload:sample-service-api",
				"workload_name":   "sample-service-api",
			},
			{
				"endpoint_id":     "endpoint:admin",
				"path":            "/admin/health",
				"methods":         []string{"GET"},
				"operation_ids":   []string{"getHealth"},
				"source_kinds":    []string{"openapi"},
				"source_paths":    []string{"openapi/admin.yaml"},
				"spec_versions":   []string{"3.1.0"},
				"api_versions":    []string{"admin"},
				"evidence_source": "graph",
				"workload_id":     "workload:sample-service-api",
				"workload_name":   "sample-service-api",
			},
		}, nil
	default:
		return nil, nil
	}
}

func (g mcpServiceStorySpecCountGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	_ map[string]any,
) (map[string]any, error) {
	if strings.Contains(cypher, "w.id = $workload_id") {
		return map[string]any{
			"id":        "workload:sample-service-api",
			"name":      "sample-service-api",
			"kind":      "service",
			"repo_id":   "repo-sample-service-api",
			"repo_name": "sample-service-api",
		}, nil
	}
	return nil, nil
}

func mcpMapValue(row map[string]any, key string) map[string]any {
	value, _ := row[key].(map[string]any)
	return value
}

type mcpNoopContentStore struct{}

func (mcpNoopContentStore) GetFileContent(context.Context, string, string) (*query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) GetFileLines(context.Context, string, string, int, int) (*query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) GetEntityContent(context.Context, string) (*query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContent(context.Context, string, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContentAnyRepo(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntityContent(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntityContentAnyRepo(context.Context, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByNameAnyRepo(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesReferencingComponent(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoFiles(context.Context, string, int) ([]query.FileContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntities(context.Context, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntitiesByType(context.Context, string, string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoEntitiesByPaths(context.Context, string, []string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) SearchEntitiesByLanguageAndType(
	context.Context,
	string,
	string,
	string,
	string,
	int,
) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListFrameworkRoutes(context.Context, string) ([]query.FrameworkRouteEvidence, error) {
	return nil, nil
}

func (mcpNoopContentStore) RepositoryCoverage(context.Context, string) (query.RepositoryContentCoverage, error) {
	return query.RepositoryContentCoverage{}, nil
}

func (mcpNoopContentStore) CountRepositoriesByLanguage(
	context.Context,
	[]string,
	bool,
	[]string,
	[]string,
) (query.RepositoryLanguageAggregate, error) {
	return query.RepositoryLanguageAggregate{}, nil
}

func (mcpNoopContentStore) ListRepositoriesByLanguage(
	context.Context,
	[]string,
	int,
	int,
	bool,
	[]string,
	[]string,
) ([]query.RepositoryLanguageRepository, error) {
	return nil, nil
}

func (mcpNoopContentStore) RepositoryLanguageInventory(
	context.Context,
	int,
	int,
	bool,
	[]string,
	[]string,
) ([]query.RepositoryLanguageInventoryRow, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepositories(context.Context) ([]query.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (mcpNoopContentStore) MatchRepositories(context.Context, string) ([]query.RepositoryCatalogEntry, error) {
	return nil, nil
}

func (mcpNoopContentStore) ResolveRepository(context.Context, string) (*query.RepositoryCatalogEntry, error) {
	return nil, nil
}

var _ query.ContentStore = mcpNoopContentStore{}
