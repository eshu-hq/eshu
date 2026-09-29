// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

// Live Neo4j proof for #7220: the scoped infra grant must not admit a
// non-Repository node just because its `id` spells a granted repository id or
// granted scope id. Such a node is admitted only through its own `repo_id` or
// the ownership edges the predicate walks.
//
// Run against an isolated Neo4j (the docker-compose.live-backend-neo4j.yml
// digest):
//
//	ESHU_INFRA_SCOPE_NEO4J_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:27920 \
//	go test -tags live_infra_scope_neo4j ./internal/query \
//	  -run 'TestLiveInfraScopeNeo4jIDCollision' -count=1 -v
package query

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// idCollisionFixture seeds one granted repository owner and the nodes that
// probe the id-equality disjuncts. Every node name carries a per-run nonce so a
// search for the nonce matches only this fixture.
type idCollisionFixture struct {
	nonce     string
	grantRepo string // granted repository id
	grantScop string // granted scope id
	foreign   string // an ungranted repository id
}

func (f *idCollisionFixture) name(part string) string { return f.nonce + "-" + part }

// Node names, by role.
const (
	collideRepoPart  = "module-id-eq-granted-repo"
	collideScopePart = "module-id-eq-granted-scope"
	collideBarePart  = "k8s-id-eq-granted-repo-no-repo-id"
	controlPart      = "module-owned-by-granted"
	anchorPart       = "resource-anchor"
)

func seedIDCollisionFixture(t *testing.T, l *liveNeo4jScope) *idCollisionFixture {
	t.Helper()
	nonce := fmt.Sprintf("n7220x%d", time.Now().UnixNano())
	f := &idCollisionFixture{
		nonce:     nonce,
		grantRepo: nonce + "-repo-00",
		grantScop: nonce + "-scope-00",
		foreign:   nonce + "-repo-29",
	}
	type node struct{ label, id, repoID, name string }
	nodes := []node{
		{"Repository", f.grantRepo, "", f.name("repo-00")},
		{"Repository", f.foreign, "", f.name("repo-29")},
		// The granted anchor every neighbour read starts from.
		{"TerraformResource", f.name("anchor-id"), f.grantRepo, f.name(anchorPart)},
		// Owned by an ungranted repository, but the node id spells a granted id.
		{"TerraformModule", f.grantRepo, f.foreign, f.name(collideRepoPart)},
		{"TerraformModule", f.grantScop, f.foreign, f.name(collideScopePart)},
		// No repo_id at all: id equality is its only path to admission.
		{"K8sResource", f.grantRepo, "", f.name(collideBarePart)},
		// Control: really owned by the granted repository.
		{"TerraformModule", f.name("control-id"), f.grantRepo, f.name(controlPart)},
	}
	for _, n := range nodes {
		props := map[string]any{"id": n.id, "name": n.name, "live7220": nonce}
		if n.repoID != "" {
			props["repo_id"] = n.repoID
		}
		l.raw(t, 60*time.Second, "CREATE (n:"+n.label+") SET n = $props", map[string]any{"props": props})
	}
	for _, target := range []string{collideRepoPart, collideScopePart, collideBarePart, controlPart} {
		l.raw(t, 60*time.Second,
			"MATCH (a:TerraformResource {name: $from}) MATCH (b {name: $to}) CREATE (a)-[:DEPENDS_ON]->(b)",
			map[string]any{"from": f.name(anchorPart), "to": f.name(target)})
	}
	t.Cleanup(func() {
		_, _ = l.rawErr(60*time.Second, "MATCH (n) WHERE n.live7220 = $nonce DETACH DELETE n", map[string]any{"nonce": nonce})
	})
	return f
}

func (f *idCollisionFixture) grant() dialectGrant {
	return dialectGrant{name: "g1", repos: []string{f.grantRepo}, scopes: []string{f.grantScop}}
}

func (f *idCollisionFixture) leaked() []string {
	return []string{f.name(collideRepoPart), f.name(collideScopePart), f.name(collideBarePart)}
}

// requireNoCollisionLeak fails when a collision node name is present and when
// the granted control is absent (which would make the no-leak check vacuous).
func (f *idCollisionFixture) requireNoCollisionLeak(t *testing.T, label string, names []string) {
	t.Helper()
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		seen[name] = true
	}
	for _, leak := range f.leaked() {
		if seen[leak] {
			t.Errorf("%s: leaked %s (id equals a granted id, repo_id ungranted); returned %v", label, leak, names)
		}
	}
	if !seen[f.name(controlPart)] {
		t.Errorf("%s: granted control %s missing; the proof would be vacuous; returned %v", label, f.name(controlPart), names)
	}
}

