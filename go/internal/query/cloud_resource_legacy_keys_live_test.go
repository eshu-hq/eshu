// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/compare"
	"github.com/eshu-hq/eshu/go/internal/query/entity"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestCloudResourceListSuppressesHistoricalAppSyncKeysLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live historical-key suppression proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedCloudResourceListLiveCorpus(t, ctx, db)
	if _, err := db.ExecContext(ctx, `UPDATE graph_node_owner SET winning_row = jsonb_set(winning_row, '{resource_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE uid = 'uid-000001'`); err != nil {
		t.Fatalf("seed historical key owner row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET payload = jsonb_set(payload, '{resource_type}', to_jsonb('aws_appsync_api_key'::text)) WHERE fact_id = 'fact-000001'`); err != nil {
		t.Fatalf("seed historical key fact: %v", err)
	}

	rows, err := NewPostgresCloudResourceListStore(db).ListCloudResourceIdentities(ctx, CloudResourceListPageFilter{Limit: 2, AllScopes: true})
	if err != nil {
		t.Fatalf("list cloud-resource identities: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list returned %d rows, want a full page of 2 healthy resources", len(rows))
	}
	for _, row := range rows {
		if row.UID == "uid-000001" || row.ResourceType == "aws_appsync_api_key" {
			t.Fatalf("historical key leaked into cloud-resource page: %#v", row)
		}
	}
}

func TestHistoricalAppSyncKeyReadRoutesSuppressLegacyGraphNodeLive(t *testing.T) {
	if os.Getenv("ESHU_LEGACY_APPSYNC_SUPPRESSION_LIVE") != "1" || os.Getenv("ESHU_LEGACY_APPSYNC_SUPPRESSION_ISOLATED") != "1" {
		t.Skip("set ESHU_LEGACY_APPSYNC_SUPPRESSION_LIVE=1 and ESHU_LEGACY_APPSYNC_SUPPRESSION_ISOLATED=1 for an isolated graph proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open isolated Neo4j: %v", err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify isolated Neo4j: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	prefix := fmt.Sprintf("eshu-security-key-proof-%d", time.Now().UnixNano())
	keyID, healthyID := prefix+":key", prefix+":api"
	workloadID, instanceID, repoID := prefix+":workload", prefix+":instance", prefix+":repo"
	secretMarker := prefix + ":stored-secret-marker"
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(context.Background()) }()
	seed := `CREATE (repo:Repository {id:$repo_id, name:$prefix, repo_id:$repo_id})
CREATE (workload:Workload {id:$workload_id, name:$prefix, kind:'service', repo_id:$repo_id})
CREATE (repo)-[:DEFINES]->(workload)
CREATE (instance:WorkloadInstance {id:$instance_id, name:$prefix, workload_id:$workload_id, environment:'prod', repo_id:$repo_id})
CREATE (instance)-[:INSTANCE_OF]->(workload)
CREATE (key:CloudResource {id:$key_id, uid:$key_id, name:$secret_marker, resource_id:$secret_marker, arn:$secret_marker, resource_type:'aws_appsync_api_key', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (api:CloudResource {id:$healthy_id, uid:$healthy_id, name:$prefix+':healthy-api', resource_id:$prefix+':rid', arn:'arn:aws:appsync:us-east-1:123456789012:apis/healthy', resource_type:'aws_appsync_graphql_api', provider:'aws', source_system:'aws', environment:'prod', repo_id:$repo_id})
CREATE (instance)-[:USES]->(key)
CREATE (instance)-[:USES]->(api)
CREATE (workload)-[:USES]->(key)
CREATE (workload)-[:USES]->(api)`
	params := map[string]any{"prefix": prefix, "key_id": keyID, "healthy_id": healthyID, "workload_id": workloadID, "instance_id": instanceID, "repo_id": repoID, "secret_marker": secretMarker}
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
	mux := http.NewServeMux()
	(&InfraHandler{Neo4j: reader}).Mount(mux)
	(&GraphEntityInventoryHandler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)
	(&entity.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)
	(&impact.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)
	(&compare.Handler{Neo4j: reader, Profile: querycontract.ProfileProduction}).Mount(mux)

	checks := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{"infra search", http.MethodPost, "/api/v0/infra/resources/search", `{"query":"` + prefix + `","limit":10}`, http.StatusOK},
		{"graph entity list", http.MethodGet, "/api/v0/graph/entities?kind=cloud_resources&q=" + url.QueryEscape(prefix) + "&limit=10", "", http.StatusOK},
		{"entity context relationship", http.MethodGet, "/api/v0/entities/" + url.PathEscape(workloadID) + "/context", "", http.StatusOK},
		{"infra relationships", http.MethodPost, "/api/v0/infra/relationships", `{"entity_id":"` + healthyID + `"}`, http.StatusOK},
		{"resource investigation", http.MethodPost, "/api/v0/impact/resource-investigation", `{"resource_id":"` + keyID + `","resource_type":"cloud","limit":5}`, http.StatusOK},
		{"change surface resolver", http.MethodPost, "/api/v0/impact/change-surface/investigate", `{"target":"` + keyID + `","target_type":"cloud_resource","limit":5}`, http.StatusOK},
		{"entity map resolver", http.MethodPost, "/api/v0/impact/entity-map", `{"from":"` + keyID + `","from_type":"cloud_resource","limit":5}`, http.StatusOK},
		{"deployment trace", http.MethodPost, "/api/v0/impact/trace-deployment-chain", `{"service_name":"` + prefix + `"}`, http.StatusOK},
		{"environment comparison", http.MethodPost, "/api/v0/compare/environments", `{"workload_id":"` + workloadID + `","left":"prod","right":"qa"}`, http.StatusOK},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			req := httptest.NewRequest(check.method, check.path, strings.NewReader(check.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != check.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, check.wantStatus, rec.Body.String())
			}
			response := rec.Body.String()
			if strings.Contains(response, secretMarker) {
				t.Fatalf("response exposed a stored legacy AppSync key field: %s", response)
			}
			if !strings.Contains(check.path+check.body, keyID) && strings.Contains(response, keyID) {
				t.Fatalf("response exposed an unrequested legacy AppSync key identity: %s", response)
			}
			if strings.Contains(check.body, keyID) && (check.name == "resource investigation" || check.name == "change surface resolver" || check.name == "entity map resolver") && !strings.Contains(response, `"status":"no_match"`) {
				t.Fatalf("selector resolved a legacy AppSync key; response=%s", response)
			}
		})
	}
}
