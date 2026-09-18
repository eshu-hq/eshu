// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live scoped-grant answer-truth proof for #6786.
//
// GET /api/v0/entities/{id}/context and GET /api/v0/workloads/{id}/context
// both bound a scoped caller's read to their granted repositories. The
// scoped predicate used to be rendered as Cypher -- a multi-line
// `AND EXISTS { ... }` group for the entity route, and a multi-line
// `AND ( ... OR EXISTS {...} )` group for the workload route. On the pinned
// NornicDB v1.3.3 image that shape is unreliable: it can silently drop the
// WHOLE WHERE clause, including an unrelated `e.id = $entity_id` /
// `w.id = $workload_id` anchor on the SAME MATCH, so a scoped caller's
// request for one entity/workload could read back a DIFFERENT one it never
// asked for, or an ungranted caller could read a workload it has no
// relationship to at all. The fix moved the grant decision into Go; these
// tests drive the real production handlers against a live backend with
// Eshu's real schema applied (NornicDB's read-predicate behavior differs
// materially without it) to prove the fix and guard the regression.
//
// Run against both pinned backends (schema and seed are applied fresh by
// each test run; the id prefix is unique per run):
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27880 \
//	  ESHU_LIVE_GRAPH_BACKEND=nornicdb ESHU_LIVE_GRAPH_DATABASE=nornic \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveScoped -count=1 -v
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27890 \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveScoped -count=1 -v
package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const scopedGrantLivePrefix = "scoped-grant-6786:"

// scopedGrantLiveSeed creates two repositories -- one the test grants access
// to, one it does not -- plus one entity and one workload anchored to each,
// and a name-collision workload whose OWN repo_id names the ungranted
// repository but which the granted repository DEFINES too.
//
// wl-in and wl-out each carry a DEFINES edge from their OWN repo_id's
// repository, matching production materialization (the reducer projects a
// DEFINES edge from every workload's own repository, not only for
// name-collision cases). #6786 review follow-up (F1): without repo-b
// DEFINES wl-out, the live no_grant_relationship_returns_not_found case
// below could pass on a false green -- FetchWorkloadRepositoryForAccess's
// DEFINES read finding zero rows either because NornicDB correctly filtered
// them, or because there was nothing to filter in the first place. Seeding
// the ungranted DEFINES edge forces the read to prove it actually excludes
// an ungranted candidate rather than finding an empty set trivially.
var scopedGrantLiveSeed = []string{
	`CREATE (:Repository {id: 'scoped-grant-6786:repo-a', name: 'repo-a'})`,
	`CREATE (:Repository {id: 'scoped-grant-6786:repo-b', name: 'repo-b'})`,
	`CREATE (:File {id: 'scoped-grant-6786:file-a', relative_path: 'a.go', language: 'go'})`,
	`CREATE (:File {id: 'scoped-grant-6786:file-b', relative_path: 'b.go', language: 'go'})`,
	`CREATE (:Function {id: 'scoped-grant-6786:fn-in', name: 'FnIn', language: 'go'})`,
	`CREATE (:Function {id: 'scoped-grant-6786:fn-out', name: 'FnOut', language: 'go'})`,
	`CREATE (:Workload {id: 'scoped-grant-6786:wl-in', name: 'wl-in', repo_id: 'scoped-grant-6786:repo-a'})`,
	`CREATE (:Workload {id: 'scoped-grant-6786:wl-out', name: 'wl-out', repo_id: 'scoped-grant-6786:repo-b'})`,
	`CREATE (:Workload {id: 'scoped-grant-6786:wl-collision', name: 'wl-collision', repo_id: 'scoped-grant-6786:repo-b'})`,
	// #6786 review follow-up (F2): a WorkloadInstance of each workload, to
	// exercise GetEntityContext's queryselector hydration path for entity
	// types no File CONTAINS -- Workload and WorkloadInstance both resolve
	// their repo_id through DEFINES, not REPO_CONTAINS/CONTAINS.
	`CREATE (:WorkloadInstance {id: 'scoped-grant-6786:wli-in', name: 'wli-in'})`,
	`CREATE (:WorkloadInstance {id: 'scoped-grant-6786:wli-out', name: 'wli-out'})`,
	scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-a", "REPO_CONTAINS", "File", "scoped-grant-6786:file-a"),
	scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-b", "REPO_CONTAINS", "File", "scoped-grant-6786:file-b"),
	scopedGrantLiveEdge("File", "scoped-grant-6786:file-a", "CONTAINS", "Function", "scoped-grant-6786:fn-in"),
	scopedGrantLiveEdge("File", "scoped-grant-6786:file-b", "CONTAINS", "Function", "scoped-grant-6786:fn-out"),
	// Every workload's own repository DEFINES it (production shape).
	scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-a", "DEFINES", "Workload", "scoped-grant-6786:wl-in"),
	scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-b", "DEFINES", "Workload", "scoped-grant-6786:wl-out"),
	scopedGrantLiveEdge("WorkloadInstance", "scoped-grant-6786:wli-in", "INSTANCE_OF", "Workload", "scoped-grant-6786:wl-in"),
	scopedGrantLiveEdge("WorkloadInstance", "scoped-grant-6786:wli-out", "INSTANCE_OF", "Workload", "scoped-grant-6786:wl-out"),
	// repo-a DEFINES the collision workload too, even though the workload's
	// own repo_id names repo-b: DEFINES admission must catch this.
	scopedGrantLiveEdge("Repository", "scoped-grant-6786:repo-a", "DEFINES", "Workload", "scoped-grant-6786:wl-collision"),
}

func scopedGrantLiveEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

const scopedGrantLiveCleanup = `MATCH (n) WHERE n.id STARTS WITH '` + scopedGrantLivePrefix + `' DETACH DELETE n`

// scopedGrantLiveFixture opens the driver, applies schema, seeds, and
// registers cleanup. It fails the test outright on any setup error: every
// scoped_grant_live_test.go test depends on this fixture being trustworthy.
func scopedGrantLiveFixture(t *testing.T) (entityLiveReader, context.Context) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := liveGraphBackend()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	if err := graph.EnsureSchemaWithBackendStrict(ctx, liveSchemaExecutor{driver: driver, database: database}, nil, backend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	reader := entityLiveReader{driver: driver, database: database}
	reader.write(ctx, t, scopedGrantLiveCleanup)
	for _, stmt := range scopedGrantLiveSeed {
		reader.write(ctx, t, stmt)
	}
	t.Cleanup(func() { reader.write(context.Background(), t, scopedGrantLiveCleanup) })

	return reader, ctx
}

// scopedRequestContext returns ctx carrying a scoped AuthContext granted only
// allowedRepositoryIDs, the shape production request middleware installs for
// a scoped token.
func scopedRequestContext(ctx context.Context, allowedRepositoryIDs ...string) context.Context {
	return queryauth.ContextWithAuthContext(ctx, queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	})
}

