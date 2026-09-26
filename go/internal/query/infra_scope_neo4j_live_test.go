// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

// Live Neo4j proof for the #7215 scoped infra dialect. It drives the REAL
// InfraHandler through the REAL Neo4jReader (so the 10 s bounded-read budget
// applies) against a live Neo4j, and proves:
//
//   - row-set equality between the Neo4j list-EXISTS rewrite and the SHAPE-A
//     statement for g1 and g5, and against the SHAPE-A predicate (hoisted, the
//     only SHAPE-A form that plans on Neo4j at that size) at the cap;
//   - both equal an oracle computed in Go from the fixture design, not from
//     any Cypher;
//   - the negative authorization cases (no ungranted row, 404 without
//     disclosure, no ungranted neighbour).
//
// Run against an isolated Neo4j (the docker-compose.neo4j.yml digest):
//
//	ESHU_INFRA_SCOPE_NEO4J_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:27787 \
//	ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD=... \
//	go test -tags live_infra_scope_neo4j ./internal/query \
//	  -run 'TestLiveInfraScopeNeo4j' -count=1 -v
package query

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type liveNeo4jScope struct {
	driver   neo4jdriver.DriverWithContext
	database string
	reader   *Neo4jReader
}

func openLiveNeo4jScope(t *testing.T) *liveNeo4jScope {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ESHU_INFRA_SCOPE_NEO4J_LIVE")) == "" {
		t.Skip("set ESHU_INFRA_SCOPE_NEO4J_LIVE=1 and ESHU_NEO4J_URI to run the live #7215 Neo4j proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if user := os.Getenv("ESHU_NEO4J_USERNAME"); user != "" {
		auth = neo4jdriver.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	return &liveNeo4jScope{driver: driver, database: database, reader: NewNeo4jReader(driver, database)}
}

// raw runs a statement outside the handler with its own long timeout, for
// seeding and for the SHAPE-A reference statements (which can take far longer
// than the 10 s handler budget on Neo4j; that is the bug).
func (l *liveNeo4jScope) raw(t *testing.T, timeout time.Duration, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := l.rawErr(timeout, cypher, params)
	if err != nil {
		t.Fatalf("raw cypher: %v\n%.400s", err, cypher)
	}
	return rows
}

func (l *liveNeo4jScope) rawErr(timeout time.Duration, cypher string, params map[string]any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+30*time.Second)
	defer cancel()
	session := l.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: l.database})
	defer func() { _ = session.Close(context.Background()) }()
	res, err := session.Run(ctx, cypher, params, neo4jdriver.WithTxTimeout(timeout))
	if err != nil {
		return nil, err
	}
	recs, err := res.Collect(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.AsMap())
	}
	return out, nil
}

// liveScopeFixture is the seeded topology plus its oracle. Every id carries a
// per-run nonce, so the search query (the nonce) matches only this fixture.
type liveScopeFixture struct {
	nonce  string
	owners []string // repo / scope ids that own admitted nodes
	admit  map[string]func(d, s map[string]bool) bool
	labels map[string]string // node id -> label
}

func (f *liveScopeFixture) id(part string) string { return f.nonce + "-" + part }

