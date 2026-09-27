// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Repo-scale live proof for #6929: the convention-outlier cohort-read fix
// (outlierScopePredicates dropping coalesce() on the repo-scope predicate)
// against a real Neo4j backend sized to the platform-scale shape the issue
// names -- one swept repository at 45,000+ Function members plus 450,000+
// Function nodes spread across other repositories, so the old
// NodeByLabelScan cost (proportional to the WHOLE graph) is distinguishable
// from the new NodeIndexSeek cost (proportional to the swept repository
// alone). Skipped unless ESHU_OUTLIER_NEO4J_LIVE=1: seeding ~500k nodes
// takes real wall time, so this is a scheduled, not CI-blocking, proof (see
// specs/live-tests.v1.yaml).
//
// Run against the container the #6929 evidence doc names:
//
//	docker run -d --name eshu-6929-neo4j -p 17929:7687 \
//	  -e NEO4J_AUTH=neo4j/eshu-6929-pass neo4j:2026-community
//	ESHU_OUTLIER_NEO4J_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17929 \
//	  ESHU_NEO4J_USER=neo4j ESHU_NEO4J_PASSWORD=eshu-6929-pass \
//	  go test ./internal/query/codequery -run TestLiveOutlierRepoScaleNeo4j -v -count=1 -timeout 20m
//	docker rm -f eshu-6929-neo4j
package codequery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	liveScaleRepo         = "repo-6929-scale"
	liveScaleSignalPrefix = "s6929"
	liveScaleNoiseFiles   = 901
	liveScaleFuncsPerFile = 50
	liveScaleOtherRepos   = 460
	liveScaleOtherPerRepo = 1000
	liveScaleWriteBatch   = 5000
	// liveScaleMinTargetFunctions and liveScaleMinOtherFunctions are the
	// #6929 handoff's acceptance floors: >=45,000 Functions in the swept
	// repository, >=450,000 Functions in the rest of the graph.
	liveScaleMinTargetFunctions = 45000
	liveScaleMinOtherFunctions  = 450000
)

func liveScaleSkipUnset(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ESHU_OUTLIER_NEO4J_LIVE")) == "" {
		t.Skip("set ESHU_OUTLIER_NEO4J_LIVE=1 to run the #6929 repo-scale Neo4j proof")
	}
}

func liveScaleOpenDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17929"
	}
	user := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USER"))
	if user == "" {
		user = "neo4j"
	}
	password := strings.TrimSpace(os.Getenv("ESHU_NEO4J_PASSWORD"))
	if password == "" {
		password = "eshu-6929-pass"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.BasicAuth(user, password, ""))
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver
}

// liveScaleSchemaExecutor adapts the Neo4j driver to graph.CypherExecutor so
// the real production schema (go/internal/graph) applies to the live
// container -- the same DDL, including the function_repo_id RANGE index
// this fix depends on, that a production deployment applies.
type liveScaleSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *liveScaleSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func liveScaleApplySchema(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database string) {
	t.Helper()

	executor := &liveScaleSchemaExecutor{driver: driver, database: database}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := graph.EnsureSchemaWithBackendStrict(ctx, executor, logger, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply neo4j schema: %v", err)
	}
	liveScaleWaitForIndexOnline(ctx, t, driver, database, "function_repo_id")
}