func TestLiveScopedEntityContextGrant(t *testing.T) {
	reader, baseCtx := scopedGrantLiveFixture(t)
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}

	getEntityContext := func(ctx context.Context, entityID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+entityID+"/context", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("entity_id", entityID)
		rec := httptest.NewRecorder()
		handler.GetEntityContext(rec, req)
		return rec
	}

	t.Run("in_grant_returns_own_entity", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:fn-in")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"scoped-grant-6786:fn-in"`) {
			t.Fatalf("body = %s, want the requested in-grant entity id", rec.Body.String())
		}
	})

	t.Run("out_of_grant_returns_not_found_not_another_entity", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:fn-out")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for an out-of-grant entity id; body = %s", rec.Code, rec.Body.String())
		}
		// The #6786 failure mode: the buggy predicate returned a DIFFERENT,
		// in-grant entity's data instead of 404. Assert that entity's id
		// never leaks into this response even inside an error body.
		if strings.Contains(rec.Body.String(), "fn-in") {
			t.Fatalf("body = %s, leaked the in-grant entity's id for an out-of-grant request", rec.Body.String())
		}
	})

	t.Run("unscoped_admin_sees_out_of_grant_entity", func(t *testing.T) {
		rec := getEntityContext(baseCtx, "scoped-grant-6786:fn-out")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for an unscoped caller; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"scoped-grant-6786:fn-out"`) {
			t.Fatalf("body = %s, want the requested entity id for an unscoped caller", rec.Body.String())
		}
	})

	// #6786 review follow-up (F2): GetEntityContext's graph MATCH has no
	// label filter, so a Workload or WorkloadInstance id reaches it too. No
	// File CONTAINS either label, so the OPTIONAL MATCH the entity route
	// itself renders never resolves repo_id for them; that job belongs to
	// queryselector.HydrateResolvedEntityRepoIdentity's DEFINES-based
	// backfill (entity_repo_identity.go), which is NOT part of this PR's
	// diff but was proven live to return garbage column values on NornicDB
	// v1.3.3 (an UNWIND variable colliding with a RETURN alias). This is an
	// explicit, intentional behavior change from pre-#6786 semantics on
	// Neo4j (where these ids used to 404 via a different path): a scoped
	// caller now gets 200 with the correct repo for an in-grant Workload/
	// WorkloadInstance id, and 404 for an out-of-grant one, on both backends.
	t.Run("workload_entity_id_in_grant_returns_200_with_repo", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:wl-in")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"repo_id":"scoped-grant-6786:repo-a"`) {
			t.Fatalf("body = %s, want repo_id hydrated to the granted repository", rec.Body.String())
		}
	})

	t.Run("workload_entity_id_out_of_grant_returns_not_found", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:wl-out")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for an out-of-grant Workload entity id; body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("workload_instance_entity_id_in_grant_returns_200_with_repo", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:wli-in")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"repo_id":"scoped-grant-6786:repo-a"`) {
			t.Fatalf("body = %s, want repo_id hydrated via the INSTANCE_OF/DEFINES path to the granted repository", rec.Body.String())
		}
	})

	t.Run("workload_instance_entity_id_out_of_grant_returns_not_found", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getEntityContext(ctx, "scoped-grant-6786:wli-out")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for an out-of-grant WorkloadInstance entity id; body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestLiveScopedWorkloadContextGrant(t *testing.T) {
	reader, baseCtx := scopedGrantLiveFixture(t)
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}

	getWorkloadContext := func(ctx context.Context, workloadID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/"+workloadID+"/context", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("workload_id", workloadID)
		rec := httptest.NewRecorder()
		handler.GetWorkloadContext(rec, req)
		return rec
	}

	t.Run("direct_grant_admits", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getWorkloadContext(ctx, "scoped-grant-6786:wl-in")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"scoped-grant-6786:wl-in"`) {
			t.Fatalf("body = %s, want the requested in-grant workload id", rec.Body.String())
		}
	})

	t.Run("no_grant_relationship_returns_not_found", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getWorkloadContext(ctx, "scoped-grant-6786:wl-out")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for a workload with no grant relationship; body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("defines_admission_admits_name_collision_workload", func(t *testing.T) {
		ctx := scopedRequestContext(baseCtx, "scoped-grant-6786:repo-a")
		rec := getWorkloadContext(ctx, "scoped-grant-6786:wl-collision")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (admitted via DEFINES despite an ungranted own repo_id); body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"scoped-grant-6786:wl-collision"`) {
			t.Fatalf("body = %s, want the requested collision workload id", rec.Body.String())
		}
	})

	t.Run("unscoped_admin_sees_out_of_grant_workload", func(t *testing.T) {
		rec := getWorkloadContext(baseCtx, "scoped-grant-6786:wl-out")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for an unscoped caller; body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"scoped-grant-6786:wl-out"`) {
			t.Fatalf("body = %s, want the requested workload id for an unscoped caller", rec.Body.String())
		}
	})
}
