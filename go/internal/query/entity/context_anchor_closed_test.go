// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	testgraph "github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// The schema-closed anchor (#7212 PR-B): on Neo4j the labeled CALL () anchor
// is the only entity-context statement. The unlabeled whole-graph fallback is
// gone because the census (anchor.Classify, CensusCypher, and the reducer
// gauge eshu_dp_graph_id_anchor_unreachable_nodes) proves every id-bearing node
// a writer produces is reachable through the anchor. These tests hold the
// anchor and the census definition together on every label the census knows.

// anchorModelReader is a fake Neo4j that evaluates the CALL () anchor the way
// Neo4j does, over the given nodes: the uid branch matches a node on one of its
// labels whose uid and id both equal $entity_id, and the id branch matches a
// node on one of its labels whose id equals $entity_id. The unlabeled fallback,
// if it were ever sent, matches any node with that id, so a test can tell a
// fallback answer from an anchor answer. It records every statement sent.
func anchorModelReader(t *testing.T, nodes []anchor.Node) (*[]string, testgraph.FakeGraphReader) {
	t.Helper()
	var calls []string
	reader := testgraph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			calls = append(calls, cypher)
			id, _ := params["entity_id"].(string)
			var uidBranch, idBranch []string
			if m := wideUIDBranch.FindStringSubmatch(cypher); m != nil {
				uidBranch = strings.Split(m[1], "|")
			}
			if m := wideIDBranch.FindStringSubmatch(cypher); m != nil {
				idBranch = strings.Split(m[1], "|")
			}
			unlabeled := strings.Contains(cypher, entityContextUnlabeledAnchor)
			for _, node := range nodes {
				if node.ID == nil || *node.ID != id {
					continue
				}
				uidMatches := node.UID != nil && *node.UID == id
				hit := unlabeled
				for _, label := range node.Labels {
					if slices.Contains(idBranch, label) || (uidMatches && slices.Contains(uidBranch, label)) {
						hit = true
					}
				}
				if hit {
					labels := make([]any, len(node.Labels))
					for i, label := range node.Labels {
						labels[i] = label
					}
					return map[string]any{"id": id, "labels": labels, "name": id, "relationships": []any{}}, nil
				}
			}
			return nil, nil
		},
	}
	return &calls, reader
}

func strPtr(s string) *string { return &s }

// getEntityContext drives GetEntityContext once with no instruments and
// returns the recorder.
func getEntityContext(h *Handler, entityID string, authCtx auth.AuthContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+entityID+"/context", nil)
	if authCtx.Mode != "" {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	}
	req.SetPathValue("entity_id", entityID)
	rec := httptest.NewRecorder()
	h.GetEntityContext(rec, req)
	return rec
}

// TestNeo4jEntityContextSendsOnlyTheIndexedAnchor: on Neo4j a request that
// misses everything sends one statement, the indexed CALL () anchor, for both
// caller shapes. The unlabeled `MATCH (e)` fallback (2.24M db hits on the QA
// environment) is never sent.
func TestNeo4jEntityContextSendsOnlyTheIndexedAnchor(t *testing.T) {
	t.Parallel()

	for _, scoped := range []bool{false, true} {
		calls, _ := neo4jContextRequest(t, "entity-miss", scoped, func(string) (map[string]any, error) {
			return nil, nil
		})
		if len(calls) != 1 {
			t.Fatalf("scoped=%v: graph reads on a full miss = %d, want 1 (the indexed anchor only)", scoped, len(calls))
		}
		if !strings.Contains(calls[0], "CALL () {") {
			t.Errorf("scoped=%v: the only read is not the CALL () anchor:\n%s", scoped, calls[0])
		}
		if strings.Contains(calls[0], entityContextUnlabeledAnchor) {
			t.Errorf("scoped=%v: the unlabeled fallback was sent:\n%s", scoped, calls[0])
		}
	}
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	for _, statement := range (&Handler{GraphBackend: querycontract.GraphBackendNeo4j}).EntityContextStatements(unscoped) {
		if strings.Contains(statement, entityContextUnlabeledAnchor) {
			t.Errorf("EntityContextStatements carries the unlabeled fallback on Neo4j:\n%s", statement)
		}
	}
}

