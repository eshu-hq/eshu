// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	testgraph "github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// neo4jWideAnchorStatementSHA256 is the SHA-256 of the unscoped Neo4j anchor
// statement the #7212 T1 shim measured on ops-qa (124 NodeUniqueIndexSeek
// operators, 124 db hits on a miss) and in the local cost bench. Production
// must send exactly that text, or the measurement proves nothing about it.
const neo4jWideAnchorStatementSHA256 = "e3a6d9edd0fc7ca56892c3fda781d397094b0c12b972b99a89e2f0917c72ed25"

var (
	wideUIDBranch  = regexp.MustCompile(`MATCH \(e:([\w|]+) \{uid: \$entity_id\}\) WHERE e\.id = \$entity_id`)
	wideIDBranch   = regexp.MustCompile(`MATCH \(e:([\w|]+) \{id: \$entity_id\}\)`)
	wideRankList   = regexp.MustCompile(`\[((?:"\w+"(?:, )?)+)\]\[i\]`)
	wideRangeUpper = regexp.MustCompile(`range\(0, (\d+)\)`)
	wideParam      = regexp.MustCompile(`\$(\w+)`)
)

// goldenWideAnchorClause reads the checked-in anchor clause of the measured
// statement. The file ends in one newline that is not part of the clause.
func goldenWideAnchorClause(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/neo4j_wide_anchor.cypher")
	if err != nil {
		t.Fatalf("read golden anchor clause: %v", err)
	}
	return strings.TrimSuffix(string(data), "\n")
}

// TestNeo4jEntityContextAnchorIsTheMeasuredStatement pins the production
// unscoped anchor statement byte for byte to the statement the T1 shim
// measured, by digest and against the checked-in clause.
func TestNeo4jEntityContextAnchorIsTheMeasuredStatement(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilterFromContext(context.Background())
	statements := (&Handler{GraphBackend: querycontract.GraphBackendNeo4j}).entityContextStatements(access)
	if len(statements) != 2 {
		t.Fatalf("Neo4j statements = %d, want 2 (anchor, fallback)", len(statements))
	}
	sum := sha256.Sum256([]byte(statements[0]))
	if got := hex.EncodeToString(sum[:]); got != neo4jWideAnchorStatementSHA256 {
		t.Errorf("anchor statement sha256 = %s, want %s (the measured candidate)", got, neo4jWideAnchorStatementSHA256)
	}
	if got, want := neo4jEntityContextAnchor(), goldenWideAnchorClause(t); got != want {
		t.Errorf("anchor clause differs from testdata/neo4j_wide_anchor.cypher:\n%s\n----\n%s", got, want)
	}
	if got, want := statements[1], entityContextCypher(entityContextFallbackAnchor, access); got != want {
		t.Errorf("fallback statement changed:\n%s\n----\n%s", got, want)
	}
}

// TestNeo4jEntityContextScopedShapeKeepsTheWideAnchor: the scoped caller
// shape is the same widened anchor with the grant filter on the Repository
// enrichment, then the unchanged fallback. Only $entity_id and the grant
// lists are bound.
func TestNeo4jEntityContextScopedShapeKeepsTheWideAnchor(t *testing.T) {
	t.Parallel()

	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	})
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if !access.Scoped() {
		t.Fatal("access filter is not scoped")
	}
	statements := (&Handler{GraphBackend: querycontract.GraphBackendNeo4j}).entityContextStatements(access)
	if len(statements) != 2 {
		t.Fatalf("scoped Neo4j statements = %d, want 2", len(statements))
	}
	if got, want := statements[0], entityContextStatement(goldenWideAnchorClause(t), access); got != want {
		t.Errorf("scoped anchor statement:\n%s\n----\nwant\n%s", got, want)
	}
	if !strings.Contains(statements[0], "WHERE "+access.GraphCondition("r")) {
		t.Errorf("scoped anchor lacks the Repository grant filter:\n%s", statements[0])
	}
	if got, want := statements[1], entityContextCypher(entityContextFallbackAnchor, access); got != want {
		t.Errorf("scoped fallback statement changed:\n%s\n----\n%s", got, want)
	}
	for _, statement := range statements {
		for _, param := range wideParam.FindAllStringSubmatch(statement, -1) {
			switch param[1] {
			case "entity_id", "allowed_repository_ids", "allowed_scope_ids":
			default:
				t.Errorf("statement binds unexpected parameter $%s", param[1])
			}
		}
	}
}