func liveScaleWaitForIndexOnline(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, indexName string) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		result, err := session.Run(ctx, "SHOW INDEXES YIELD name, state WHERE name = $name RETURN state", map[string]any{"name": indexName})
		if err == nil {
			records, collectErr := result.Collect(ctx)
			if collectErr == nil && len(records) > 0 {
				if state, _ := records[0].Get("state"); state == "ONLINE" {
					return
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("index %s did not reach ONLINE before the wait deadline", indexName)
}

func liveScaleRunWrite(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, label, cypher string, params map[string]any) {
	t.Helper()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("%s consume: %v", label, err)
	}
}

// liveScaleNoiseFileRow is one seeded :File row: each lives in its own
// directory so codedivergence.PackageOf groups it into its own package
// cohort of exactly liveScaleFuncsPerFile members -- the same file-level
// cohort shape diag-6929 measured on ops-qa's largest repo.
type liveScaleNoiseFileRow struct {
	UID  string
	Path string
}

func liveScaleNoiseFileRows() []liveScaleNoiseFileRow {
	rows := make([]liveScaleNoiseFileRow, 0, liveScaleNoiseFiles)
	for i := 0; i < liveScaleNoiseFiles; i++ {
		rows = append(rows, liveScaleNoiseFileRow{
			UID:  fmt.Sprintf("scale6929:file:%05d", i),
			Path: fmt.Sprintf("pkg%05d/file.go", i),
		})
	}
	return rows
}

func liveScaleSeedNoiseFiles(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) {
	t.Helper()

	rows := liveScaleNoiseFileRows()
	batch := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		batch = append(batch, map[string]any{"uid": r.UID, "path": r.Path})
	}
	cypher := `
		UNWIND $rows AS row
		CREATE (f:File {uid: row.uid, id: row.uid, relative_path: row.path, repo_id: $repo_id})
	`
	liveScaleRunWrite(ctx, t, session, "seed noise files", cypher, map[string]any{"rows": batch, "repo_id": liveScaleRepo})
}

// liveScaleSeedNoiseFunctions seeds liveScaleNoiseFiles*liveScaleFuncsPerFile
// Function nodes in the swept repository, each CONTAINS'd by its own File,
// with no CALLS/IMPLEMENTS/HANDLES_ROUTE edges: realistic filler that costs
// the package-cohort enumeration a row without ever qualifying as an
// outlier. Returns the count seeded.
func liveScaleSeedNoiseFunctions(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) int {
	t.Helper()

	type funcRow struct {
		FileUID, UID, Name string
	}
	all := make([]funcRow, 0, liveScaleNoiseFiles*liveScaleFuncsPerFile)
	for _, f := range liveScaleNoiseFileRows() {
		for j := 0; j < liveScaleFuncsPerFile; j++ {
			all = append(all, funcRow{
				FileUID: f.UID,
				UID:     fmt.Sprintf("%s:fn:%03d", f.UID, j),
				Name:    fmt.Sprintf("Fn%03d", j),
			})
		}
	}
	cypher := `
		UNWIND $rows AS row
		MATCH (file:File {uid: row.file_uid})
		CREATE (fn:Function {uid: row.uid, id: row.uid, name: row.name, repo_id: $repo_id})
		CREATE (file)-[:CONTAINS]->(fn)
	`
	for start := 0; start < len(all); start += liveScaleWriteBatch {
		end := start + liveScaleWriteBatch
		if end > len(all) {
			end = len(all)
		}
		batch := make([]map[string]any, 0, end-start)
		for _, r := range all[start:end] {
			batch = append(batch, map[string]any{"file_uid": r.FileUID, "uid": r.UID, "name": r.Name})
		}
		liveScaleRunWrite(ctx, t, session, fmt.Sprintf("seed noise functions [%d:%d]", start, end),
			cypher, map[string]any{"rows": batch, "repo_id": liveScaleRepo})
	}
	return len(all)
}

