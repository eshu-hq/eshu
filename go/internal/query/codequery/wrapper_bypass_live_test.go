// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_wrapper_bypass

// Live proof for the #6838 wrapper-bypass read surface: the shipped family
// builders run against the pinned NornicDB build and the one-hop rows
// qualify the positive fixture end to end through the findings route, with
// per-read client latency logged. See liveWrapperLatency for why plan
// inspection is out of reach on the pinned build.
//
// Run against the pinned replay-tier proof image:
//
//	docker run -d --name nornic-6838 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17687:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//
//	cd go && go test ./internal/query/codequery -tags live_nornicdb_wrapper_bypass \
//	  -run TestLiveNornicDBWrapperBypass -count=1 -v
package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const liveWrapperRepo = "repo-wb-live"

// seedLiveWrapperBypassGraph writes a full wrapper family: w-a through w-e
// all delegate to target-t, w-a carries five fan-in callers, and x-bypass
// calls target-t directly from another package. MERGE keeps repeated runs
// against a retained store idempotent.
func seedLiveWrapperBypassGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		`MERGE (f:File {uid:"wb:file-other"}) SET f.id="wb:file-other", f.relative_path="other/client.go", f.repo_id="` + liveWrapperRepo + `"`,
		`MERGE (e:Function {uid:"target-t"}) SET e.id="target-t", e.name="recordAPICallImpl", e.repo_id="` + liveWrapperRepo + `", e.cyclomatic_complexity=30`,
		`MERGE (e:Function {uid:"x-bypass"}) SET e.id="x-bypass", e.name="callSite", e.repo_id="` + liveWrapperRepo + `", e.cyclomatic_complexity=30`,
		`MATCH (f:File {uid:"wb:file-other"}), (e:Function {uid:"x-bypass"}) MERGE (f)-[:CONTAINS]->(e)`,
		`MATCH (x:Function {uid:"x-bypass"}), (t:Function {uid:"target-t"}) MERGE (x)-[r:CALLS]->(t) SET r.confidence=0.85, r.resolution_method="declared"`,
	}
	for _, pkg := range []string{"a", "b", "c", "d", "e"} {
		uid := "w-" + pkg
		file := "wb:file-" + pkg
		// w-a fronts from pkg/wrap; the peers sit in their own service
		// packages so every bypass reads cross-package.
		path := "svc-" + pkg + "/telemetry.go"
		if pkg == "a" {
			path = "pkg/wrap/w.go"
		}
		statements = append(statements,
			`MERGE (f:File {uid:"`+file+`"}) SET f.id="`+file+`", f.relative_path="`+path+`", f.repo_id="`+liveWrapperRepo+`"`,
			`MERGE (e:Function {uid:"`+uid+`"}) SET e.id="`+uid+`", e.name="recordAPICall", e.repo_id="`+liveWrapperRepo+`", e.cyclomatic_complexity=2`,
			`MATCH (f:File {uid:"`+file+`"}), (e:Function {uid:"`+uid+`"}) MERGE (f)-[:CONTAINS]->(e)`,
			`MATCH (w:Function {uid:"`+uid+`"}), (t:Function {uid:"target-t"}) MERGE (w)-[r:CALLS]->(t) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	// Five fan-in callers behind w-a so the floor (3) clears with room for
	// the runner-up check.
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		statements = append(statements,
			`MERGE (e:Function {uid:"wb:fan`+n+`"}) SET e.id="wb:fan`+n+`", e.name="fan`+n+`", e.repo_id="`+liveWrapperRepo+`", e.cyclomatic_complexity=1`,
			`MATCH (c:Function {uid:"wb:fan`+n+`"}), (w:Function {uid:"w-a"}) MERGE (c)-[r:CALLS]->(w) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}

// liveWrapperLatency measures the client-side median of three runs per
// shipped read: relative seeded-scale evidence. Plan inspection is out of
// reach on the pinned build — neither Bolt summaries nor the HTTP
// transactional endpoint expose plans — so the anchored shape (UNWIND ids
// bound to indexed entity-id node patterns with repo/grant in the anchoring
// WHERE) is pinned by the white-box builder tests instead, and full
// indexed-repo PROFILE stays remote-gated like #6834 section 11.
func liveWrapperLatency(
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	label, cypher string,
	params map[string]any,
) {
	t.Helper()

	latencies := make([]time.Duration, 0, 3)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		started := time.Now()
		result, err := session.Run(ctx, cypher, params)
		if err != nil {
			cancel()
			t.Fatalf("%s run %d: %v", label, i, err)
		}
		if _, err := result.Collect(ctx); err != nil {
			cancel()
			t.Fatalf("%s collect %d: %v", label, i, err)
		}
		latencies = append(latencies, time.Since(started))
		cancel()
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("%s client_median=%s (seeded scale, n=3)", label, latencies[1])
}

// liveWrapperOpenDriver connects to the proof NornicDB over Bolt, defaulting
// to the local proof container.
func liveWrapperOpenDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17687"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver
}