// seedLiveScopeFixture writes, for each owner o: a Repository {id:o}; a
// TerraformResource owned by o (direct); a CloudResource USED by o's
// WorkloadInstance (USES); a TerraformStateResource matched by o's
// TerraformResource (MATCHES_STATE); an other-tenant HelmChart o DEFINES
// (DEFINES collision); an other-tenant K8sResource deployed from o
// (DEPLOYMENT_SOURCE). Negatives: an ungranted-used and an orphan
// CloudResource, an unmatched and an ungranted-matched TerraformStateResource,
// an ungranted-owned K8sResource with no path, and ungranted neighbours on a
// granted anchor in both directions.
func seedLiveScopeFixture(t *testing.T, l *liveNeo4jScope) *liveScopeFixture {
	t.Helper()
	f := &liveScopeFixture{
		nonce:  fmt.Sprintf("n7215x%d", time.Now().UnixNano()),
		admit:  map[string]func(d, s map[string]bool) bool{},
		labels: map[string]string{},
	}
	f.owners = []string{f.id("repo-00"), f.id("repo-01"), f.id("repo-04"), f.id("repo-40"), f.id("scope-00"), f.id("scope-03"), f.id("scope-60")}
	other := f.id("other")
	never := func(_, _ map[string]bool) bool { return false }

	type node struct{ label, id, repoID string }
	type edge struct{ from, rel, to string }
	var nodes []node
	var edges []edge
	add := func(label, id, repoID string, admit func(d, s map[string]bool) bool) {
		nodes = append(nodes, node{label, id, repoID})
		f.labels[id] = label
		f.admit[id] = admit
	}
	for _, o := range f.owners {
		o := o
		tag := strings.TrimPrefix(o, f.nonce+"-")
		inD := func(d, _ map[string]bool) bool { return d[o] }
		inS := func(_, s map[string]bool) bool { return s[o] }
		add("Repository", o, "", func(d, _ map[string]bool) bool { return d[o] })
		add("TerraformResource", f.id("tf-"+tag), o, inD)
		add("WorkloadInstance", f.id("wi-"+tag), o, inD)
		add("CloudResource", f.id("cr-"+tag), "", inS)
		add("TerraformStateResource", f.id("tsr-"+tag), "", inS)
		add("HelmChart", f.id("hc-"+tag), other, inS)
		add("K8sResource", f.id("k8s-"+tag), other, inD)
		edges = append(edges,
			edge{f.id("wi-" + tag), "USES", f.id("cr-" + tag)},
			edge{f.id("tf-" + tag), "MATCHES_STATE", f.id("tsr-" + tag)},
			edge{o, "DEFINES", f.id("hc-" + tag)},
			edge{f.id("k8s-" + tag), "DEPLOYMENT_SOURCE", o},
		)
	}
	add("Repository", other, "", never)
	add("WorkloadInstance", f.id("wi-other"), other, never)
	add("CloudResource", f.id("cr-ungranted"), "", never)
	add("CloudResource", f.id("cr-orphan"), "", never)
	add("TerraformStateResource", f.id("tsr-unmatched"), "", never)
	add("TerraformResource", f.id("tf-other"), other, never)
	add("TerraformStateResource", f.id("tsr-ungranted"), "", never)
	add("K8sResource", f.id("k8s-ungranted"), other, never)
	edges = append(edges,
		edge{f.id("wi-other"), "USES", f.id("cr-ungranted")},
		edge{f.id("tf-other"), "MATCHES_STATE", f.id("tsr-ungranted")},
		edge{f.id("tf-repo-00"), "DEPENDS_ON", f.id("k8s-ungranted")},
		edge{f.id("k8s-ungranted"), "DEPENDS_ON", f.id("tf-repo-00")},
		edge{f.id("cr-ungranted"), "DEPENDS_ON", f.id("tf-repo-00")},
		edge{f.id("tf-repo-00"), "DEPENDS_ON", f.id("cr-repo-00")},
	)

	for _, n := range nodes {
		props := map[string]any{"id": n.id, "name": n.id, "live7215": f.nonce}
		if n.repoID != "" {
			props["repo_id"] = n.repoID
		}
		l.raw(t, 60*time.Second, "CREATE (n:"+n.label+") SET n = $props", map[string]any{"props": props})
	}
	for _, e := range edges {
		l.raw(t, 60*time.Second,
			"MATCH (a:"+f.labels[e.from]+" {id: $from}) MATCH (b:"+f.labels[e.to]+" {id: $to}) CREATE (a)-[:"+e.rel+"]->(b)",
			map[string]any{"from": e.from, "to": e.to})
	}
	t.Cleanup(func() {
		_, _ = l.rawErr(60*time.Second, "MATCH (n) WHERE n.live7215 = $nonce DETACH DELETE n", map[string]any{"nonce": f.nonce})
	})
	return f
}

// liveGrant builds g1 (1 repo + 1 scope), g5 (5 + 5) and cap (70 + 70 = 140
// scalars; the capped slice keeps all 70 repos and scope-00..57, so scope-60
// is a direct-ownership grant whose USES / MATCHES_STATE / DEFINES admission
// is truncated in BOTH dialects).
func (f *liveScopeFixture) grant(name string, n int) dialectGrant {
	g := dialectGrant{name: name}
	for i := 0; i < n; i++ {
		g.repos = append(g.repos, f.id(fmt.Sprintf("repo-%02d", i)))
		g.scopes = append(g.scopes, f.id(fmt.Sprintf("scope-%02d", i)))
	}
	return g
}

func (f *liveScopeFixture) grants() []dialectGrant {
	return []dialectGrant{f.grant("g1", 1), f.grant("g5", 5), f.grant("cap", 70)}
}

// admitted evaluates the oracle for one node under a grant.
func (f *liveScopeFixture) admitted(id string, g dialectGrant) bool {
	d := map[string]bool{}
	for _, v := range append(append([]string{}, g.repos...), g.scopes...) {
		d[v] = true
	}
	scalars, _ := querycontract.ScopeGrantInlineScalars(g.repos, g.scopes)
	s := map[string]bool{}
	for _, v := range scalars {
		s[v] = true
	}
	admit, ok := f.admit[id]
	return ok && admit(d, s)
}

// expectedSearch is the oracle's admitted search set: fixture nodes on an
// infra label that the grant admits.
func (f *liveScopeFixture) expectedSearch(g dialectGrant) []string {
	infra := map[string]bool{}
	for _, label := range allInfraLabels {
		infra[label] = true
	}
	var out []string
	for id, label := range f.labels {
		if infra[label] && f.admitted(id, g) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func sortedIDs(rows []map[string]any, key string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprint(row[key]))
	}
	sort.Strings(out)
	return out
}

// liveHandlerRequest serves one request through the real handler on the real
// reader with the given backend.
func (l *liveNeo4jScope) liveHandlerRequest(t *testing.T, backend querycontract.GraphBackend, g dialectGrant, path, body string) (int, []byte, time.Duration) {
	t.Helper()
	auth := g.auth()
	start := time.Now()
	rec := serveInfraDialect(t, backend, &auth, l.reader, path, body)
	return rec.Code, rec.Body.Bytes(), time.Since(start)
}

// requireStatus fails with the body when a live read did not answer as
// expected (a 504 here means the 10 s budget was exceeded).
func requireStatus(t *testing.T, label string, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: status = %d, want %d; body = %.600s", label, got, want, body)
	}
}
