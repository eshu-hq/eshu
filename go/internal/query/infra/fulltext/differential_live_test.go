// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live differential for #7245 PR-2 on the pinned Neo4j: for every request
// shape, the full-text statement the production handler emits returns the
// same rows as the legacy statement it replaces, unscoped and under a scoped
// grant. The fixture holds the adversarial rows the hand-off names: key
// overflow, a key of exactly 8,000 and 8,001 code points, case, Lucene
// specials, whitespace, unicode, the U+001F separator inside a value,
// duplicate projected rows, a multi-label node, non-string properties, the
// data_type fallback, the "::" equality branch, categories and kind filters,
// and USES / MATCHES_STATE grant admission. Neo4j-only: NornicDB has no form
// of infra_search_key_ft.
//
//	docker run -d --name eshu-live-neo4j -p 127.0.0.1:27950:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27950 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/infra/fulltext -tags live_nornicdb_answer_truth \
//	  -run TestLiveInfraSearchFulltextDifferential -count=1 -v
package fulltext_test

import (
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
	"unicode/utf8"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// liveFulltextGraph runs statements on the live Neo4j and records the last
// one, so a test can rerun the exact statement the handler emitted.
type liveFulltextGraph struct {
	driver   neo4jdriver.DriverWithContext
	database string
	cypher   string
	params   map[string]any
}

func openLiveFulltextGraph(t *testing.T) *liveFulltextGraph {
	t.Helper()
	if backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")); backend != string(graph.SchemaBackendNeo4j) {
		t.Skipf("ESHU_LIVE_GRAPH_BACKEND = %q; infra_search_key_ft is Neo4j-only", backend)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	return &liveFulltextGraph{driver: driver, database: database}
}

func (g *liveFulltextGraph) exec(ctx context.Context, mode neo4jdriver.AccessMode, cypher string, params map[string]any) ([]map[string]any, error) {
	session := g.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: mode, DatabaseName: g.database})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

// Run records and answers a read; the handler calls it.
func (g *liveFulltextGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher, g.params = cypher, params
	return g.exec(ctx, neo4jdriver.AccessModeRead, cypher, params)
}

func (*liveFulltextGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// write is the search.Runner the backfill and fixture use.
type liveFulltextWriter struct{ g *liveFulltextGraph }

func (w liveFulltextWriter) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return w.g.exec(ctx, neo4jdriver.AccessModeWrite, cypher, params)
}

func (g *liveFulltextGraph) mustWrite(t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := g.exec(ctx, neo4jdriver.AccessModeWrite, cypher, params)
	if err != nil {
		t.Fatalf("write %.80q: %v", cypher, err)
	}
	return rows
}