// liveScaleSeedOtherRepoFunctions seeds liveScaleOtherRepos *
// liveScaleOtherPerRepo Function nodes across other repositories: no
// relationships, since the enumeration's WHERE filters them out before any
// further MATCH expansion (the point of the fix is that these rows are no
// longer scanned at all once the predicate is index-servable). Returns the
// count seeded.
func liveScaleSeedOtherRepoFunctions(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) int {
	t.Helper()

	cypher := `
		UNWIND $rows AS row
		CREATE (fn:Function {uid: row.uid, id: row.uid, name: row.name, repo_id: row.repo_id})
	`
	total := 0
	batch := make([]map[string]any, 0, liveScaleWriteBatch)
	flush := func(idx int) {
		if len(batch) == 0 {
			return
		}
		liveScaleRunWrite(ctx, t, session, fmt.Sprintf("seed other-repo functions batch %d", idx), cypher, map[string]any{"rows": batch})
		total += len(batch)
		batch = batch[:0]
	}
	batchIdx := 0
	for r := 0; r < liveScaleOtherRepos; r++ {
		repoID := fmt.Sprintf("repo-6929-other-%03d", r)
		for j := 0; j < liveScaleOtherPerRepo; j++ {
			batch = append(batch, map[string]any{
				"uid":     fmt.Sprintf("scale6929:other:%03d:%04d", r, j),
				"name":    fmt.Sprintf("OFn%04d", j),
				"repo_id": repoID,
			})
			if len(batch) >= liveScaleWriteBatch {
				flush(batchIdx)
				batchIdx++
			}
		}
	}
	flush(batchIdx)
	return total
}

// liveScaleSignalSeedStatements seeds one small positive-signal fixture in
// the swept repository, adapted from the co: fixture in
// outlier_live_test.go (prefix s6929: instead of co:, repo liveScaleRepo
// instead of repo-co-live): five /signalwidgets handlers (four calling a
// guard directly, one mediated through a wrapper) and two implementers of
// one interface with a 2/3 guard majority on an inferred edge. It gives all
// three cohort sources -- package, interface, router -- at least one
// nonempty, non-noise cohort with a real majority-callee pattern, so the
// full sweep exercises mediation and content hydration, not just an empty
// scan of noise.
func liveScaleSignalSeedStatements() []string {
	repo, p := liveScaleRepo, liveScaleSignalPrefix
	statements := []string{
		`MERGE (f:File {uid:"` + p + `:file-api"}) SET f.id="` + p + `:file-api", f.relative_path="signalpkg/api.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"` + p + `:file-impla"}) SET f.id="` + p + `:file-impla", f.relative_path="signalpkg/impl/a.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"` + p + `:file-implb"}) SET f.id="` + p + `:file-implb", f.relative_path="signalpkg/impl/b.go", f.repo_id="` + repo + `"`,
		`MERGE (f:File {uid:"` + p + `:file-other"}) SET f.id="` + p + `:file-other", f.relative_path="signalpkg/other/wrap.go", f.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"` + p + `:guard"}) SET e.id="` + p + `:guard", e.name="requireAuth", e.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"` + p + `:wrap"}) SET e.id="` + p + `:wrap", e.name="checkedHandler", e.repo_id="` + repo + `"`,
		`MERGE (e:Function {uid:"` + p + `:log"}) SET e.id="` + p + `:log", e.name="logHelper", e.repo_id="` + repo + `"`,
		`MERGE (e:Interface {uid:"` + p + `:iface-a"}) SET e.id="` + p + `:iface-a", e.name="Handler", e.repo_id="` + repo + `"`,
		`MERGE (e:Class {uid:"` + p + `:impl-a"}) SET e.id="` + p + `:impl-a", e.name="ImplA", e.repo_id="` + repo + `"`,
		`MERGE (e:Class {uid:"` + p + `:impl-b"}) SET e.id="` + p + `:impl-b", e.name="ImplB", e.repo_id="` + repo + `"`,
		`MERGE (e:Endpoint {uid:"` + p + `:ep-w"}) SET e.id="` + p + `:ep-w", e.repo_id="` + repo + `", e.path="/signalwidgets"`,
		`MATCH (f:File {uid:"` + p + `:file-other"}), (e:Function {uid:"` + p + `:wrap"}) MERGE (f)-[:CONTAINS]->(e)`,
		`MATCH (a:Class {uid:"` + p + `:impl-a"}), (i:Interface {uid:"` + p + `:iface-a"}) MERGE (a)-[:IMPLEMENTS]->(i)`,
		`MATCH (b:Class {uid:"` + p + `:impl-b"}), (i:Interface {uid:"` + p + `:iface-a"}) MERGE (b)-[:IMPLEMENTS]->(i)`,
	}
	for _, h := range []string{"1", "2", "3", "4", "5"} {
		uid := p + ":h-" + h
		statements = append(statements,
			`MERGE (e:Function {uid:"`+uid+`"}) SET e.id="`+uid+`", e.name="handle", e.repo_id="`+repo+`"`,
			`MATCH (f:File {uid:"`+p+`:file-api"}), (e:Function {uid:"`+uid+`"}) MERGE (f)-[:CONTAINS]->(e)`,
		)
	}
	for _, h := range []string{"1", "2", "3", "4"} {
		statements = append(statements,
			`MATCH (h:Function {uid:"`+p+`:h-`+h+`"}), (g:Function {uid:"`+p+`:guard"}) MERGE (h)-[r:CALLS]->(g) SET r.confidence=0.9, r.resolution_method="declared"`,
			`MATCH (h:Function {uid:"`+p+`:h-`+h+`"}), (e:Endpoint {uid:"`+p+`:ep-w"}) MERGE (h)-[r:HANDLES_ROUTE]->(e) SET r.confidence=0.9, r.resolution_method="declared"`,
		)
	}
	methods := []struct{ uid, file, impl, method string }{
		{p + ":m-a1", p + ":file-impla", p + ":impl-a", "declared"},
		{p + ":m-a2", p + ":file-impla", p + ":impl-a", "scope_unique_name"},
		{p + ":m-b1", p + ":file-implb", p + ":impl-b", "declared"},
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
		if m.uid != p+":m-b1" {
			statements = append(statements,
				`MATCH (h:Function {uid:"`+m.uid+`"}), (g:Function {uid:"`+p+`:guard"}) MERGE (h)-[r:CALLS]->(g) SET r.confidence=`+confidence+`, r.resolution_method="`+m.method+`"`,
			)
		}
	}
	statements = append(statements,
		`MATCH (m:Function {uid:"`+p+`:wrap"}), (g:Function {uid:"`+p+`:guard"}) MERGE (m)-[r:CALLS]->(g) SET r.confidence=0.9, r.resolution_method="declared"`,
		`MATCH (h:Function {uid:"`+p+`:h-5"}), (w:Function {uid:"`+p+`:wrap"}) MERGE (h)-[r:CALLS]->(w) SET r.confidence=0.9, r.resolution_method="declared"`,
		`MATCH (h:Function {uid:"`+p+`:m-b1"}), (l:Function {uid:"`+p+`:log"}) MERGE (h)-[r:CALLS]->(l) SET r.confidence=0.9, r.resolution_method="declared"`,
	)
	return statements
}

