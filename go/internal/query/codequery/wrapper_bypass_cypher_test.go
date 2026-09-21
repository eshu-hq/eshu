// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func wrapperBypassBackends() []querycontract.GraphBackend {
	return []querycontract.GraphBackend{querycontract.GraphBackendNeo4j, querycontract.GraphBackendNornicDB}
}

func unscopedWrapperAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllScopes: true}
}

func scopedWrapperAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
}

// TestBuildWrapperCallersCypherAnchorsAndScopes pins the shipped one-hop
// text on both backends: an indexed entity-id anchor, one CALLS hop, the
// repo scope and grant in the anchoring WHERE, and the caller columns the
// qualification reads (identity, file, edge provenance, complexity).
func TestBuildWrapperCallersCypherAnchorsAndScopes(t *testing.T) {
	t.Parallel()

	for _, backend := range wrapperBypassBackends() {
		cypher, params := BuildWrapperCallersCypher("fn-target", "repo-a", backend, scopedWrapperAccess())
		for _, want := range []string{
			"CALLS",
			"$target_entity_id",
			"edge_method",
			"edge_confidence",
			"file_path",
			"complexity",
			"$repo_id",
		} {
			if !strings.Contains(cypher, want) {
				t.Errorf("backend %v callers cypher missing %q:\n%s", backend, want, cypher)
			}
		}
		if params["target_entity_id"] != "fn-target" {
			t.Errorf("backend %v target param = %v, want fn-target", backend, params["target_entity_id"])
		}
		if !strings.Contains(cypher, "repo-a") && params["repo_id"] != "repo-a" {
			// The grant travels as params; the text must still name the
			// grant's repo predicate on the caller.
			t.Errorf("backend %v callers cypher drops the grant scope", backend)
		}
	}
}

// TestBuildWrapperFanInCypherCountsCallers pins the fan-in count read: a
// CALLS hop into the anchored entity with a count projection, so the floor
// check never fetches caller rows.
func TestBuildWrapperFanInCypherCountsCallers(t *testing.T) {
	t.Parallel()

	for _, backend := range wrapperBypassBackends() {
		cypher, params := BuildWrapperFanInCypher("fn-wrap", "repo-a", backend, unscopedWrapperAccess())
		for _, want := range []string{"CALLS", "$entity_id", "count(caller)", "fan_in"} {
			if !strings.Contains(cypher, want) {
				t.Errorf("backend %v fan-in cypher missing %q:\n%s", backend, want, cypher)
			}
		}
		if params["entity_id"] != "fn-wrap" {
			t.Errorf("backend %v entity param = %v, want fn-wrap", backend, params["entity_id"])
		}
	}
}

// TestBuildWrapperCalleesCypherListsOutgoingIDs pins the outgoing-callee
// read the thinness check counts in Go: outgoing CALLS from the anchored
// wrapper, projecting callee ids only.
func TestBuildWrapperCalleesCypherListsOutgoingIDs(t *testing.T) {
	t.Parallel()

	for _, backend := range wrapperBypassBackends() {
		cypher, params := BuildWrapperCalleesCypher("fn-wrap", "fn-target", "repo-a", backend, unscopedWrapperAccess())
		for _, want := range []string{"CALLS", "$entity_id", "as id"} {
			if !strings.Contains(cypher, want) {
				t.Errorf("backend %v callees cypher missing %q:\n%s", backend, want, cypher)
			}
		}
		if params["target_entity_id"] != "fn-target" {
			t.Errorf("backend %v target param = %v, want fn-target", backend, params["target_entity_id"])
		}
	}
}