// keyAll creates infra_search_key_ft when missing, keys every infra node,
// and waits for the index.
func (g *liveFulltextGraph) keyAll(t *testing.T) {
	t.Helper()
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range statements {
		if strings.Contains(stmt, graph.InfraSearchKeyIndexName) {
			g.mustWrite(t, stmt, nil)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := search.Backfill(ctx, liveFulltextWriter{g: g}, search.BackfillOptions{}); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	g.mustWrite(t, "CALL db.awaitIndex($name, 300)", map[string]any{"name": graph.InfraSearchKeyIndexName})
}

func TestLiveInfraSearchFulltextDifferential(t *testing.T) {
	g := openLiveFulltextGraph(t)
	token := fmt.Sprintf("zz7245r%d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() {
		g.mustWrite(t, "MATCH (n) WHERE n.uid STARTS WITH $t DETACH DELETE n", map[string]any{"t": token})
	})
	seedLiveFulltextFixture(t, g, token)
	g.keyAll(t)
	seedLiveFulltextCapEdge(t, g, token)

	readiness := fulltext.NewReadiness(g, nil, nil)
	if st := readiness.Refresh(context.Background()); st.Index != fulltext.IndexOnline || st.Err != nil {
		t.Fatalf("live readiness = %+v, want index online with no probe error", st)
	}

	scoped := query.AuthContext{
		Mode:                 query.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{token + "-repo-a"},
		AllowedScopeIDs:      []string{token + "-scope-a"},
	}
	var compared, nonEmpty int
	for _, body := range liveFulltextBodies(token) {
		for _, auth := range []*query.AuthContext{nil, &scoped} {
			rows := compareLiveFulltext(t, g, token, body, auth)
			compared++
			if rows > 0 {
				nonEmpty++
			}
		}
	}
	t.Logf("compared %d request shapes, %d with fixture rows", compared, nonEmpty)
	if nonEmpty < compared/2 {
		t.Fatalf("only %d of %d shapes matched fixture rows; the differential is vacuous", nonEmpty, compared)
	}
}

// compareLiveFulltext captures the legacy and full-text statements the real
// handler emits for body, reruns both unbounded, and compares the fixture
// rows as sorted multisets. For a fixture-only query it also compares the
// production page's name sequence. It returns the fixture row count.
func compareLiveFulltext(t *testing.T, g *liveFulltextGraph, token, body string, auth *query.AuthContext) int {
	t.Helper()
	name := body
	if len(name) > 90 {
		name = name[:90]
	}
	if auth != nil {
		name = "scoped " + name
	}
	legacyCypher, legacyParams := captureLiveFulltextStatement(t, g, body, auth, nil)
	ftCypher, ftParams := captureLiveFulltextStatement(t, g, body, auth, liveReadyReader(t))
	var req struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(body), &req)
	if utf8.RuneCountInString(strings.TrimSpace(req.Query)) > fulltext.MaxQueryRunes {
		// Past the bound the handler must keep the legacy statement.
		if ftCypher != legacyCypher {
			t.Fatalf("%s: a query past fulltext.MaxQueryRunes left the legacy statement:\n%s", name, ftCypher)
		}
		return 0
	}
	if !strings.HasPrefix(ftCypher, "CALL db.index.fulltext.queryNodes(") {
		t.Fatalf("%s: handler did not select the full-text read:\n%s", name, ftCypher)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	unbounded := func(params map[string]any) map[string]any {
		out := make(map[string]any, len(params))
		for k, v := range params {
			out[k] = v
		}
		out["limit"] = 10_000_000
		return out
	}
	legacyAll, err := g.exec(ctx, neo4jdriver.AccessModeRead, legacyCypher, unbounded(legacyParams))
	if err != nil {
		t.Fatalf("%s: legacy: %v", name, err)
	}
	ftAll, err := g.exec(ctx, neo4jdriver.AccessModeRead, ftCypher, unbounded(ftParams))
	if err != nil {
		t.Fatalf("%s: full-text statement failed: %v", name, err)
	}
	legacyKeys, ftKeys := liveFixtureRowKeys(legacyAll, token), liveFixtureRowKeys(ftAll, token)
	if strings.Join(legacyKeys, "\n") != strings.Join(ftKeys, "\n") {
		t.Errorf("%s: fixture rows differ: legacy %d, full-text %d\nlegacy:   %.400v\nfulltext: %.400v",
			name, len(legacyKeys), len(ftKeys), legacyKeys, ftKeys)
	}
	if strings.Contains(body, token) {
		legacyPage, err := g.exec(ctx, neo4jdriver.AccessModeRead, legacyCypher, legacyParams)
		if err != nil {
			t.Fatalf("%s: legacy page: %v", name, err)
		}
		ftPage, err := g.exec(ctx, neo4jdriver.AccessModeRead, ftCypher, ftParams)
		if err != nil {
			t.Fatalf("%s: full-text page: %v", name, err)
		}
		if got, want := liveRowNames(ftPage), liveRowNames(legacyPage); got != want {
			t.Errorf("%s: page name sequence differs:\nlegacy:   %q\nfulltext: %q", name, want, got)
		}
	}
	return len(legacyKeys)
}

func captureLiveFulltextStatement(t *testing.T, g *liveFulltextGraph, body string, auth *query.AuthContext, reader *fulltext.Reader) (string, map[string]any) {
	t.Helper()
	handler := &query.InfraHandler{GraphBackend: querycontract.GraphBackendNeo4j, Neo4j: g, SearchKey: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(body))
	if auth != nil {
		req = req.WithContext(query.ContextWithAuthContext(req.Context(), *auth))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%.80s: status %d body %.300s", body, rec.Code, rec.Body.String())
	}
	return g.cypher, g.params
}

// liveReadyReader is an enabled Reader whose readiness reads ready, so the
// handler emits the full-text statement for every eligible query; the live
// readiness probe is checked separately.
func liveReadyReader(t *testing.T) *fulltext.Reader {
	t.Helper()
	readiness := fulltext.NewReadiness(&probeGraph{state: "ONLINE"}, nil, nil)
	if !readiness.Refresh(context.Background()).Ready {
		t.Fatal("fake readiness not ready")
	}
	return &fulltext.Reader{Enabled: true, Readiness: readiness}
}

// liveFixtureRowKeys returns the sorted JSON of every row that belongs to the
// fixture (its projection mentions token), so rows other tests left in a
// shared database never decide the comparison.
func liveFixtureRowKeys(rows []map[string]any, token string) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		b, _ := json.Marshal(row)
		if strings.Contains(string(b), token) {
			keys = append(keys, string(b))
		}
	}
	sort.Strings(keys)
	return keys
}

func liveRowNames(rows []map[string]any) string {
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = fmt.Sprint(row["name"])
	}
	return strings.Join(names, "\x00")
}