// liveScaleSeedGraph seeds noise files/functions, the other-repo Function
// population, and the signal fixture, in that order (files before the
// functions that CONTAINS-anchor to them, endpoints/interfaces before the
// edges that reference them).
func liveScaleSeedGraph(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext) (targetFunctions, otherFunctions int) {
	t.Helper()

	liveScaleSeedNoiseFiles(ctx, t, session)
	noise := liveScaleSeedNoiseFunctions(ctx, t, session)
	for i, stmt := range liveScaleSignalSeedStatements() {
		liveScaleRunWrite(ctx, t, session, fmt.Sprintf("seed signal fixture statement %d", i), stmt, nil)
	}
	other := liveScaleSeedOtherRepoFunctions(ctx, t, session)
	const signalFunctionCount = 11 // guard, wrap, log, h-1..h-5, m-a1, m-a2, m-b1
	return noise + signalFunctionCount, other
}

func liveScaleCount(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, label, cypher string, params map[string]any) int {
	t.Helper()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("%s collect: %v", label, err)
	}
	if len(records) == 0 {
		return 0
	}
	v, _ := records[0].Get("c")
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// legacyBuildOutlierCohortsCypher reproduces the pre-#6929 statement text
// (the old coalesce(member.repo_id, "") = $repo_id) for measurement only: production
// no longer emits this shape, but the before/after proof needs the exact
// old bytes to compare against BuildOutlierCohortsCypher's current output.
// Mirrors BuildOutlierCohortsCypher's structure with the single predicate
// difference under test; only the unscoped (AllScopes) shape is needed
// here, matching the access filter this live proof drives the handler
// with.
func legacyBuildOutlierCohortsCypher(source codedivergence.CohortSource, repoID string) (string, map[string]any) {
	params := map[string]any{"repo_id": repoID}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (member:Function)")
	cypher.WriteString("\n\t\tWHERE coalesce(member.repo_id, '') = $repo_id")
	switch source {
	case codedivergence.CohortInterface:
		cypher.WriteString("\n\t\tMATCH (member)<-[:CONTAINS]-(impl)-[:IMPLEMENTS]->(iface)")
		cypher.WriteString("\n\t\tRETURN coalesce(iface.id, iface.uid) as iface_id,\n")
		cypher.WriteString("\t\t       iface.name as iface_name,\n")
	case codedivergence.CohortRouter:
		cypher.WriteString("\n\t\tMATCH (member)-[:HANDLES_ROUTE]->(ep:Endpoint)")
		cypher.WriteString("\n\t\tRETURN ep.path as endpoint_path,\n")
	default:
		cypher.WriteString("\n\t\tMATCH (member)<-[:CONTAINS]-(containerFile:File)")
		cypher.WriteString("\n\t\tRETURN containerFile.relative_path as file_path,\n")
	}
	cypher.WriteString("\t\t       coalesce(member.id, member.uid) as member_id,\n")
	cypher.WriteString("\t\t       member.name as member_name\n")
	return cypher.String(), params
}

