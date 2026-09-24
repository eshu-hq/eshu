// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestOverrideRowsCypherUsesEvaluatedLabelFilter pins the OVERRIDES story read
// to the endpoint label filter NornicDB v1.3.3 evaluates in the WHERE of a
// relationship MATCH (#6786 X11), for the unscoped and the scoped+language
// renderings, and requires every override label on both endpoints.
func TestOverrideRowsCypherUsesEvaluatedLabelFilter(t *testing.T) {
	t.Parallel()

	scoped := querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repository:r"},
		Allowed:              map[string]struct{}{"repository:r": {}},
	}
	cases := map[string]struct {
		req    codemodel.RelationshipStoryRequest
		access querycontract.RepositoryAccessFilter
	}{
		"unscoped":        {req: codemodel.RelationshipStoryRequest{RepoID: "repository:r"}, access: querycontract.RepositoryAccessFilter{AllScopes: true}},
		"scoped language": {req: codemodel.RelationshipStoryRequest{RepoID: "repository:r", Language: "go"}, access: scoped},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cypher, params := OverrideRowsCypher(tc.req, tc.access)
			graph.AssertCypherHasNoIgnoredLabelPredicate(t, cypher)
			graph.AssertCypherHasNoBrokenAndOr(t, cypher)
			if _, ok := params["override_labels"]; ok {
				t.Fatalf("params still bind override_labels, which no statement term reads: %v", params)
			}
			for _, alias := range []string{"source", "target"} {
				for _, label := range OverrideNodeLabels() {
					if term := "'" + label + "' IN labels(" + alias + ")"; !strings.Contains(cypher, term) {
						t.Fatalf("cypher is missing endpoint filter %q:\n%s", term, cypher)
					}
				}
			}
		})
	}
}