// seedLiveFulltextFixture writes the #7245 differential fixture. Every node
// carries uid prefix token for cleanup, and every projected row mentions
// token so the comparison can ignore rows other tests own.
func seedLiveFulltextFixture(t *testing.T, g *liveFulltextGraph, token string) {
	t.Helper()
	types := []string{"aws_iam_role", "aws_sns_topic", "aws_sqs_queue", "aws_iam_role_policy_attachment"}
	labels := []string{"TerraformResource", "CloudResource", "K8sResource", "TerraformVariable", "HelmChart", "TerraformDataSource"}
	for i := 0; i < 48; i++ {
		label := labels[i%len(labels)]
		typeProp := "resource_type"
		if label == "TerraformDataSource" {
			typeProp = "data_type"
		}
		repo := token + "-repo-b"
		if i%2 == 0 {
			repo = token + "-repo-a"
		}
		g.mustWrite(t, "CREATE (n:"+label+" {uid: $uid, id: $id, name: $name, "+typeProp+": $type, repo_id: $repo, "+
			"config_path: $path, region: $region, kind: $kind})", map[string]any{
			"uid":    token + ":r:" + itoa(i),
			"id":     token + ":" + strings.ToLower(label) + ":" + itoa(i),
			"name":   token + "-svc-" + itoa(i%7) + "-" + types[i%len(types)],
			"type":   types[i%len(types)],
			"repo":   repo,
			"path":   "/" + token + "/m" + itoa(i%3) + "/main.tf",
			"region": []string{"us-east-1", "us-west-2"}[i%2],
			"kind":   map[bool]any{true: "Deployment", false: nil}[i%3 == 0],
		})
	}
	big := strings.Repeat("x", 40000)
	repoA := token + "-repo-a"
	adversarial := []struct {
		cypher string
		params map[string]any
	}{
		// overflow: a 40 KB name holding the term, a 70 KB config_path
		{"CREATE (:CloudResource {uid: $t + ':ovf1', id: $t + ':ovf1', name: $big + $t + '-ovf-aws_iam_role' + $big, resource_type: 'aws_iam_role', repo_id: $a})", nil},
		{"CREATE (:TerraformResource {uid: $t + ':ovf2', id: $t + ':ovf2', name: 'plain-ovf', config_path: $big + $big + $t + '-cfgovf', repo_id: $a})", nil},
		// case sensitivity
		{"CREATE (:TerraformResource {uid: $t + ':case', id: $t + ':case', name: 'MyAWS_IAM_Role-' + $t, resource_type: 'AWS_IAM_ROLE', repo_id: $a})", nil},
		// whitespace, tab, Lucene specials
		{"CREATE (:K8sResource {uid: $t + ':ws', id: $t + ':ws', name: $t + ' bucket name\ttab', kind: 'ConfigMap', repo_id: $a})", nil},
		{"CREATE (:HelmChart {uid: $t + ':spec', id: $t + ':spec', name: $t + 'a+b-(c)[d]{e}^\"f\"~*?:\\\\/ & | ! OR AND NOT', repo_id: $a})", nil},
		// unicode
		{"CREATE (:HelmValues {uid: $t + ':uni', id: $t + ':uni', name: 'é😀-' + $t, region: 'ünïcode', repo_id: $a})", nil},
		// two nodes with identical projected rows, both granted
		{"CREATE (:TerraformLocal {uid: $t + ':dupa', id: $t + ':dup', name: $t + '-dup-row', repo_id: $a}), (:TerraformLocal {uid: $t + ':dupb', id: $t + ':dup', name: $t + '-dup-row', repo_id: $a})", nil},
		// multi-label node
		{"CREATE (:TerraformResource:TerraformBlock {uid: $t + ':multi', id: $t + ':multi', name: $t + '-multi-label', repo_id: $a})", nil},
		// non-string name and list id
		{"CREATE (:TerraformOutput {uid: $t + ':int', id: [$t + ':list'], name: 4242, region: $t + '-int', repo_id: $a})", nil},
		// data_type fallback with no resource_type
		{"CREATE (:TerraformDataSource {uid: $t + ':dt', id: $t + ':dt', name: $t + '-dt-only', data_type: 'aws_caller_identity', repo_id: $a})", nil},
		// the key separator inside a value
		{"CREATE (:CrossplaneXRD {uid: $t + ':sep', id: $t + ':sep', name: 'left\u001F' + $t, repo_id: $a})", nil},
		// "::" equality branch
		{"CREATE (:CloudResource {uid: $t + ':arn', id: $t + ':arn', name: $t + '-arn-holder', arn: 'arn:aws:sns::' + $t + ':topic', resource_id: $t + '::rid', resource_type: $t + '::type'})", nil},
		// grant admission through USES and MATCHES_STATE; an orphan state row
		{"CREATE (:WorkloadInstance {uid: $t + ':wi', id: $t + ':wi', repo_id: $a})-[:USES]->(:CloudResource {uid: $t + ':used', id: $t + ':used', name: $t + '-used-resource'})", nil},
		{"CREATE (:TerraformResource {uid: $t + ':cfg', id: $t + ':cfg', name: $t + '-cfg-resource', repo_id: $a})-[:MATCHES_STATE]->(:TerraformStateResource {uid: $t + ':state', id: $t + ':state', name: $t + '-state-resource'})", nil},
		{"CREATE (:TerraformStateResource {uid: $t + ':orphan', id: $t + ':orphan', name: $t + '-state-orphan'})", nil},
	}
	for _, row := range adversarial {
		g.mustWrite(t, row.cypher, map[string]any{"t": token, "big": big, "a": repoA})
	}
}

