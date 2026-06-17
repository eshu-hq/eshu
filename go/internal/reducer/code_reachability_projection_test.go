package reducer

import "testing"

func TestBuildCodeReachabilityRowsComputesTransitiveReachableSet(t *testing.T) {
	rows := BuildCodeReachabilityRows(CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:  "entity:root",
			RootKinds: []string{"go.main_function"},
		}},
		Edges: []CodeReachabilityEdge{
			{
				SourceEntityID:   "entity:root",
				TargetEntityID:   "entity:service",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			},
			{
				SourceEntityID:   "entity:service",
				TargetEntityID:   "entity:helper",
				RelationshipType: "REFERENCES",
				ResolutionMethod: "repo_unique_name",
			},
			{
				SourceEntityID:   "entity:helper",
				TargetEntityID:   "entity:too-deep",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			},
		},
		MaxDepth: 2,
	})

	byEntity := make(map[string]CodeReachabilityRow)
	for _, row := range rows {
		byEntity[row.EntityID] = row
	}

	if _, ok := byEntity["entity:too-deep"]; ok {
		t.Fatalf("unexpected row past max depth: %#v", rows)
	}
	if got, want := byEntity["entity:root"].Depth, 0; got != want {
		t.Fatalf("root depth = %d, want %d", got, want)
	}
	if got, want := byEntity["entity:service"].Depth, 1; got != want {
		t.Fatalf("service depth = %d, want %d", got, want)
	}
	helper := byEntity["entity:helper"]
	if got, want := helper.Depth, 2; got != want {
		t.Fatalf("helper depth = %d, want %d", got, want)
	}
	if got, want := helper.State, CodeReachabilityStateAmbiguous; got != want {
		t.Fatalf("helper state = %q, want %q", got, want)
	}
	if got, want := helper.MinResolutionMethod, "repo_unique_name"; got != want {
		t.Fatalf("helper method = %q, want %q", got, want)
	}
}

func TestBuildCodeReachabilityRowsDeltaScopesAffectedSlice(t *testing.T) {
	rows := BuildCodeReachabilityRows(CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots: []CodeReachabilityRoot{{
			EntityID:  "entity:root",
			RootKinds: []string{"go.main_function"},
		}},
		Edges: []CodeReachabilityEdge{
			{SourceEntityID: "entity:root", TargetEntityID: "entity:a", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:a", TargetEntityID: "entity:b", RelationshipType: "CALLS", ResolutionMethod: "scip"},
			{SourceEntityID: "entity:root", TargetEntityID: "entity:c", RelationshipType: "CALLS", ResolutionMethod: "scip"},
		},
		AffectedEntityIDs: []string{"entity:a"},
		MaxDepth:          5,
	})

	if got, want := len(rows), 2; got != want {
		t.Fatalf("delta rows = %d, want %d: %#v", got, want, rows)
	}
	gotEntities := map[string]bool{}
	for _, row := range rows {
		gotEntities[row.EntityID] = true
	}
	for _, want := range []string{"entity:a", "entity:b"} {
		if !gotEntities[want] {
			t.Fatalf("delta rows missing %s: %#v", want, rows)
		}
	}
	if gotEntities["entity:root"] || gotEntities["entity:c"] {
		t.Fatalf("delta rows included unaffected entities: %#v", rows)
	}
}
