// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/compare"
	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestHistoricalAppSyncKeyReadRoutesSuppressLegacyGraphNodeLive(t *testing.T) {
	if backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")); backend != "neo4j" {
		t.Fatalf("ESHU_LIVE_GRAPH_BACKEND = %q, want neo4j from the isolated live-backend runner", backend)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required from the live-backend runner")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open isolated Neo4j: %v", err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database != "neo4j" {
		t.Fatalf("ESHU_NEO4J_DATABASE = %q, want neo4j from the live-backend runner", database)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify isolated Neo4j: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	prefix := fmt.Sprintf("eshu-security-key-proof-%d", time.Now().UnixNano())
	keyID, healthyID := prefix+":key", prefix+":api"
	pathSourceID, pathTargetID := prefix+":path-source", prefix+":path-target"
	healthySourceID, healthyTargetID := prefix+":healthy-source", prefix+":healthy-target"
	dataTypeOnlyID, conflictingTypeID := prefix+":data-type-only", prefix+":conflicting-type"
	missingTypeID := prefix + ":missing-type"
	workloadID, instanceID, repoID := prefix+":workload", prefix+":instance", prefix+":repo"
	secretMarker := prefix + ":stored-secret-marker"
	dataTypeSecretMarker, conflictingSecretMarker := prefix+":data-type-secret", prefix+":conflicting-secret"
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(context.Background()) }()
	seed := `CREATE (repo:Repository {id:$repo_id, name:$prefix, repo_id:$repo_id})
CREATE (workload:Workload {id:$workload_id, name:$prefix, kind:'service', repo_id:$repo_id})
CREATE (path_source:Workload {id:$path_source_id, name:$prefix+':path-source', kind:'service', repo_id:$repo_id})
CREATE (path_target:Workload {id:$path_target_id, name:$prefix+':path-target', kind:'service', repo_id:$repo_id})
CREATE (healthy_source:Workload {id:$healthy_source_id, name:$prefix+':healthy-source', kind:'service', repo_id:$repo_id})
CREATE (healthy_target:Workload {id:$healthy_target_id, name:$prefix+':healthy-target', kind:'service', repo_id:$repo_id})
CREATE (repo)-[:DEFINES]->(workload)
CREATE (instance:WorkloadInstance {id:$instance_id, name:$prefix, workload_id:$workload_id, environment:'prod', repo_id:$repo_id})
CREATE (instance)-[:INSTANCE_OF]->(workload)
CREATE (key:CloudResource {id:$key_id, uid:$key_id, name:$secret_marker, resource_id:$secret_marker, arn:$secret_marker, resource_type:'aws_appsync_api_key', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (data_type_key:CloudResource {id:$data_type_only_id, uid:$data_type_only_id, name:$data_type_secret_marker, data_type:'aws_appsync_api_key', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (conflicting_key:CloudResource {id:$conflicting_type_id, uid:$conflicting_type_id, name:$conflicting_secret_marker, resource_type:'aws_lambda_function', data_type:'aws_appsync_api_key', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (api:CloudResource {id:$healthy_id, uid:$healthy_id, name:$prefix+':healthy-api', resource_id:$prefix+':rid', arn:'arn:aws:appsync:us-east-1:123456789012:apis/healthy', resource_type:'aws_appsync_graphql_api', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (missing:CloudResource {id:$missing_type_id, uid:$missing_type_id, name:$prefix+':missing-type', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (instance)-[:USES]->(key)
CREATE (instance)-[:USES]->(api)
CREATE (workload)-[:USES]->(key)
CREATE (workload)-[:USES]->(data_type_key)
CREATE (workload)-[:USES]->(conflicting_key)
CREATE (workload)-[:USES]->(api)
CREATE (path_source)-[:USES]->(key)
CREATE (path_target)-[:USES]->(key)
CREATE (healthy_source)-[:USES]->(api)
CREATE (api)-[:USES]->(healthy_target)`
	params := map[string]any{"prefix": prefix, "key_id": keyID, "healthy_id": healthyID, "path_source_id": pathSourceID, "path_target_id": pathTargetID, "healthy_source_id": healthySourceID, "healthy_target_id": healthyTargetID, "data_type_only_id": dataTypeOnlyID, "conflicting_type_id": conflictingTypeID, "missing_type_id": missingTypeID, "workload_id": workloadID, "instance_id": instanceID, "repo_id": repoID, "secret_marker": secretMarker, "data_type_secret_marker": dataTypeSecretMarker, "conflicting_secret_marker": conflictingSecretMarker}
	result, err := session.Run(ctx, seed, params)
	if err != nil {
		t.Fatalf("seed synthetic legacy graph nodes: %v", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("commit synthetic legacy graph nodes: %v", err)
	}
	defer func() {
		cleanup, err := session.Run(context.Background(), `MATCH (n) WHERE n.id STARTS WITH $prefix DETACH DELETE n`, map[string]any{"prefix": prefix})
		if err == nil {
			_, _ = cleanup.Consume(context.Background())
		}
	}()

	reader := NewNeo4jReader(driver, database)
	graphBackend := querycontract.GraphBackendNeo4j
	mux := http.NewServeMux()
	(&InfraHandler{Neo4j: reader}).Mount(mux)
	(&GraphEntityInventoryHandler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)
	(&entity.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction, GraphBackend: graphBackend}).Mount(mux)
	(&impact.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)
	(&compare.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)

	checks := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{"infra search", http.MethodPost, "/api/v0/infra/resources/search", `{"query":"` + prefix + `","limit":10}`, http.StatusOK},
		{"graph entity list", http.MethodGet, "/api/v0/graph/entities?kind=cloud_resources&q=" + url.QueryEscape(prefix) + "&limit=10", "", http.StatusOK},
		{"entity context relationship", http.MethodGet, "/api/v0/entities/" + url.PathEscape(workloadID) + "/context", "", http.StatusOK},
		{"entity context hidden anchor resource_type", http.MethodGet, "/api/v0/entities/" + url.PathEscape(keyID) + "/context", "", http.StatusNotFound},
		{"entity context hidden anchor data_type", http.MethodGet, "/api/v0/entities/" + url.PathEscape(dataTypeOnlyID) + "/context", "", http.StatusNotFound},
		{"entity context hidden anchor conflicting types", http.MethodGet, "/api/v0/entities/" + url.PathEscape(conflictingTypeID) + "/context", "", http.StatusNotFound},
		{"infra relationships", http.MethodPost, "/api/v0/infra/relationships", `{"entity_id":"` + workloadID + `"}`, http.StatusOK},
		{"resource investigation", http.MethodPost, "/api/v0/impact/resource-investigation", `{"resource_id":"` + keyID + `","resource_type":"cloud","limit":5}`, http.StatusOK},
		{"change surface resolver", http.MethodPost, "/api/v0/impact/change-surface/investigate", `{"target":"` + keyID + `","target_type":"cloud_resource","limit":5}`, http.StatusOK},
		{"entity map resolver", http.MethodPost, "/api/v0/impact/entity-map", `{"from":"` + keyID + `","from_type":"cloud_resource","limit":5}`, http.StatusOK},
		{"deployment trace", http.MethodPost, "/api/v0/impact/trace-deployment-chain", `{"service_name":"` + prefix + `"}`, http.StatusOK},
		{"environment comparison", http.MethodPost, "/api/v0/compare/environments", `{"workload_id":"` + workloadID + `","left":"prod","right":"qa"}`, http.StatusOK},
		{"explain dependency path", http.MethodPost, "/api/v0/impact/explain-dependency-path", `{"source":"` + pathSourceID + `","target":"` + pathTargetID + `"}`, http.StatusOK},
		{"explain dependency path healthy control", http.MethodPost, "/api/v0/impact/explain-dependency-path", `{"source":"` + healthySourceID + `","target":"` + healthyTargetID + `"}`, http.StatusOK},
		{"entity context relationship scoped", http.MethodGet, "/api/v0/entities/" + url.PathEscape(workloadID) + "/context", "", http.StatusOK},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			req := httptest.NewRequest(check.method, check.path, strings.NewReader(check.body))
			if check.name == "entity context relationship scoped" {
				req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
					Mode: auth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a",
					AllowedRepositoryIDs: []string{repoID},
				}))
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != check.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, check.wantStatus, rec.Body.String())
			}
			response := rec.Body.String()
			if check.name == "infra relationships" {
				data := decodeLiveEnvelope(t, response)
				if got := data["id"]; got != workloadID {
					t.Fatalf("relationship anchor id = %#v, want %q", got, workloadID)
				}
				outgoing, ok := data["outgoing"].([]any)
				if !ok {
					t.Fatalf("outgoing relationships = %#v, want []any", data["outgoing"])
				}
				foundHealthyNeighbor := false
				for _, raw := range outgoing {
					relationship, ok := raw.(map[string]any)
					if !ok {
						t.Fatalf("outgoing relationship = %#v, want object", raw)
					}
					targetID := StringVal(relationship, "target_id")
					if targetID == healthyID {
						foundHealthyNeighbor = true
					}
					if targetID == keyID || targetID == dataTypeOnlyID || targetID == conflictingTypeID {
						t.Fatalf("infra relationships exposed legacy key neighbor %q: %s", targetID, response)
					}
				}
				if !foundHealthyNeighbor {
					t.Fatalf("infra relationships dropped healthy AppSync API neighbor %q: %s", healthyID, response)
				}
			}
			if strings.Contains(response, secretMarker) || strings.Contains(response, dataTypeSecretMarker) || strings.Contains(response, conflictingSecretMarker) {
				t.Fatalf("response exposed a stored legacy AppSync key field: %s", response)
			}
			if strings.Contains(response, dataTypeOnlyID) || strings.Contains(response, conflictingTypeID) {
				t.Fatalf("response exposed legacy AppSync key identity: %s", response)
			}
			if strings.HasPrefix(check.name, "entity context hidden anchor") && strings.Contains(response, keyID) {
				t.Fatalf("entity-context not-found response exposed legacy key identity: %s", response)
			}
			if !strings.Contains(check.path+check.body, keyID) && strings.Contains(response, keyID) {
				t.Fatalf("response exposed an unrequested legacy AppSync key identity: %s", response)
			}
			if strings.Contains(check.body, keyID) && (check.name == "resource investigation" || check.name == "change surface resolver" || check.name == "entity map resolver") && !strings.Contains(response, `"status":"no_match"`) {
				t.Fatalf("selector resolved a legacy AppSync key; response=%s", response)
			}
			if check.name == "explain dependency path" {
				data := decodeLiveEnvelope(t, response)
				if path, ok := data["path"]; ok && path != nil {
					t.Fatalf("dependency path crossed a hidden legacy key: %s", response)
				}
			}
			if check.name == "explain dependency path healthy control" {
				data := decodeLiveEnvelope(t, response)
				if _, ok := data["path"].(map[string]any); !ok {
					t.Fatalf("healthy dependency path was dropped: %s", response)
				}
			}
			if check.name == "graph entity list" {
				data := decodeLiveEnvelope(t, response)
				if got := cloudResourceFacetCount(t, data["kinds"]); got != 5 {
					t.Fatalf("stored cloud-resource total = %d, want 5 including historical keys and missing-type control", got)
				}
				if entities, ok := data["entities"].([]any); !ok || len(entities) != 2 {
					t.Fatalf("visible cloud-resource page = %#v, want healthy and missing-type rows only", data["entities"])
				}
				filteredReq := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=cloud_resources&q="+url.QueryEscape(secretMarker)+"&limit=10", strings.NewReader(""))
				filteredRec := httptest.NewRecorder()
				mux.ServeHTTP(filteredRec, filteredReq)
				filtered := decodeLiveEnvelope(t, filteredRec.Body.String())
				if got := cloudResourceFacetCount(t, filtered["kinds"]); got != 5 {
					t.Fatalf("search changed stored cloud-resource total to %d; want 5", got)
				}
				if entities, ok := filtered["entities"].([]any); !ok || len(entities) != 0 {
					t.Fatalf("hidden-key search returned visible entities: %#v", filtered["entities"])
				}
			}
		})
	}
}

func decodeLiveEnvelope(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode live response: %v", err)
	}
	data, ok := envelope["data"].(map[string]any)
	if ok {
		return data
	}
	return envelope
}

func cloudResourceFacetCount(t *testing.T, raw any) int {
	t.Helper()
	kinds, ok := raw.([]any)
	if !ok {
		t.Fatalf("kinds = %T, want array", raw)
	}
	for _, rawKind := range kinds {
		kind, ok := rawKind.(map[string]any)
		if ok && kind["kind"] == "cloud_resources" {
			count, ok := kind["count"].(float64)
			if !ok {
				t.Fatalf("cloud_resources count = %T, want number", kind["count"])
			}
			return int(count)
		}
	}
	t.Fatal("cloud_resources facet missing")
	return 0
}
