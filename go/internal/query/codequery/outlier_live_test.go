// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_convention_outlier

// Live proof for the #6839 convention-outlier read surface: the shipped
// cohort builders run against live graph backends and the positive fixture
// qualifies end to end through the findings route, with per-read client
// latency logged. See liveOutlierLatency for why plan inspection is out of
// reach on the pinned build.
//
// Run against the retained proof NornicDB (seed prefix co:, no collision
// with the wb: seeds):
//
//	cd go && go test ./internal/query/codequery -tags live_nornicdb_convention_outlier \
//	  -run 'TestLiveNornicDBConventionOutlier|TestLiveOutlierBackendParity' -count=1 -v
//
// Backend parity runs the same test twice, once per container:
//
//	ESHU_LIVE_GRAPH_BACKEND=nornicdb ESHU_NEO4J_URI=bolt://localhost:17687 ...
//	ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_NEO4J_URI=bolt://127.0.0.1:27687 ...
package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

const liveOutlierRepo = "repo-co-live"

// liveOutlierSeed writes the positive fixture: five handlers on the
// /widgets mount (four calling the guard, h-5 mediated through a wrapper),
// two implementers of one interface with a 2/3 guard majority on an
// inferred edge, and package cohorts mirroring both. MERGE keeps repeated
// runs against a retained store idempotent; the co: prefix never collides
// with other live seeds.
func liveOutlierSeed() []string {
	repo := liveOutlierRepo
	statements := []string{
		`MERGE (f:File {uid:"co:file-api"}) SET f.id="co:file-api", f.relative_path="api/widgets.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"co:file-impla"}) SET f.id="co:file-impla", f.relative_path="impl/a.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"co:file-implb"}) SET f.id="co:file-implb", f.relative_path="impl/b.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"co:file-other"}) SET f.id="co:file-other", f.relative_path="other/wrap.go", f.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"co:guard"}) SET e.id="co:guard", e.name="requireAuth", e.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"co:wrap"}) SET e.id="co:wrap", e.name="checkedHandler", e.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"co:log"}) SET e.id="co:log", e.name="logHelper", e.repo_id="` + repo + `"`,
		`MERGE (e:Interface {uid:"co:iface-a"}) SET e.id="co:iface-a", e.name="Handler", e.repo_id="` + repo + `"`,
		`MERGE (e:Class {uid:"co:impl-a"}) SET e.id="co:impl-a", e.name="ImplA", e.repo_id="` + repo + `"`,
		`MERGE (e:Class {uid:"co:impl-b"}) SET e.id="co:impl-b", e.name="ImplB", e.repo_id="` + repo + `"`,
		`MERGE (e:Endpoint {uid:"co:ep-w"}) SET e.id="co:ep-w", e.repo_id="` + repo + `", e.path="/widgets"`,
		`MERGE (e:Endpoint {uid:"co:ep-wid"}) SET e.id="co:ep-wid", e.repo_id="` + repo + `", e.path="/widgets/123"`,
		`MATCH (f:File {uid:"co:file-other"}), (e:Function {uid:"co:wrap"}) MERGE (f)-[:CONTAINS]->(e)`,
		`MATCH (a:Class {uid:"co:impl-a"}), (i:Interface {uid:"co:iface-a"}) MERGE (a)-[:IMPLEMENTS]->(i)`,
		`MATCH (b:Class {uid:"co:impl-b"}), (i:Interface {uid:"co:iface-a"}) MERGE (b)-[:IMPLEMENTS]->(i)`,
	}
	for _, h := range []string{"1", "2", "3", "4", "5"} {
		uid := "co:h-" + h
		statements = append(statements,
			`MERGE (e:Function {uid:"`+uid+`"}) SET e.id="`+uid+`", e.name="handle", e.repo_id="`+repo+`"`,
			`MATCH (f:File {uid:"co:file-api"}), (e:Function {uid:"`+uid+`"}) MERGE (f)-[:CONTAINS]->(e)`,
		)
	}
	for _, h := range []string{"1", "2", "3", "4"} {
		statements = append(statements,
			`MATCH (h:Function {uid:"co:h-`+h+`"}), (g:Function {uid:"co:guard"}) MERGE (h)-[r:CALLS]->(g) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	for _, h := range []string{"1", "2", "3"} {
		statements = append(statements,
			`MATCH (h:Function {uid:"co:h-`+h+`"}), (e:Endpoint {uid:"co:ep-w"}) MERGE (h)-[r:HANDLES_ROUTE]->(e) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	for _, h := range []string{"4", "5"} {
		statements = append(statements,
			`MATCH (h:Function {uid:"co:h-`+h+`"}), (e:Endpoint {uid:"co:ep-wid"}) MERGE (h)-[r:HANDLES_ROUTE]->(e) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	methods := []struct{ uid, file, impl, method string }{
		{"co:m-a1", "co:file-impla", "co:impl-a", "declared"},
		{"co:m-a2", "co:file-impla", "co:impl-a", "scope_unique_name"},
		{"co:m-b1", "co:file-implb", "co:impl-b", "declared"},
	}
	for _, m := range methods {
		confidence := "0.9"
		if m.method == "scope_unique_name" {
			confidence = "0.7"
		}
		statements = append(statements,
			`MERGE (e:Function {uid:"`+m.uid+`"}) SET e.id="`+m.uid+`", e.name="serve", e.repo_id="`+repo+`"`,
			`MATCH (f:File {uid:"`+m.file+`"}), (e:Function {uid:"`+m.uid+`"}) MERGE (f)-[:CONTAINS]->(e)`,
			`MATCH (c:Class {uid:"`+m.impl+`"}), (e:Function {uid:"`+m.uid+`"}) MERGE (c)-[:CONTAINS]->(e)`,
		)
		if m.uid != "co:m-b1" {
			statements = append(statements,
				`MATCH (h:Function {uid:"`+m.uid+`"}), (g:Function {uid:"co:guard"}) MERGE (h)-[r:CALLS]->(g) SET r.confidence=`+confidence+`, r.resolution_method="`+m.method+`"`,
			)
		}
	}
	// Cross edges last: every endpoint exists by now, so no MATCH binds
	// empty and silently writes nothing.
	statements = append(statements,
		`MATCH (m:Function {uid:"co:wrap"}), (g:Function {uid:"co:guard"}) MERGE (m)-[r:CALLS]->(g) SET r.confidence=0.9, r.resolution_method="declared"`,
		`MATCH (h:Function {uid:"co:h-5"}), (w:Function {uid:"co:wrap"}) MERGE (h)-[r:CALLS]->(w) SET r.confidence=0.9, r.resolution_method="declared"`,
		`MATCH (h:Function {uid:"co:m-b1"}), (l:Function {uid:"co:log"}) MERGE (h)-[r:CALLS]->(l) SET r.confidence=0.9, r.resolution_method="declared"`,
	)
	return statements
}

func liveOutlierOpenDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
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

func liveOutlierSeedGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database string) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: database,
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()
	run := func(stmt string) {
		t.Helper()
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
	for _, stmt := range liveOutlierSeed() {
		run(stmt)
	}
}

// liveOutlierLatency measures the client-side median of three runs per
// shipped read: relative seeded-scale evidence. Plan inspection is out of
// reach on the pinned build (neither Bolt summaries nor the HTTP
// transactional endpoint expose plans), so the anchored shape (Function
// anchor with repo/grant in the anchoring WHERE, exactly one hop) is pinned
// by the white-box builder tests instead, and full indexed-repo PROFILE
// stays remote-gated like #6834 section 11.
func liveOutlierLatency(
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

func liveOutlierMembersByID() map[string]codedivergence.Member {
	out := map[string]codedivergence.Member{}
	paths := map[string]string{}
	for _, h := range []string{"1", "2", "3", "4", "5"} {
		paths["co:h-"+h] = "api/widgets.go"
	}
	paths["co:m-a1"], paths["co:m-a2"] = "impl/a.go", "impl/a.go"
	paths["co:m-b1"] = "impl/b.go"
	line := 10
	for _, id := range []string{"co:h-1", "co:h-2", "co:h-3", "co:h-4", "co:h-5", "co:m-a1", "co:m-a2", "co:m-b1"} {
		out[id] = codedivergence.Member{
			EntityID: id, EntityName: "handle", EntityType: "Function",
			RelativePath: paths[id], Language: "go",
			StartLine: line, EndLine: line + 15, TokenCount: 120,
		}
		line += 20
	}
	return out
}

func liveOutlierRunRead(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	label, cypher string,
	params map[string]any,
) []map[string]any {
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

// liveOutlierCanonicalFindings drives the findings route and returns one
// canonical string per finding: cohort source/key, callee, share,
// confidence, inferred, outliers, mediated. Both backends must produce the
// same four strings.
func liveOutlierCanonicalFindings(t *testing.T, handler *CodeHandler) []string {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(`{"repo_id":"repo-co-live","kind":"convention_outlier"}`),
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
				Kind        string   `json:"kind"`
				Fingerprint string   `json:"fingerprint"`
				Share       float64  `json:"share"`
				Confidence  float64  `json:"confidence"`
				Inferred    bool     `json:"inferred"`
				Outliers    []string `json:"outliers"`
				Cohort      struct {
					Source string `json:"source"`
					Key    string `json:"key"`
				} `json:"cohort"`
				MajorityCallee struct {
					EntityID string `json:"entity_id"`
				} `json:"majority_callee"`
				Mediated []struct {
					OutlierID  string `json:"outlier_id"`
					MediatorID string `json:"mediator_id"`
				} `json:"mediated_outliers"`
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	canonical := make([]string, 0, len(envelope.Data.Findings))
	for _, finding := range envelope.Data.Findings {
		outliers := append([]string(nil), finding.Outliers...)
		sort.Strings(outliers)
		mediated := []string{}
		for _, mediation := range finding.Mediated {
			mediated = append(mediated, mediation.OutlierID+">"+mediation.MediatorID)
		}
		sort.Strings(mediated)
		canonical = append(canonical, fmt.Sprintf(
			"%s|%s|%s|%s|%.2f|%.2f|%v|%v|%v",
			finding.Kind, finding.Cohort.Source, finding.Cohort.Key,
			finding.MajorityCallee.EntityID, finding.Share, finding.Confidence,
			finding.Inferred, outliers, mediated,
		))
		_ = finding.Fingerprint
	}
	sort.Strings(canonical)
	return canonical
}

// TestLiveNornicDBConventionOutlier runs the positive fixture through the
// shipped reads and the findings route against live NornicDB.
func TestLiveNornicDBConventionOutlier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := liveOutlierOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	liveOutlierSeedGraph(ctx, t, driver, "nornic")

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        newLiveNornicDBReader(driver, "nornic"),
		Content:      outlierFixtureStore{membersByID: liveOutlierMembersByID()},
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer func() { _ = session.Close(ctx) }()

	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	interfaceCypher, interfaceParams := BuildOutlierCohortsCypher(
		codedivergence.CohortInterface, liveOutlierRepo, querycontract.GraphBackendNornicDB, access)
	routerCypher, routerParams := BuildOutlierCohortsCypher(
		codedivergence.CohortRouter, liveOutlierRepo, querycontract.GraphBackendNornicDB, access)
	packageCypher, packageParams := BuildOutlierCohortsCypher(
		codedivergence.CohortPackage, liveOutlierRepo, querycontract.GraphBackendNornicDB, access)

	routerRows := liveOutlierRunRead(ctx, t, session, "router cohorts", routerCypher, routerParams)
	routerMembers := map[string]bool{}
	for _, row := range routerRows {
		routerMembers[StringVal(row, "member_id")] = true
	}
	for _, want := range []string{"co:h-1", "co:h-2", "co:h-3", "co:h-4", "co:h-5"} {
		if !routerMembers[want] {
			t.Errorf("router cohorts miss %s: %v", want, routerRows)
		}
	}

	interfaceRows := liveOutlierRunRead(ctx, t, session, "interface cohorts", interfaceCypher, interfaceParams)
	interfaceMembers := map[string]bool{}
	for _, row := range interfaceRows {
		interfaceMembers[StringVal(row, "member_id")] = true
		if StringVal(row, "iface_id") != "co:iface-a" {
			t.Errorf("interface row iface = %q, want co:iface-a", StringVal(row, "iface_id"))
		}
	}
	for _, want := range []string{"co:m-a1", "co:m-a2", "co:m-b1"} {
		if !interfaceMembers[want] {
			t.Errorf("interface cohorts miss %s: %v", want, interfaceRows)
		}
	}

	packageRows := liveOutlierRunRead(ctx, t, session, "package cohorts", packageCypher, packageParams)
	packageMembers := map[string]int{}
	for _, row := range packageRows {
		packageMembers[StringVal(row, "member_id")]++
	}
	for _, want := range []string{"co:h-1", "co:m-a1", "co:m-b1"} {
		if packageMembers[want] != 1 {
			t.Errorf("package cohorts hold %s %d times, want exactly once: %v", want, packageMembers[want], packageRows)
		}
	}

	allMembers := []string{"co:h-1", "co:h-2", "co:h-3", "co:h-4", "co:h-5", "co:m-a1", "co:m-a2", "co:m-b1", "co:wrap"}
	edgesCypher, edgesParams := BuildOutlierCalleeEdgesCypher(
		allMembers, liveOutlierRepo, querycontract.GraphBackendNornicDB, access)
	edgeRows := liveOutlierRunRead(ctx, t, session, "callee edges", edgesCypher, edgesParams)
	pairs := map[string]string{}
	for _, row := range edgeRows {
		pairs[StringVal(row, "member_id")+">"+StringVal(row, "callee_id")] = StringVal(row, "edge_method")
	}
	for _, want := range []string{
		"co:h-1>co:guard", "co:h-2>co:guard", "co:h-3>co:guard", "co:h-4>co:guard",
		"co:h-5>co:wrap", "co:wrap>co:guard", "co:m-a1>co:guard", "co:m-a2>co:guard", "co:m-b1>co:log",
	} {
		if _, ok := pairs[want]; !ok {
			t.Errorf("callee edges miss %s: %v", want, edgeRows)
		}
	}
	if pairs["co:m-a2>co:guard"] != "scope_unique_name" {
		t.Errorf("m-a2 edge method = %q, want scope_unique_name", pairs["co:m-a2>co:guard"])
	}

	// Same-engine dialect parity: the Neo4j-rendered callee-edges read
	// returns the same row set as the NornicDB rendering.
	neoEdgesCypher, neoEdgesParams := BuildOutlierCalleeEdgesCypher(
		allMembers, liveOutlierRepo, querycontract.GraphBackendNeo4j, access)
	neoEdgeRows := liveOutlierRunRead(ctx, t, session, "neo4j-dialect callee edges", neoEdgesCypher, neoEdgesParams)
	neoPairs := map[string]bool{}
	for _, row := range neoEdgeRows {
		neoPairs[StringVal(row, "member_id")+">"+StringVal(row, "callee_id")] = true
	}
	for pair := range pairs {
		if !neoPairs[pair] {
			t.Errorf("neo4j dialect misses pair %s: %v", pair, neoEdgeRows)
		}
	}
	for pair := range neoPairs {
		if _, ok := pairs[pair]; !ok {
			t.Errorf("neo4j dialect adds pair %s: %v", pair, neoEdgeRows)
		}
	}

	liveOutlierLatency(t, session, "cohorts-interface", interfaceCypher, interfaceParams)
	liveOutlierLatency(t, session, "cohorts-router", routerCypher, routerParams)
	liveOutlierLatency(t, session, "cohorts-package", packageCypher, packageParams)
	liveOutlierLatency(t, session, "callee-edges", edgesCypher, edgesParams)

	canonical := liveOutlierCanonicalFindings(t, handler)
	want := []string{
		"parallel_implementation.convention_outlier|interface|co:iface-a|co:guard|0.67|0.70|true|[co:m-b1]|[]",
		"parallel_implementation.convention_outlier|package|api|co:guard|0.80|0.90|false|[co:h-5]|[co:h-5>co:wrap]",
		"parallel_implementation.convention_outlier|package|impl|co:guard|0.67|0.70|true|[co:m-b1]|[]",
		"parallel_implementation.convention_outlier|router|widgets|co:guard|0.80|0.90|false|[co:h-5]|[co:h-5>co:wrap]",
	}
	if len(canonical) != len(want) {
		t.Fatalf("canonical findings = %d, want %d: %v", len(canonical), len(want), canonical)
	}
	for i := range want {
		if canonical[i] != want[i] {
			t.Errorf("finding %d = %q, want %q", i, canonical[i], want[i])
		}
	}

	// Investigate round-trips every emitted fingerprint.
	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, fingerprint := range liveOutlierFingerprints(t, handler) {
		body, _ := json.Marshal(map[string]any{
			"repo_id": liveOutlierRepo, "kind": "convention_outlier", "fingerprint": fingerprint,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/divergence/investigate", bytes.NewBuffer(body))
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("investigate %q status = %d, want 200 body=%s", fingerprint, rec.Code, rec.Body.String())
		}
	}
}

func liveOutlierFingerprints(t *testing.T, handler *CodeHandler) []string {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(`{"repo_id":"repo-co-live","kind":"convention_outlier"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var envelope struct {
		Data struct {
			Findings []struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	fingerprints := make([]string, 0, len(envelope.Data.Findings))
	for _, finding := range envelope.Data.Findings {
		fingerprints = append(fingerprints, finding.Fingerprint)
	}
	return fingerprints
}

// TestLiveOutlierBackendParity runs the same findings proof on the backend
// named by ESHU_LIVE_GRAPH_BACKEND: identical canonical findings on
// NornicDB and Neo4j-compat.
func TestLiveOutlierBackendParity(t *testing.T) {
	backendName := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backendName != "nornicdb" && backendName != "neo4j" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	database := "nornic"
	graphBackend := GraphBackendNornicDB
	if backendName == "neo4j" {
		database = "neo4j"
		graphBackend = GraphBackendNeo4j
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := liveOutlierOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	liveOutlierSeedGraph(ctx, t, driver, database)

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: graphBackend,
		Neo4j:        newLiveNornicDBReader(driver, database),
		Content:      outlierFixtureStore{membersByID: liveOutlierMembersByID()},
	}
	canonical := liveOutlierCanonicalFindings(t, handler)
	want := []string{
		"parallel_implementation.convention_outlier|interface|co:iface-a|co:guard|0.67|0.70|true|[co:m-b1]|[]",
		"parallel_implementation.convention_outlier|package|api|co:guard|0.80|0.90|false|[co:h-5]|[co:h-5>co:wrap]",
		"parallel_implementation.convention_outlier|package|impl|co:guard|0.67|0.70|true|[co:m-b1]|[]",
		"parallel_implementation.convention_outlier|router|widgets|co:guard|0.80|0.90|false|[co:h-5]|[co:h-5>co:wrap]",
	}
	if len(canonical) != len(want) {
		t.Fatalf("backend %s findings = %d, want %d: %v", backendName, len(canonical), len(want), canonical)
	}
	for i := range want {
		if canonical[i] != want[i] {
			t.Errorf("backend %s finding %d = %q, want %q", backendName, i, canonical[i], want[i])
		}
	}
}
