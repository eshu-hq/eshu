// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetRelationshipEdgesResolvesMixedCaseVerb guards issue #5450 P1-B:
// relationshipVerbByName is keyed by the catalog's raw verb string, but every
// lookup site (getRelationshipEdges here, relationshipEvidenceTargetAttributable
// in evidence.go) upper-cases the input verb first. AWS_lambda_function_uses_image
// is the only catalog entry whose canonical verb is not already all-uppercase,
// so before the fix its mixed-case lookup key never matched an upper-cased
// input and every casing of the verb returned 400 "unknown relationship verb".
// The persisted Cypher relationship type and the API's echoed "verb" field
// must stay mixed-case (they are the literal graph token / UI display value),
// so this only asserts the map is keyed case-insensitively, not that the
// catalog entry itself changes case.
func TestGetRelationshipEdgesResolvesMixedCaseVerb(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		{"source_id": "lambda-1", "source_name": "lambda-1", "target_id": "img-1", "target_name": "img-1", "evidence": ""},
	}
	reader := &fakeRelationshipsGraphReader{
		edgesByVerb: map[string][]map[string]any{"AWS_lambda_function_uses_image": edges},
	}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	for _, verbInput := range []string{"AWS_lambda_function_uses_image", "AWS_LAMBDA_FUNCTION_USES_IMAGE"} {
		t.Run(verbInput, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"verb": verbInput})
			req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			handler.getRelationshipEdges(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var env ResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			data, ok := env.Data.(map[string]any)
			if !ok {
				t.Fatalf("data is not an object: %T", env.Data)
			}
			if got, want := data["verb"], "AWS_lambda_function_uses_image"; got != want {
				t.Fatalf("verb = %v, want %v (exact stored case)", got, want)
			}
		})
	}
}

// TestRelationshipEdgesScopeBindsEdgeScopeForLambdaImageVerb guards issue
// #5450 P1-C: AWS_lambda_function_uses_image's authoritative reducer scope
// lives on the EDGE (rel.scope_id, see CloudResourceContainerImageEdgeWriter),
// not on either endpoint node, so the endpoint-only scope predicate alone
// silently returns zero edges for every scoped caller even when granted the
// exact scope that produced them. A scoped caller must be admitted via a
// direct `r.scope_id IN $allowed_scope_ids` compare OR'd with the
// endpoint-scope group -- additive only, never replacing the endpoint check --
// and the OR must stay confined so it cannot swallow the source_tool AND in
// the filtered variant. Every other catalog verb's scoped Cypher must stay
// byte-identical to the pre-#5450-P1-C shape (no r.scope_id reference at all),
// so this also guards against the edge-scope OR leaking onto an unrelated
// verb.
//
// The `r.scope_id IN $allowed_scope_ids` term is folded into the SAME flat
// endpoint OR-group as its final disjunct -- one atomically parenthesized chain
// "(s.repo_id ... OR ... OR r.scope_id IN $allowed_scope_ids)" -- rather than a
// nested "(endpoint group) OR term". The flat group keeps the shape identical to
// the endpoint predicate every other scoped verb already ships (one disjunct
// wider), and its single set of parens AND-combines safely after the
// source_tool filter without the OR detaching (Cypher's AND binds tighter than
// OR). The runtime edge-scope proof is the live NornicDB test
// TestRelationshipEdgesScopedCallerSeesLambdaImageEdgeLive, not this
// generated-Cypher shape assertion.
func TestRelationshipEdgesScopeBindsEdgeScopeForLambdaImageVerb(t *testing.T) {
	t.Parallel()

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-a"},
		allowed:              map[string]struct{}{"repo-a": {}},
	}

	for _, entry := range relationshipVerbCatalog {
		unfiltered := relationshipEdgesCypher(entry, scoped)
		filtered := relationshipEdgesCypherFiltered(entry, scoped)

		if entry.verb != "AWS_lambda_function_uses_image" {
			if strings.Contains(unfiltered, "r.scope_id") {
				t.Fatalf("%s unfiltered scoped cypher must not reference r.scope_id: %s", entry.verb, unfiltered)
			}
			if strings.Contains(filtered, "r.scope_id") {
				t.Fatalf("%s filtered scoped cypher must not reference r.scope_id: %s", entry.verb, filtered)
			}
			continue
		}

		// Unfiltered: WHERE opens the single flat OR-group with the source
		// endpoint disjuncts and closes it with the edge-scope disjunct, so the
		// edge check is additive to (never replaces) the endpoint check.
		whereLine := strings.SplitN(unfiltered, "\n", 2)[1]
		if !strings.HasPrefix(whereLine, "WHERE (s.repo_id IN $allowed_repository_ids") {
			t.Fatalf("unfiltered scoped cypher for %s must open a flat endpoint OR-group: %s", entry.verb, unfiltered)
		}
		if !strings.Contains(unfiltered, " OR r.scope_id IN $allowed_scope_ids)\n") {
			t.Fatalf("unfiltered scoped cypher for %s must fold edge-scope in as the final OR-group disjunct: %s", entry.verb, unfiltered)
		}

		// Filtered: source_tool stays AND'd at the top level, ahead of the same
		// flat edge-scope-bearing OR-group.
		if !strings.Contains(filtered, "WHERE r.source_tool = $source_tool AND (s.repo_id IN $allowed_repository_ids") {
			t.Fatalf("filtered scoped cypher for %s must keep source_tool AND'd ahead of the flat endpoint OR-group: %s", entry.verb, filtered)
		}
		if !strings.Contains(filtered, " OR r.scope_id IN $allowed_scope_ids)\n") {
			t.Fatalf("filtered scoped cypher for %s must fold edge-scope in as the final OR-group disjunct: %s", entry.verb, filtered)
		}
	}
}

// TestRelationshipVerbCatalogEdgeScopeInvariant enforces the invariant
// relationshipEdgesScopeExpr relies on: an edgeScopeAttributable verb must be
// endpoint-source-only (!targetAttributable), so its endpoint scope group is the
// single source-alias disjunct list the edge-scope term is folded into.
func TestRelationshipVerbCatalogEdgeScopeInvariant(t *testing.T) {
	t.Parallel()
	for _, entry := range relationshipVerbCatalog {
		if entry.edgeScopeAttributable && entry.targetAttributable {
			t.Fatalf("verb %s is both edgeScopeAttributable and targetAttributable; the edge-scope fold assumes a single source-alias endpoint group", entry.verb)
		}
	}
}