func stringField(rows []any, key string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m[key] != nil {
			out = append(out, fmt.Sprint(m[key]))
		}
	}
	return out
}

// TestLiveInfraScopeNeo4jIDCollisionSearch proves the search route on both
// dialects never returns a node admitted only by id equality.
func TestLiveInfraScopeNeo4jIDCollisionSearch(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedIDCollisionFixture(t, l)
	g := f.grant()
	body := fmt.Sprintf(`{"query":%q,"limit":200}`, f.nonce)

	t.Run("neo4j_list_exists", func(t *testing.T) {
		code, raw, _ := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraSearchPath, body)
		requireStatus(t, "search", code, http.StatusOK, raw)
		data := decodeInfraData(t, raw)
		results, _ := data["results"].([]any)
		f.requireNoCollisionLeak(t, "neo4j search", stringField(results, "name"))
	})

	t.Run("shape_a", func(t *testing.T) {
		call := captureStatements(t, querycontract.GraphBackendNornicDB, g, infraSearchPath, body)[0]
		rows := l.raw(t, shapeAReferenceTimeout, call.Cypher, call.Params)
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, fmt.Sprint(row["name"]))
		}
		f.requireNoCollisionLeak(t, "shape-a search", names)
	})
}

// TestLiveInfraScopeNeo4jIDCollisionRelationships proves a granted anchor's
// neighbour list never carries a collision node, on both dialects.
func TestLiveInfraScopeNeo4jIDCollisionRelationships(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedIDCollisionFixture(t, l)
	g := f.grant()
	body := fmt.Sprintf(`{"entity_id":%q}`, f.name("anchor-id"))

	t.Run("neo4j_list_exists", func(t *testing.T) {
		code, raw, _ := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, body)
		requireStatus(t, "relationships", code, http.StatusOK, raw)
		data := decodeInfraData(t, raw)
		out, _ := data["outgoing"].([]any)
		f.requireNoCollisionLeak(t, "neo4j relationships", stringField(out, "target_name"))
	})

	t.Run("shape_a", func(t *testing.T) {
		row := shapeARelationshipsReference(t, l, g, body)
		if row == nil {
			t.Fatal("shape-a relationships: anchor not found")
		}
		out, _ := row["outgoing"].([]any)
		f.requireNoCollisionLeak(t, "shape-a relationships", stringField(out, "target_name"))
	})
}

// TestLiveInfraScopeNeo4jIDCollisionAnchors proves the relationships anchor
// lookup, in both dialects: a granted Repository resolves as an anchor, and a
// node whose id equals a granted scope id but that no Repository carries does
// not (the route answers the nonexistent-id 404, disclosing nothing).
func TestLiveInfraScopeNeo4jIDCollisionAnchors(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedIDCollisionFixture(t, l)
	g := f.grant()
	repoBody := fmt.Sprintf(`{"entity_id":%q}`, f.grantRepo)
	collideBody := fmt.Sprintf(`{"entity_id":%q}`, f.grantScop)

	t.Run("neo4j_list_exists", func(t *testing.T) {
		code, raw, _ := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, repoBody)
		requireStatus(t, "granted repository anchor", code, http.StatusOK, raw)
		if labels, _ := decodeInfraData(t, raw)["labels"].([]any); len(labels) != 1 || labels[0] != "Repository" {
			t.Fatalf("granted anchor labels = %v, want [Repository]; body = %.400s", labels, raw)
		}
		code, raw, _ = l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, collideBody)
		requireStatus(t, "collision anchor", code, http.StatusNotFound, raw)
	})

	t.Run("shape_a", func(t *testing.T) {
		row := shapeARelationshipsReference(t, l, g, repoBody)
		if row == nil {
			t.Fatal("granted repository anchor: no row; a granted Repository must resolve")
		}
		if labels, _ := row["labels"].([]any); len(labels) != 1 || labels[0] != "Repository" {
			t.Fatalf("granted anchor labels = %v, want [Repository]", row["labels"])
		}
		if row := shapeARelationshipsReference(t, l, g, collideBody); row != nil {
			t.Fatalf("collision anchor resolved: %v", row["labels"])
		}
	})
}