func liveScalePlanOperators(p neo4jdriver.Plan) []string {
	if p == nil {
		return nil
	}
	ops := []string{p.Operator()}
	for _, child := range p.Children() {
		ops = append(ops, liveScalePlanOperators(child)...)
	}
	return ops
}

func liveScaleExplain(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, label, cypher string, params map[string]any) []string {
	t.Helper()

	result, err := session.Run(ctx, "EXPLAIN "+cypher, params)
	if err != nil {
		t.Fatalf("%s explain: %v", label, err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("%s explain consume: %v", label, err)
	}
	return liveScalePlanOperators(summary.Plan())
}

// containsOp reports whether any plan operator name in ops names want.
// Neo4j 2026.09.0 suffixes each operator with its runtime source (e.g.
// "NodeByLabelScan@neo4j"), so this matches by prefix rather than exact
// equality.
func containsOp(ops []string, want string) bool {
	for _, op := range ops {
		if strings.HasPrefix(op, want) {
			return true
		}
	}
	return false
}

func liveScaleCanonicalRows(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, label, cypher string, params map[string]any) []string {
	t.Helper()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("%s collect: %v", label, err)
	}
	rows := make([]string, 0, len(records))
	for _, rec := range records {
		parts := make([]string, 0, len(rec.Keys))
		for _, key := range rec.Keys {
			v, _ := rec.Get(key)
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
		sort.Strings(parts)
		rows = append(rows, strings.Join(parts, "|"))
	}
	sort.Strings(rows)
	return rows
}

func liveScaleTimeRead(ctx context.Context, t *testing.T, session neo4jdriver.SessionWithContext, label, cypher string, params map[string]any, runs int) []time.Duration {
	t.Helper()

	durations := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		started := time.Now()
		result, err := session.Run(ctx, cypher, params)
		if err != nil {
			t.Fatalf("%s run %d: %v", label, i, err)
		}
		if _, err := result.Collect(ctx); err != nil {
			t.Fatalf("%s collect %d: %v", label, i, err)
		}
		durations = append(durations, time.Since(started))
	}
	return durations
}

// liveScalePercentile linearly interpolates the pth percentile (0..1) over
// an ascending-sorted duration slice.
func liveScalePercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lo)
	return sorted[lo] + time.Duration(frac*float64(sorted[hi]-sorted[lo]))
}

