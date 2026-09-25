// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

const liveAnchorPrefix = "anc5167:"

// TestLiveImpactScopedAnchorSharedName is the #5167 review D1 backend proof:
// two tenants' CloudResources share the name "dup-bucket" (repo-b's sorts
// first by id), and a scoped repo-a caller naming it must anchor on repo-a's
// node on every run, through the real handler, candidate statement, and live
// CloudResource ownership statement. A name only repo-b carries renders like
// an unknown name. The unscoped single-row resolve still returns one row.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//		ESHU_NEO4J_URI=bolt://localhost:17998 \
//		go test ./internal/query -run TestLiveImpactScopedAnchorSharedName -count=1 -v
func TestLiveImpactScopedAnchorSharedName(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live scoped anchor proof")
	}
	reader := openImpactLiveReader(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p := func(id string) string { return liveAnchorPrefix + id }
	cleanup := func() {
		_, _ = reader.RunWrite(context.Background(), `MATCH (n) WHERE n.id STARTS WITH $p DETACH DELETE n`, map[string]any{"p": liveAnchorPrefix})
	}
	cleanup()
	t.Cleanup(cleanup)
	seed := strings.ReplaceAll(`CREATE (ra:Repository {id:'$Prepo-a', name:'$Psvc-a'}), (rb:Repository {id:'$Prepo-b', name:'$Psvc-b'}),
		(wa:WorkloadInstance {id:'$Pwi-a', repo_id:'$Prepo-a'}), (wb:WorkloadInstance {id:'$Pwi-b', repo_id:'$Prepo-b'}),
		(c1:CloudResource {id:'$Pcr-dup-1', uid:'$Pcr-dup-1', name:'$Pdup-bucket'}),
		(c2:CloudResource {id:'$Pcr-dup-2', uid:'$Pcr-dup-2', name:'$Pdup-bucket'}),
		(c3:CloudResource {id:'$Pcr-only-b', uid:'$Pcr-only-b', name:'$Ponly-b'}),
		(wb)-[:USES]->(c1), (wa)-[:USES]->(c2), (wb)-[:USES]->(c3),
		(c1)-[:DEPENDS_ON {confidence:0.9, reason:'fixture'}]->(rb),
		(c2)-[:DEPENDS_ON {confidence:0.9, reason:'fixture'}]->(ra),
		(c3)-[:DEPENDS_ON {confidence:0.9, reason:'fixture'}]->(rb)`, "$P", liveAnchorPrefix)
	if err := reader.ExecuteCypher(ctx, graph.CypherStatement{Cypher: seed}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	candidates, err := deployment.ResolveImpactAnchorCandidates(ctx, reader, "start_id", p("dup-bucket"))
	if err != nil || len(candidates) != 2 || candidates[0].ID != p("cr-dup-1") || candidates[1].ID != p("cr-dup-2") {
		t.Fatalf("candidates = %+v, %v; want [cr-dup-1 cr-dup-2] in id order", candidates, err)
	}
	single, err := deployment.ResolveImpactAnchorNode(ctx, reader, "start_id", p("dup-bucket"))
	if err != nil || single == nil {
		t.Fatalf("unscoped single-row resolve = %+v, %v; want one node", single, err)
	}

	mux := http.NewServeMux()
	(&ImpactHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}).Mount(mux)
	scoped := testutil.ScopedTestAuthContext("tenant-a", []string{p("repo-a")})
	post := func(start string) (int, string, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-resource-to-code",
			bytes.NewBufferString(`{"start":"`+start+`","max_depth":2}`))
		req.Header.Set("Accept", EnvelopeMIMEType)
		req = req.WithContext(ContextWithAuthContext(req.Context(), scoped))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var env ResponseEnvelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		data, _ := env.Data.(map[string]any)
		return rec.Code, rec.Body.String(), data
	}
	for run := range 20 {
		_, body, data := post(p("dup-bucket"))
		startInfo, _ := data["start"].(map[string]any)
		if startInfo["id"] != p("cr-dup-2") || strings.Contains(body, p("cr-dup-1")) || strings.Contains(body, p("repo-b")) {
			t.Fatalf("run %d: scoped shared-name anchor resolved %v, want repo-a's cr-dup-2 with no repo-b identifier: %s", run, startInfo, body)
		}
		if count, _ := data["count"].(float64); count != 1 {
			t.Fatalf("run %d: count = %v, want the one cr-dup-2 -> repo-a path: %s", run, data["count"], body)
		}
	}
	gotCode, gotBody, _ := post(p("only-b"))
	wantCode, wantBody, _ := post(p("absent-name"))
	if gotCode != wantCode || strings.Replace(gotBody, p("only-b"), p("absent-name"), 1) != wantBody {
		t.Fatalf("ungranted-only name\n got %d %s\nwant %d %s (the unknown-name shape)", gotCode, gotBody, wantCode, wantBody)
	}
	t.Logf("scoped shared-name anchor resolved repo-a's node on every run; ungranted-only name renders as unknown")
}
