// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships/story"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// backendDivergenceAllowlistPath is the divergence allowlist this test
// pins to the current query builders. A read-shape change that moves a
// builder must move the allowlist in the same change; this test fails
// first, while the advisory differential would only fail on its next
// hosted run.
const backendDivergenceAllowlistPath = "../../../../specs/backend-divergence-allowlist.v1.yaml"

// allowlistEntryCount7417 is the entry count after the #7417 refresh (10
// refreshed Neo4j-shaped story/CALLS/traversal entries plus 17 newly
// named entity-context and degree-read entries). Update it when entries
// are deliberately added or removed, alongside the diff that does so.
const allowlistEntryCount7417 = 63

// loadBackendDivergenceAllowlist parses the real allowlist, so the pins
// below run against the text the gate excuses.
func loadBackendDivergenceAllowlist(t *testing.T) *Allowlist {
	t.Helper()
	raw, err := os.ReadFile(backendDivergenceAllowlistPath)
	if err != nil {
		t.Fatalf("read %s: %v", backendDivergenceAllowlistPath, err)
	}
	allow, err := ParseAllowlist(raw)
	if err != nil {
		t.Fatalf("ParseAllowlist(%s): %v", backendDivergenceAllowlistPath, err)
	}
	return allow
}

// TestAllowlistEntryCountPins7417 keeps the allowlist's size deliberate:
// every added or removed entry updates this pin in the same change.
func TestAllowlistEntryCountPins7417(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	if len(allow.entries) != allowlistEntryCount7417 {
		t.Fatalf("allowlist entries = %d, want %d (update the pin with the deliberate add/remove)",
			len(allow.entries), allowlistEntryCount7417)
	}
}

// storyReadCase is one Neo4j-shaped story/CALLS/traversal read the corpus
// backend-diff run records, built by the production builder with the
// corpus request shape (unscoped, corpus depths).
type storyReadCase struct {
	name   string
	cypher string
	params map[string]any
}

// corpusStoryReads builds the ten refreshed #7417 reads through the
// production builders. Request shapes mirror the recorded corpus run:
// the IMPORTS story carries no repo scope, the INHERITS stories scope
// both endpoints, transitive INHERITS walks depth 4, transitive CALLS
// walks depth 4, and the call-chain traversal walks depth 2.
func corpusStoryReads() []storyReadCase {
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	importsReq := codemodel.RelationshipStoryRequest{EntityID: "e1", RelationshipType: "IMPORTS"}
	inheritsReq := codemodel.RelationshipStoryRequest{EntityID: "e1", RepoID: "repository:r1", RelationshipType: "INHERITS"}
	methodsReq := codemodel.RelationshipStoryRequest{EntityID: "e1"}
	depthReq := codemodel.RelationshipStoryRequest{EntityID: "e1", MaxDepth: 4}

	importsOut, importsOutParams := story.GraphCypher(importsReq, nil, "outgoing", unscoped)
	inheritsOut, inheritsOutParams := story.GraphCypher(inheritsReq, nil, "outgoing", unscoped)
	inheritsIn, inheritsInParams := story.GraphCypher(inheritsReq, nil, "incoming", unscoped)
	methods, methodsParams := story.ClassMethodsCypher(methodsReq, "e1", unscoped)
	transOut, transOutParams := story.InheritanceDepthCypher(depthReq, "e1", "outgoing", unscoped)
	transIn, transInParams := story.InheritanceDepthCypher(depthReq, "e1", "incoming", unscoped)
	entity := codemodel.RelationshipGraphRowCypherFromAnchor(
		codemodel.Neo4jEntityIDAnchor("e", "$entity_id"), unscoped)
	tcallsOut, tcallsOutParams := codemodel.BuildTransitiveRelationshipRowsCypher(
		"e1", "outgoing", 4, querycontract.GraphBackendNeo4j, unscoped)
	tcallsIn, tcallsInParams := codemodel.BuildTransitiveRelationshipRowsCypher(
		"e1", "incoming", 4, querycontract.GraphBackendNeo4j, unscoped)
	shortest, shortestParams := chain.BuildCallChainCypher(chain.Request{
		StartEntityID: "s1", EndEntityID: "e1", RepoID: "repository:r1", MaxDepth: 2,
	}, querycontract.GraphBackendNeo4j, unscoped)

	return []storyReadCase{
		{"imports-outgoing", importsOut, importsOutParams},
		{"inherits-outgoing", inheritsOut, inheritsOutParams},
		{"inherits-incoming", inheritsIn, inheritsInParams},
		{"class-methods", methods, methodsParams},
		{"transitive-inherits-outgoing", transOut, transOutParams},
		{"transitive-inherits-incoming", transIn, transInParams},
		{"entity-read", entity, map[string]any{"entity_id": "e1"}},
		{"transitive-calls-outgoing", tcallsOut, tcallsOutParams},
		{"transitive-calls-incoming", tcallsIn, tcallsInParams},
		{"call-chain-shortest", shortest, shortestParams},
	}
}

// TestAllowlistCoversCurrentNeo4jStoryReads pins the #7417 refresh to the
// production builders: each refreshed entry's statement must excuse the
// fingerprinted current builder output as a missing-kind divergence. A
// read-shape change without an allowlist refresh fails here instead of
// blinding the advisory differential with a stale entry.
func TestAllowlistCoversCurrentNeo4jStoryReads(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	for _, read := range corpusStoryReads() {
		t.Run(read.name, func(t *testing.T) {
			fingerprint, err := backendconformance.FingerprintStatement(read.cypher, read.params)
			if err != nil {
				t.Fatalf("FingerprintStatement(%s): %v", read.name, err)
			}
			diff := backendconformance.DifferentialDifference{
				Fingerprint: fingerprint,
				Kind:        backendconformance.DivergenceMissing,
				Detail:      "recorded on one backend only",
			}
			for _, entry := range allow.entries {
				if entryMatches(entry, diff) {
					return
				}
			}
			t.Fatalf("no allowlist entry excuses the current %s read; refresh specs/backend-divergence-allowlist.v1.yaml", read.name)
		})
	}
}

// TestAllowlistCarriesNoIDOrUIDAnchor pins the stale shape out: the
// pre-#7057 Neo4j story reads anchored on an id-OR-uid predicate the
// planner could not index, and no refreshed entry may still carry one.
func TestAllowlistCarriesNoIDOrUIDAnchor(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	anchors := []string{
		"OR source.uid = $entity_id",
		"OR target.uid = $entity_id",
		"OR class.uid = $entity_id",
		"OR e.uid = $entity_id",
		"OR start.uid = $start_entity_id",
		"OR end.uid = $end_entity_id",
	}
	for i, entry := range allow.entries {
		for _, anchor := range anchors {
			if strings.Contains(entry.Statement, anchor) {
				t.Errorf("entry %d still carries the pre-#7057 anchor %q; refresh it to the current builder text", i, anchor)
			}
		}
	}
}
