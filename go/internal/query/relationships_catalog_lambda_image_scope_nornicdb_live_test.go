// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const relationshipsEdgeScopeLiveEnv = "ESHU_RELATIONSHIPS_EDGE_SCOPE_NORNICDB_LIVE"

// TestRelationshipEdgesScopedCallerSeesLambdaImageEdgeLive proves issue #5450
// P1-C end to end against a real NornicDB: a scoped caller granted ONLY the
// AWS reducer scope that produced an AWS_lambda_function_uses_image edge --
// with NO repository grant at all, since CloudResource and ContainerImage
// carry no repo_id -- must still see exactly that edge through
// POST /api/v0/relationships/edges.
//
// Before the fix, relationshipEdgesScopeWhereClause bound only
// relationshipEndpointScopePredicate on the source/target NODES. Neither
// CloudResource nor ContainerImage carries repo_id or scope_id (the edge
// writer stamps rel.scope_id on the relationship itself, see
// CloudResourceContainerImageEdgeWriter), so that endpoint-only predicate
// could never match and every scoped caller silently received zero edges --
// a fail-closed but wrong empty result, not a leak. This test seeds the exact
// node/edge shape the production writers produce (CloudResource.id mirrors
// uid per CloudResourceNodeWriter; rel.scope_id is set per
// CloudResourceContainerImageEdgeWriter) and drives the real HTTP handler, so
// it fails the same way the reported production symptom did.
//
// Run against an isolated NornicDB:
//
//	ESHU_RELATIONSHIPS_EDGE_SCOPE_NORNICDB_LIVE=1 \
//	ESHU_NEO4J_URI=bolt://localhost:17699 \
//	go test ./internal/query -run TestRelationshipEdgesScopedCallerSeesLambdaImageEdgeLive -count=1 -v
func TestRelationshipEdgesScopedCallerSeesLambdaImageEdgeLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(relationshipsEdgeScopeLiveEnv)) == "" {
		t.Skip("set " + relationshipsEdgeScopeLiveEnv + "=1 to run the live NornicDB edge-scope proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	reader := NewNeo4jReader(driver, "nornic")
	countRow, err := reader.RunSingle(ctx, `MATCH (n:CloudResource) RETURN count(n) AS count`, nil)
	if err != nil {
		t.Fatalf("count existing CloudResource nodes: %v", err)
	}
	if got := IntVal(countRow, "count"); got != 0 {
		t.Fatalf("live proof requires an isolated graph with zero CloudResource nodes, got %d", got)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	lambdaUID := "lambda-live-" + suffix
	imageUID := "image-live-" + suffix
	grantedScope := "aws-scope-live-" + suffix
	otherScope := "aws-scope-other-" + suffix

	write := func(cypher string, params map[string]any) {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: "nornic",
		})
		defer func() { _ = session.Close(ctx) }()
		result, runErr := session.Run(ctx, cypher, params)
		if runErr != nil {
			t.Fatalf("live NornicDB write: %v", runErr)
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			t.Fatalf("consume live NornicDB write: %v", consumeErr)
		}
	}
	cleanup := func() {
		write(`MATCH (n:CloudResource {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": lambdaUID})
		write(`MATCH (n:ContainerImage {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": imageUID})
	}
	defer cleanup()

	// Mirror CloudResourceNodeWriter (r.id = row.uid) and
	// CloudResourceContainerImageEdgeWriter (rel.scope_id = row.scope_id)
	// exactly, including the fixed AWS_lambda_function_uses_image relationship
	// type token.
	write(`
CREATE (r:CloudResource)
SET r.uid = $uid, r.id = $uid, r.resource_type = 'aws_lambda_function', r.name = $name`,
		map[string]any{"uid": lambdaUID, "name": "live-lambda-fn"})
	write(`CREATE (i:ContainerImage) SET i.uid = $uid, i.id = $uid`,
		map[string]any{"uid": imageUID})
	write(`
MATCH (source:CloudResource {uid: $source_uid})
MATCH (target:ContainerImage {uid: $target_uid})
MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target)
SET rel.relationship_type = 'lambda_function_uses_image',
    rel.resolution_mode = 'exact',
    rel.scope_id = $scope_id,
    rel.generation_id = 'gen-live-1',
    rel.evidence_source = 'aws_cloud_image_join'`,
		map[string]any{"source_uid": lambdaUID, "target_uid": imageUID, "scope_id": grantedScope})

	entry, ok := relationshipVerbByName["AWS_LAMBDA_FUNCTION_USES_IMAGE"]
	if !ok || !entry.edgeScopeAttributable {
		t.Fatal("catalog entry for AWS_lambda_function_uses_image must resolve and be edgeScopeAttributable for this proof to be meaningful")
	}

	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}
	runEdgesRequest := func(allowedScopeIDs []string) map[string]any {
		t.Helper()
		body := []byte(`{"verb":"AWS_lambda_function_uses_image"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
		req.Header.Set("Accept", EnvelopeMIMEType)
		req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
			Mode:            AuthModeScoped,
			TenantID:        "tenant-live",
			WorkspaceID:     "workspace-live",
			AllowedScopeIDs: allowedScopeIDs,
		}))
		rec := httptest.NewRecorder()
		handler.getRelationshipEdges(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var env ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("data is not an object: %T", env.Data)
		}
		return data
	}

	// A caller granted the exact scope that produced the edge sees exactly one
	// edge, with the seeded endpoints, even though it holds no repository
	// grant at all.
	granted := runEdgesRequest([]string{grantedScope})
	edges, _ := granted["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("scoped caller granted %q got %d edges, want exactly 1: %+v", grantedScope, len(edges), granted)
	}
	first, _ := edges[0].(map[string]any)
	if first["source_id"] != lambdaUID {
		t.Fatalf("edge source_id = %v, want %v", first["source_id"], lambdaUID)
	}
	if first["target_id"] != imageUID {
		t.Fatalf("edge target_id = %v, want %v", first["target_id"], imageUID)
	}

	// A caller granted a DIFFERENT scope must not see this edge: the edge-scope
	// OR admits the exact granted scope_id only, never widens to any scope.
	other := runEdgesRequest([]string{otherScope})
	edgesOther, _ := other["edges"].([]any)
	if len(edgesOther) != 0 {
		t.Fatalf("caller granted a different scope %q got %d edges, want 0 (no cross-tenant leak): %+v", otherScope, len(edgesOther), other)
	}
}