// liveWrapperMembersByID resolves the live fixture's finding members: the
// five nominated wrappers plus the bypasser, keyed by the entity ids the
// live graph seeds.
func liveWrapperMembersByID() map[string]codedivergence.Member {
	out := map[string]codedivergence.Member{}
	for _, pkg := range []string{"a", "b", "c", "d", "e"} {
		uid := "w-" + pkg
		out[uid] = codedivergence.Member{
			EntityID: uid, EntityName: "recordAPICall", EntityType: "Function",
			RelativePath: "svc-" + pkg + "/telemetry.go", Language: "go",
			StartLine: 10, EndLine: 16, TokenCount: 12,
		}
	}
	out["x-bypass"] = codedivergence.Member{
		EntityID: "x-bypass", EntityName: "callSite", EntityType: "Function",
		RelativePath: "other/client.go", Language: "go", StartLine: 40, EndLine: 90, TokenCount: 200,
	}
	return out
}

// TestLiveNornicDBWrapperBypass runs the positive fixture through the
// findings route against live NornicDB and PROFILEs the three shipped
// family reads.
func TestLiveNornicDBWrapperBypass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := liveWrapperOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveWrapperBypassGraph(ctx, t, driver)

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        newLiveNornicDBReader(driver, "nornic"),
		Content:      wrapperFixtureStore{membersByID: liveWrapperMembersByID()},
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer func() { _ = session.Close(ctx) }()

	// Unscoped like the unauthenticated handler request below: the zero
	// filter would be scoped-empty and match nothing.
	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	calleesCypher, calleesParams := BuildWrapperFamilyCalleesCypher(
		[]string{"w-a", "w-b"}, liveWrapperRepo, querycontract.GraphBackendNornicDB, access)
	callersCypher, callersParams := BuildWrapperFamilyCallersCypher(
		[]string{"target-t"}, liveWrapperRepo, querycontract.GraphBackendNornicDB, access)
	fanInCypher, fanInParams := BuildWrapperFamilyFanInCypher(
		[]string{"w-a", "x-bypass"}, liveWrapperRepo, querycontract.GraphBackendNornicDB, access)

	runRead := func(label, cypher string, params map[string]any) []map[string]any {
		t.Helper()
		res, err := session.Run(ctx, cypher, params)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		records, err := res.Collect(ctx)
		if err != nil {
			t.Fatalf("collect %s: %v", label, err)
		}
		rows := make([]map[string]any, 0, len(records))
		for _, record := range records {
			row := map[string]any{}
			for _, key := range record.Keys {
				value, _ := record.Get(key)
				row[key] = value
			}
			rows = append(rows, row)
		}
		return rows
	}

	calleeRows := runRead("family callees", calleesCypher, calleesParams)
	delegations := map[string]bool{}
	for _, row := range calleeRows {
		delegations[StringVal(row, "source_id")+">"+StringVal(row, "id")] = true
	}
	for _, want := range []string{"w-a>target-t", "w-b>target-t"} {
		if !delegations[want] {
			t.Errorf("family callees miss delegation %s: %v", want, calleeRows)
		}
	}

	callerRows := runRead("family callers", callersCypher, callersParams)
	seen := map[string]bool{}
	for _, row := range callerRows {
		seen[StringVal(row, "id")] = true
		if StringVal(row, "target_id") != "target-t" {
			t.Errorf("caller row demux target_id = %q, want target-t", StringVal(row, "target_id"))
		}
	}
	for _, want := range []string{"w-a", "w-b", "w-c", "w-d", "w-e", "x-bypass"} {
		if !seen[want] {
			t.Errorf("family callers miss %s: %v", want, callerRows)
		}
	}

	fanInRows := runRead("family fan-in", fanInCypher, fanInParams)
	fanIn := map[string]int{}
	for _, row := range fanInRows {
		fanIn[StringVal(row, "id")] = IntVal(row, "fan_in")
	}
	if fanIn["w-a"] != 5 {
		t.Errorf("wrapper fan-in = %d, want exactly 5: %v", fanIn["w-a"], fanInRows)
	}

	liveWrapperLatency(t, session, "family-callees", calleesCypher, calleesParams)
	liveWrapperLatency(t, session, "family-callers", callersCypher, callersParams)
	liveWrapperLatency(t, session, "family-fan-in", fanInCypher, fanInParams)

	// Full route through the handler: the content fake nominates the five
	// wrappers, the live rows qualify target-t with w-a canonical.
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(`{"repo_id":"repo-wb-live","kind":"wrapper_bypass"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("findings status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Findings []struct {
				Fingerprint string `json:"fingerprint"`
				Members     []struct {
					EntityID string `json:"entity_id"`
				} `json:"members"`
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data.Findings) != 1 {
		t.Fatalf("live findings = %d, want 1 body=%s", len(envelope.Data.Findings), rec.Body.String())
	}
	finding := envelope.Data.Findings[0]
	if finding.Fingerprint != "target-t" {
		t.Errorf("live fingerprint = %q, want target-t", finding.Fingerprint)
	}
	if len(finding.Members) != 6 {
		t.Errorf("live members = %d, want 6 (wrapper plus five bypassers)", len(finding.Members))
	}
	if finding.Members[0].EntityID != "w-a" {
		t.Errorf("live first member = %q, want canonical w-a first", finding.Members[0].EntityID)
	}
}