// seedLiveFulltextCapEdge writes two ArgoCDApplication nodes whose joined
// keys are exactly graph.InfraSearchKeyMaxChars and one code point more,
// measured from a probe node keyed by the real backfill, then re-keys.
func seedLiveFulltextCapEdge(t *testing.T, g *liveFulltextGraph, token string) {
	t.Helper()
	const probeLen = 100
	g.mustWrite(t, "CREATE (:ArgoCDApplication {uid: $t + ':capprobe', id: $t + ':cap', name: $name})",
		map[string]any{"t": token, "name": strings.Repeat("😀", probeLen)})
	g.keyAll(t)
	rows := g.mustWrite(t, "MATCH (n:ArgoCDApplication {uid: $uid}) RETURN size(n."+graph.InfraSearchKeyProperty+") AS size",
		map[string]any{"uid": token + ":capprobe"})
	size, _ := rows[0]["size"].(int64)
	if size <= probeLen {
		t.Fatalf("probe key size = %d, want > %d", size, probeLen)
	}
	overhead := int(size) - probeLen
	for suffix, extra := range map[string]int{"cap8000": 0, "cap8001": 1} {
		g.mustWrite(t, "CREATE (:ArgoCDApplication {uid: $t + ':' + $s, id: $t + ':cap', name: $name})", map[string]any{
			"t": token, "s": suffix, "name": strings.Repeat("😀", graph.InfraSearchKeyMaxChars-overhead+extra),
		})
	}
	g.keyAll(t)
	flags := g.mustWrite(t, "MATCH (n:ArgoCDApplication) WHERE n.uid IN [$t + ':cap8000', $t + ':cap8001'] "+
		"RETURN n.uid AS uid, n."+graph.InfraSearchOverflowProperty+" AS ovf, size(n."+graph.InfraSearchKeyProperty+") AS size",
		map[string]any{"t": token})
	for _, f := range flags {
		want := graph.InfraSearchOverflowNo
		if strings.HasSuffix(f["uid"].(string), "cap8001") {
			want = graph.InfraSearchOverflowYes
		}
		if f["ovf"] != want {
			t.Fatalf("cap edge %v: ovf = %v (key size %v), want %s", f["uid"], f["ovf"], f["size"], want)
		}
	}
}

// liveFulltextBodies is every request body the differential runs.
func liveFulltextBodies(token string) []string {
	terms := []string{
		token, "aws_iam_role", token + "-svc-1-", "us-west-2", "/" + token + "/m1/", "AWS_IAM", "Role", "e", "aws",
		"zzznomatch" + token, token + " bucket", "bucket name\ttab", "a+b-(c)", `"f"~*?`, `\/ & | !`, "OR AND NOT",
		"*", "?", "é😀-" + token, "ünï", token + "-dup", token + "-multi", "4242", token + ":list", "aws_caller",
		"left\u001F" + token, "t\u001F", "arn:aws:sns::" + token + ":topic", token + "::rid", token + "::type", "::",
		token + "-ovf", token + "-cfgovf", "xxxxxxxxxx", "😀😀😀", token + "-used", token + "-state", token + ":cap",
		strings.Repeat("😀", fulltext.MaxQueryRunes), strings.Repeat("a", fulltext.MaxQueryRunes),
		strings.Repeat("a", fulltext.MaxQueryRunes+1), strings.Repeat("a", 9000),
	}
	var bodies []string
	for _, q := range terms {
		b, _ := json.Marshal(map[string]any{"query": q, "limit": 200})
		bodies = append(bodies, string(b))
	}
	for _, extra := range []string{`"category":"terraform"`, `"category":"cloud"`, `"kind":"aws_iam_role"`, `"category":"k8s","kind":"ConfigMap"`, `"category":"argocd"`} {
		for _, q := range []string{"aws_iam_role", token + "-svc-1", token + " bucket", "aws", token} {
			qb, _ := json.Marshal(q)
			bodies = append(bodies, `{"query":`+string(qb)+`,`+extra+`,"limit":16}`)
		}
	}
	return bodies
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