func liveScaleAuthedHandler(driver neo4jdriver.DriverWithContext, database string) *CodeHandler {
	return &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNeo4j,
		Neo4j:        newLiveNornicDBReader(driver, database),
		Content:      outlierFixtureStore{membersByID: liveScaleMembersByID()},
	}
}

func liveScaleMembersByID() map[string]codedivergence.Member {
	p := liveScaleSignalPrefix
	out := map[string]codedivergence.Member{}
	paths := map[string]string{}
	for _, h := range []string{"1", "2", "3", "4", "5"} {
		paths[p+":h-"+h] = "signalpkg/api.go"
	}
	paths[p+":m-a1"], paths[p+":m-a2"] = "signalpkg/impl/a.go", "signalpkg/impl/a.go"
	paths[p+":m-b1"] = "signalpkg/impl/b.go"
	line := 10
	for _, id := range []string{p + ":h-1", p + ":h-2", p + ":h-3", p + ":h-4", p + ":h-5", p + ":m-a1", p + ":m-a2", p + ":m-b1"} {
		out[id] = codedivergence.Member{
			EntityID: id, EntityName: "handle", EntityType: "Function",
			RelativePath: paths[id], Language: "go",
			StartLine: line, EndLine: line + 15, TokenCount: 120,
		}
		line += 20
	}
	return out
}