// TestNeo4jEntityContextAnchorLabelSetsAreTheSchemaConstraints derives the
// expected label sets from the Neo4j schema DDL, never from the anchor's own
// lists: every label with a uid uniqueness constraint seeks on uid, every
// other label with an id uniqueness constraint seeks on id. Directory has
// neither and stays with the unlabeled fallback. The rank list covers each
// label once: the constrained EntityContextAnchorLabels in loop order, then
// the rest alphabetically.
func TestNeo4jEntityContextAnchorLabelSetsAreTheSchemaConstraints(t *testing.T) {
	t.Parallel()

	uidDDL := schemaUIDConstrainedLabels(t, graph.SchemaBackendNeo4j)
	idDDL := schemaIDConstrainedLabels(t, graph.SchemaBackendNeo4j)
	var wantUID, wantID []string
	for label := range uidDDL {
		wantUID = append(wantUID, label)
	}
	for label := range idDDL {
		if !uidDDL[label] {
			wantID = append(wantID, label)
		}
	}
	sort.Strings(wantUID)
	sort.Strings(wantID)

	anchor := neo4jEntityContextAnchor()
	uidMatch := wideUIDBranch.FindStringSubmatch(anchor)
	idMatch := wideIDBranch.FindStringSubmatch(anchor)
	if uidMatch == nil || idMatch == nil {
		t.Fatalf("anchor lacks the uid or id seek branch:\n%s", anchor)
	}
	gotUID, gotID := labelSet(uidMatch[1]), labelSet(idMatch[1])
	if strings.Join(gotUID, ",") != strings.Join(wantUID, ",") {
		t.Errorf("uid seek labels (%d) = %v, want every schema uid-constrained label (%d) %v", len(gotUID), gotUID, len(wantUID), wantUID)
	}
	if strings.Join(gotID, ",") != strings.Join(wantID, ",") {
		t.Errorf("id seek labels (%d) = %v, want every schema id-constrained label without a uid constraint (%d) %v", len(gotID), gotID, len(wantID), wantID)
	}
	inUID := map[string]bool{}
	for _, label := range gotUID {
		inUID[label] = true
	}
	for _, label := range gotID {
		if inUID[label] {
			t.Errorf("label %s is in both the uid and the id branch", label)
		}
	}
	if strings.Contains(anchor, "Directory") {
		t.Errorf("anchor mentions Directory, which has no id or uid index:\n%s", anchor)
	}

	rankMatch := wideRankList.FindStringSubmatch(anchor)
	if rankMatch == nil {
		t.Fatalf("anchor lacks the precedence rank list:\n%s", anchor)
	}
	gotRank := strings.Split(strings.ReplaceAll(rankMatch[1], `"`, ""), ", ")
	var wantRank, rest []string
	ranked := map[string]bool{}
	for _, label := range EntityContextAnchorLabels {
		if uidDDL[label] || idDDL[label] {
			wantRank = append(wantRank, label)
			ranked[label] = true
		}
	}
	for _, label := range append(append([]string{}, wantUID...), wantID...) {
		if !ranked[label] {
			rest = append(rest, label)
		}
	}
	sort.Strings(rest)
	wantRank = append(wantRank, rest...)
	if strings.Join(gotRank, ",") != strings.Join(wantRank, ",") {
		t.Errorf("rank order = %v, want %v", gotRank, wantRank)
	}
	if len(gotRank) != len(gotUID)+len(gotID) {
		t.Errorf("rank list has %d labels, want one per seek label (%d)", len(gotRank), len(gotUID)+len(gotID))
	}
	upper := wideRangeUpper.FindStringSubmatch(anchor)
	if upper == nil || upper[1] != strconv.Itoa(len(gotRank)-1) {
		t.Errorf("rank range upper bound = %v, want %d", upper, len(gotRank)-1)
	}
}

// wideAnchorGraph models a graph holding one node, with the given label and
// id == uid. The fake answers exactly the statements whose seek can reach the
// node: the CALL () anchor when its label is in either branch, and the
// unlabeled fallback always. It returns the statements sent, in order.
func wideAnchorGraph(entityID, label string) (*[]string, testgraph.FakeGraphReader) {
	var calls []string
	reader := testgraph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			calls = append(calls, cypher)
			if params["entity_id"] != entityID {
				return nil, nil
			}
			row := map[string]any{"id": entityID, "labels": []any{label}, "name": entityID, "relationships": []any{}}
			if strings.Contains(cypher, entityContextFallbackAnchor) {
				return row, nil
			}
			for _, branch := range []*regexp.Regexp{wideUIDBranch, wideIDBranch} {
				if m := branch.FindStringSubmatch(cypher); m != nil {
					for _, got := range strings.Split(m[1], "|") {
						if got == label {
							return row, nil
						}
					}
				}
			}
			return nil, nil
		},
	}
	return &calls, reader
}

// TestNeo4jEntityContextResolvesFormerFallbackLabelsOnTheAnchor pins the #7212
// T1 fix: an id on a constrained label outside the old 15 (a uid-constrained
// TerraformVariable, an id-constrained Endpoint) resolves on the indexed
// anchor, statements[0], and the whole-graph fallback is never sent.
func TestNeo4jEntityContextResolvesFormerFallbackLabelsOnTheAnchor(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"TerraformVariable", "Endpoint"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			entityID := "id-" + label
			calls, reader := wideAnchorGraph(entityID, label)
			handler := &Handler{
				GraphBackend: querycontract.GraphBackendNeo4j,
				Neo4j:        reader,
				Profile:      querycontract.ProfileLocalAuthoritative,
			}
			obs := observeEntityContext(t, handler, entityID)
			if obs.rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", obs.rec.Code, obs.rec.Body.String())
			}
			assertResolved(t, obs, "anchor", 1)
			for _, cypher := range *calls {
				if strings.Contains(cypher, entityContextFallbackAnchor) {
					t.Errorf("the unlabeled fallback was sent for a %s id", label)
				}
			}
		})
	}
}