// TestNeo4jEntityContextMissKeepsTheNotFoundShape: a true miss on Neo4j still
// answers the same 404 body, headers, and resolved_by=none as the per-label
// loop's miss, after one statement instead of two.
func TestNeo4jEntityContextMissKeepsTheNotFoundShape(t *testing.T) {
	t.Parallel()

	newHandler := func(backend querycontract.GraphBackend) *Handler {
		return &Handler{
			GraphBackend: backend,
			Neo4j: testgraph.FakeGraphReader{
				RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) { return nil, nil },
			},
			Content:              entityContextFakeContentStore{},
			ContentRelationships: scriptedContentRelationshipBuilder{},
			Profile:              querycontract.ProfileLocalAuthoritative,
		}
	}
	neo4jObs := observeEntityContext(t, newHandler(querycontract.GraphBackendNeo4j), "missing")
	loopRec := getEntityContext(newHandler(querycontract.GraphBackendNornicDB), "missing", auth.AuthContext{})

	if neo4jObs.rec.Code != http.StatusNotFound || loopRec.Code != http.StatusNotFound {
		t.Fatalf("status: neo4j = %d, loop = %d, want 404 on both", neo4jObs.rec.Code, loopRec.Code)
	}
	if got, want := neo4jObs.rec.Body.String(), loopRec.Body.String(); got != want {
		t.Errorf("Neo4j miss body = %s, want the loop's %s", got, want)
	}
	if got, want := neo4jObs.rec.Header(), loopRec.Header(); !reflect.DeepEqual(got, want) {
		t.Errorf("Neo4j miss headers = %v, want the loop's %v", got, want)
	}
	assertResolved(t, neo4jObs, "none", 1)
}

// TestNeo4jEntityContextAnchorAgreesWithTheCensusOnEveryLabel holds the anchor
// and the census definition (anchor.Classify) together on every label the
// census counts, for three node shapes per label: the canonical shape (uid
// labels carry id == uid, id labels carry id), an id with no uid, and an id
// that differs from its uid. A node the census calls reachable must resolve on
// the anchor in one statement; a node it calls unreachable must answer the
// not-found shape in one statement. The census gauge reading zero unreachable
// nodes on a deployment is then the proof that no answer was lost there.
func TestNeo4jEntityContextAnchorAgreesWithTheCensusOnEveryLabel(t *testing.T) {
	t.Parallel()

	uidSet, idSet := map[string]bool{}, map[string]bool{}
	for _, label := range anchor.UIDLabels() {
		uidSet[label] = true
	}
	for _, label := range anchor.IDLabels() {
		idSet[label] = true
	}
	labels := make([]string, 0, len(anchor.Labels()))
	for label := range anchor.Labels() {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	if len(labels) == 0 {
		t.Fatal("anchor.Labels() is empty: the census knows no label")
	}

	type shape struct {
		name string
		node func(label, id string) anchor.Node
	}
	shapes := []shape{
		{"canonical", func(label, id string) anchor.Node {
			node := anchor.Node{Labels: []string{label}, ID: strPtr(id)}
			if uidSet[label] {
				node.UID = strPtr(id)
			}
			return node
		}},
		{"id-only", func(label, id string) anchor.Node {
			return anchor.Node{Labels: []string{label}, ID: strPtr(id)}
		}},
		{"split-uid", func(label, id string) anchor.Node {
			return anchor.Node{Labels: []string{label}, ID: strPtr(id), UID: strPtr(id + "-uid")}
		}},
	}
	// Directory is keyed by path and has no uid or id constraint; the
	// canonical writer never sets its id, and the census counts a Directory
	// that has one as unreachable.
	cases := []struct {
		label string
		shape shape
	}{{"Directory", shapes[1]}}
	for _, label := range labels {
		for _, s := range shapes {
			cases = append(cases, struct {
				label string
				shape shape
			}{label, s})
		}
	}

	reachableCanonical := 0
	for _, tc := range cases {
		id := "census-" + tc.label + "-" + tc.shape.name
		node := tc.shape.node(tc.label, id)
		reachable := anchor.Classify(node, uidSet, idSet) != anchor.Unreachable
		calls, reader := anchorModelReader(t, []anchor.Node{node})
		h := &Handler{
			GraphBackend:         querycontract.GraphBackendNeo4j,
			Neo4j:                reader,
			Content:              entityContextFakeContentStore{},
			ContentRelationships: scriptedContentRelationshipBuilder{},
			Profile:              querycontract.ProfileLocalAuthoritative,
		}
		rec := getEntityContext(h, id, auth.AuthContext{})
		if len(*calls) != 1 {
			t.Errorf("%s %s: graph reads = %d, want 1", tc.label, tc.shape.name, len(*calls))
		}
		switch {
		case reachable && rec.Code != http.StatusOK:
			t.Errorf("%s %s: census says reachable, anchor answered %d: %s", tc.label, tc.shape.name, rec.Code, rec.Body.String())
		case !reachable && rec.Code != http.StatusNotFound:
			t.Errorf("%s %s: census says unreachable, anchor answered %d, want 404", tc.label, tc.shape.name, rec.Code)
		case reachable:
			var body struct {
				Data struct {
					Labels []string `json:"labels"`
				} `json:"data"`
				Labels []string `json:"labels"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := body.Labels
			if len(got) == 0 {
				got = body.Data.Labels
			}
			if len(got) == 0 || got[0] != tc.label {
				t.Errorf("%s %s: resolved labels = %v", tc.label, tc.shape.name, got)
			}
			if tc.shape.name == "canonical" {
				reachableCanonical++
			}
		}
	}
	if reachableCanonical != len(labels) {
		t.Errorf("canonical nodes resolved on the anchor = %d, want every census label (%d)", reachableCanonical, len(labels))
	}
}