// TestLiveOutlierRepoScaleNeo4j is the #6929 repo-scale proof: seed a real
// Neo4j container to platform scale (45,000+ Functions in the swept
// repository, 450,000+ elsewhere), then prove (a) old vs. new cohort
// statement text return identical row multisets, (b) the new statement
// plans a NodeIndexSeek on function_repo_id with no NodeByLabelScan on
// Function, and (c) the full production sweep's cold/warm wall time through
// CodeHandler.assembleOutlierTrack, with a phase breakdown identifying the
// dominant step.
func TestLiveOutlierRepoScaleNeo4j(t *testing.T) {
	liveScaleSkipUnset(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	driver := liveScaleOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()

	const database = "neo4j"
	liveScaleApplySchema(ctx, t, driver, database)

	writeSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	seedStarted := time.Now()
	targetSeeded, otherSeeded := liveScaleSeedGraph(ctx, t, writeSession)
	_ = writeSession.Close(ctx)
	t.Logf("seeded target=%d other=%d functions in %s", targetSeeded, otherSeeded, time.Since(seedStarted))

	readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = readSession.Close(ctx) }()

	targetCount := liveScaleCount(ctx, t, readSession, "count target functions",
		"MATCH (f:Function) WHERE f.repo_id = $repo_id RETURN count(f) AS c", map[string]any{"repo_id": liveScaleRepo})
	totalCount := liveScaleCount(ctx, t, readSession, "count all functions", "MATCH (f:Function) RETURN count(f) AS c", nil)
	otherCount := totalCount - targetCount
	t.Logf("verified target=%d other=%d total=%d", targetCount, otherCount, totalCount)
	if targetCount < liveScaleMinTargetFunctions {
		t.Fatalf("target repo Functions = %d, want >= %d", targetCount, liveScaleMinTargetFunctions)
	}
	if otherCount < liveScaleMinOtherFunctions {
		t.Fatalf("other-repo Functions = %d, want >= %d", otherCount, liveScaleMinOtherFunctions)
	}

	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	sources := []codedivergence.CohortSource{codedivergence.CohortInterface, codedivergence.CohortRouter, codedivergence.CohortPackage}

	// (a) rows-equal and (b) EXPLAIN, per source.
	for _, source := range sources {
		oldCypher, oldParams := legacyBuildOutlierCohortsCypher(source, liveScaleRepo)
		newCypher, newParams := BuildOutlierCohortsCypher(source, liveScaleRepo, querycontract.GraphBackendNeo4j, access)

		oldRows := liveScaleCanonicalRows(ctx, t, readSession, fmt.Sprintf("%s old rows", source), oldCypher, oldParams)
		newRows := liveScaleCanonicalRows(ctx, t, readSession, fmt.Sprintf("%s new rows", source), newCypher, newParams)
		if len(oldRows) == 0 {
			t.Errorf("%s: old statement returned zero rows, cannot prove row-set equality on real data", source)
		}
		if len(oldRows) != len(newRows) {
			t.Fatalf("%s: old rows = %d, new rows = %d, want equal", source, len(oldRows), len(newRows))
		}
		for i := range oldRows {
			if oldRows[i] != newRows[i] {
				t.Fatalf("%s: row %d differs\nold: %s\nnew: %s", source, i, oldRows[i], newRows[i])
			}
		}

		oldOps := liveScaleExplain(ctx, t, readSession, fmt.Sprintf("%s old", source), oldCypher, oldParams)
		newOps := liveScaleExplain(ctx, t, readSession, fmt.Sprintf("%s new", source), newCypher, newParams)
		t.Logf("%s plan old=%v new=%v", source, oldOps, newOps)
		if containsOp(newOps, "NodeByLabelScan") {
			t.Errorf("%s: new statement plan still contains NodeByLabelScan: %v", source, newOps)
		}
		if source == codedivergence.CohortPackage {
			if !containsOp(oldOps, "NodeByLabelScan") {
				t.Errorf("package: expected the old coalesce()-wrapped statement to plan a NodeByLabelScan, got %v", oldOps)
			}
			if !containsOp(newOps, "NodeIndexSeek") {
				t.Errorf("package: expected the new statement to plan a NodeIndexSeek, got %v", newOps)
			}
		}
	}

	// Seed-enumeration-only timing, old vs new, per source: 1 cold + 5 warm.
	type seedTiming struct {
		source               codedivergence.CohortSource
		oldMedian, newMedian time.Duration
	}
	var seedTimings []seedTiming
	for _, source := range sources {
		oldCypher, oldParams := legacyBuildOutlierCohortsCypher(source, liveScaleRepo)
		newCypher, newParams := BuildOutlierCohortsCypher(source, liveScaleRepo, querycontract.GraphBackendNeo4j, access)
		oldDurations := liveScaleTimeRead(ctx, t, readSession, fmt.Sprintf("%s old timing", source), oldCypher, oldParams, 6)
		newDurations := liveScaleTimeRead(ctx, t, readSession, fmt.Sprintf("%s new timing", source), newCypher, newParams, 6)
		sort.Slice(oldDurations, func(i, j int) bool { return oldDurations[i] < oldDurations[j] })
		sort.Slice(newDurations, func(i, j int) bool { return newDurations[i] < newDurations[j] })
		// Drop the cold first run; median of the 5 warm runs.
		oldWarm, newWarm := oldDurations[1:], newDurations[1:]
		sort.Slice(oldWarm, func(i, j int) bool { return oldWarm[i] < oldWarm[j] })
		sort.Slice(newWarm, func(i, j int) bool { return newWarm[i] < newWarm[j] })
		t.Logf("%s cohort read cold old=%s new=%s; warm samples old=%v new=%v",
			source, oldDurations[0], newDurations[0], oldWarm, newWarm)
		seedTimings = append(seedTimings, seedTiming{
			source:    source,
			oldMedian: liveScalePercentile(oldWarm, 0.5),
			newMedian: liveScalePercentile(newWarm, 0.5),
		})
	}
	var seedDelta time.Duration
	for _, st := range seedTimings {
		delta := st.oldMedian - st.newMedian
		seedDelta += delta
		t.Logf("%s cohort read warm median old=%s new=%s delta=%s", st.source, st.oldMedian, st.newMedian, delta)
	}
	t.Logf("summed warm-median cohort-read delta across all three sources (old-new) = %s", seedDelta)

	// (c) full production sweep via CodeHandler.assembleOutlierTrack: 1 cold
	// + 10 warm, using the CURRENT (fixed) production code path.
	handler := liveScaleAuthedHandler(driver, database)
	const fullRuns = 11
	fullDurations := make([]time.Duration, 0, fullRuns)
	var lastFindingCount int
	for i := 0; i < fullRuns; i++ {
		started := time.Now()
		findings, suppressions, err := handler.assembleOutlierTrack(ctx, liveScaleRepo, false)
		if err != nil {
			t.Fatalf("assembleOutlierTrack run %d: %v", i, err)
		}
		fullDurations = append(fullDurations, time.Since(started))
		lastFindingCount = len(findings)
		if i == fullRuns-1 {
			t.Logf("assembleOutlierTrack findings=%d suppressions=%v", len(findings), suppressions)
		}
	}
	if lastFindingCount == 0 {
		t.Errorf("assembleOutlierTrack returned zero findings; the signal fixture should produce at least one")
	}
	coldFull := fullDurations[0]
	warmFull := append([]time.Duration(nil), fullDurations[1:]...)
	sort.Slice(warmFull, func(i, j int) bool { return warmFull[i] < warmFull[j] })
	p50 := liveScalePercentile(warmFull, 0.5)
	p95 := liveScalePercentile(warmFull, 0.95)
	t.Logf("full sweep (assembleOutlierTrack, current/fixed code): cold=%s warm p50=%s p95=%s samples=%v", coldFull, p50, p95, warmFull)
	t.Logf("estimated pre-fix full sweep (warm p50 + summed old-new cohort-read delta) ~= %s", p50+seedDelta)

	// Phase breakdown to name the dominant step, per the diag-6929
	// NOT_CHECKED item on the 50-id CALLS-fanout chunk count.
	seedsStart := time.Now()
	seeds, err := handler.readOutlierCohortSeeds(ctx, liveScaleRepo, outlierCohortSources())
	if err != nil {
		t.Fatalf("readOutlierCohortSeeds: %v", err)
	}
	seedsElapsed := time.Since(seedsStart)
	cohorts, _ := codedivergence.GroupOutlierCohorts(seeds, codedivergence.DefaultOutlierParams())
	memberSet := map[string]struct{}{}
	for _, cohort := range cohorts {
		for _, member := range cohort.Members {
			memberSet[member] = struct{}{}
		}
	}
	memberIDs := make([]string, 0, len(memberSet))
	for id := range memberSet {
		memberIDs = append(memberIDs, id)
	}
	sort.Strings(memberIDs)
	chunkCount := (len(memberIDs) + outlierCalleeEdgeBatchSize - 1) / outlierCalleeEdgeBatchSize
	edgesStart := time.Now()
	if _, _, err := handler.readOutlierCalleeEdges(ctx, memberIDs, liveScaleRepo); err != nil {
		t.Fatalf("readOutlierCalleeEdges: %v", err)
	}
	edgesElapsed := time.Since(edgesStart)
	remainder := p50 - seedsElapsed - edgesElapsed
	t.Logf("phase breakdown: cohorts=%d members=%d calls-fanout-chunks(%d/chunk)~=%d; seed-reads=%s callee-edges-fanout=%s remainder(mediation+hydration+assembly)~=%s",
		len(cohorts), len(memberIDs), outlierCalleeEdgeBatchSize, chunkCount, seedsElapsed, edgesElapsed, remainder)
	dominant := "seed-enumeration reads"
	dominantValue := seedsElapsed
	if edgesElapsed > dominantValue {
		dominant, dominantValue = "CALLS-fanout (50-id chunks)", edgesElapsed
	}
	if remainder > dominantValue {
		dominant, dominantValue = "mediation+hydration+assembly", remainder
	}
	if p50 < time.Second {
		t.Logf("full sweep warm p50 %s is UNDER the 1s budget", p50)
	} else {
		t.Logf("full sweep warm p50 %s is OVER the 1s budget; dominant step: %s (%s)", p50, dominant, dominantValue)
	}

	// Sanity: the real HTTP findings route serves 200 with the same shape.
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/divergence/findings",
		strings.NewReader(fmt.Sprintf(`{"repo_id":%q,"kind":"convention_outlier"}`, liveScaleRepo)))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("findings route status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}
