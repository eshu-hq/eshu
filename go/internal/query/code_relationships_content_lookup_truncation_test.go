// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// lookupCapContentStore serves one entity by id plus a fixed candidate list
// for the name and referencing-component lookups. Like the production SQL it
// returns at most `limit` rows, so a lookup that asks for exactly the response
// ceiling can never see that more rows exist (#7151).
type lookupCapContentStore struct {
	fakePortContentStore
	entity     EntityContent
	candidates []EntityContent
}

func (s lookupCapContentStore) GetEntityContent(_ context.Context, entityID string) (*EntityContent, error) {
	if entityID != s.entity.EntityID {
		return nil, nil
	}
	found := s.entity
	return &found, nil
}

func (s lookupCapContentStore) capped(limit int) []EntityContent {
	if limit > 0 && limit < len(s.candidates) {
		return s.candidates[:limit]
	}
	return s.candidates
}

func (s lookupCapContentStore) SearchEntitiesByName(_ context.Context, _, _, _ string, limit int) ([]EntityContent, error) {
	return s.capped(limit), nil
}

func (s lookupCapContentStore) SearchEntitiesReferencingComponent(_ context.Context, _, _ string, limit int) ([]EntityContent, error) {
	return s.capped(limit), nil
}

// lookupCandidates builds n distinct rows for a lookup; shape adjusts each.
func lookupCandidates(n int, shape func(i int, e *EntityContent)) []EntityContent {
	rows := make([]EntityContent, 0, n)
	for i := 0; i < n; i++ {
		e := EntityContent{
			EntityID: fmt.Sprintf("cand-%03d", i), RepoID: "repo-1",
			RelativePath: "src/a.rs", EntityName: fmt.Sprintf("cand%03d", i),
		}
		shape(i, &e)
		rows = append(rows, e)
	}
	return rows
}

// TestHandleRelationshipsContentLookupsReportCeilingClip proves every content
// lookup capped at contentRelationshipLimit reports the direction it clipped:
// limit+1 candidates come back as exactly limit relationships with the flag
// set, and exactly limit candidates are complete (#7151).
func TestHandleRelationshipsContentLookupsReportCeilingClip(t *testing.T) {
	t.Parallel()

	jsxSource := EntityContent{
		EntityID: "page-1", RepoID: "repo-1", RelativePath: "src/page.tsx",
		EntityType: "Function", EntityName: "Page",
		Metadata: map[string]any{"jsx_component_usage": []any{"Button"}},
	}
	component := EntityContent{
		EntityID: "button-1", RepoID: "repo-1", RelativePath: "src/button.tsx",
		EntityType: "Component", EntityName: "Button",
	}
	kustomize := EntityContent{
		EntityID: "kustomize-1", RepoID: "repo-1", RelativePath: "overlay/kustomization.yaml",
		EntityType: "KustomizeOverlay", EntityName: "overlay",
		Metadata: map[string]any{"patch_targets": []any{"Deployment/web"}},
	}
	implBlock := EntityContent{
		EntityID: "impl-1", RepoID: "repo-1", RelativePath: "src/a.rs",
		EntityType: "ImplBlock", EntityName: "Point",
	}
	rustFn := EntityContent{
		EntityID: "fn-1", RepoID: "repo-1", RelativePath: "src/a.rs",
		EntityType: "Function", EntityName: "new",
		Metadata: map[string]any{"impl_context": "Point"},
	}
	plain := func(_ int, e *EntityContent) { e.EntityType = "Component" }
	deployment := func(_ int, e *EntityContent) {
		e.EntityType = "K8sResource"
		e.Metadata = map[string]any{"kind": "Deployment"}
	}
	rustMember := func(_ int, e *EntityContent) {
		e.EntityType = "Function"
		e.Metadata = map[string]any{"impl_context": "Point"}
	}
	rustImpl := func(_ int, e *EntityContent) { e.EntityType = "ImplBlock" }

	tests := []struct {
		name     string
		entity   EntityContent
		shape    func(int, *EntityContent)
		outgoing bool // which direction the lookup fills
		edgeType string
	}{
		{"jsx component names", jsxSource, plain, true, "REFERENCES"},
		{"referencing components", component, plain, false, "REFERENCES"},
		{"kustomize patch targets", kustomize, deployment, true, "PATCHES"},
		{"rust impl functions", implBlock, rustMember, true, "CONTAINS"},
		{"rust function impl blocks", rustFn, rustImpl, false, "CONTAINS"},
	}
	for _, tt := range tests {
		tt := tt
		for _, tc := range []struct {
			label      string
			candidates int
			clipped    bool
		}{
			{"over the ceiling", contentRelationshipLimit + 1, true},
			{"at the ceiling", contentRelationshipLimit, false},
		} {
			tc := tc
			t.Run(tt.name+"/"+tc.label, func(t *testing.T) {
				t.Parallel()
				store := lookupCapContentStore{
					entity:     tt.entity,
					candidates: lookupCandidates(tc.candidates, tt.shape),
				}
				resp := contentFallbackRelationships(t, store, fmt.Sprintf(`{"entity_id":%q}`, tt.entity.EntityID))
				side, other := "incoming", "outgoing"
				if tt.outgoing {
					side, other = "outgoing", "incoming"
				}
				rows, _ := resp[side].([]any)
				if len(rows) != contentRelationshipLimit {
					t.Fatalf("len(%s) = %d, want %d (the ceiling)", side, len(rows), contentRelationshipLimit)
				}
				requireContentTruncationFlags(t, resp, tt.outgoing && tc.clipped, !tt.outgoing && tc.clipped)
				if resp[other+"_truncated"] != false {
					t.Fatalf("%s_truncated = %v, want false", other, resp[other+"_truncated"])
				}
				if _, leaked := resp["outgoing_truncated_type"]; leaked {
					t.Fatalf("internal scoping key leaked into the response: %#v", resp)
				}
				if _, leaked := resp["incoming_truncated_type"]; leaked {
					t.Fatalf("internal scoping key leaked into the response: %#v", resp)
				}

				if !tc.clipped {
					return
				}
				// The clip belongs to one edge type: a filter on another type
				// hides it, the matching type and a direction filter behave.
				for _, filter := range []struct {
					body            string
					wantOut, wantIn bool
				}{
					{fmt.Sprintf(`{"entity_id":%q,"relationship_type":%q}`, tt.entity.EntityID, tt.edgeType), tt.outgoing, !tt.outgoing},
					{fmt.Sprintf(`{"entity_id":%q,"relationship_type":"CALLS"}`, tt.entity.EntityID), false, false},
					{fmt.Sprintf(`{"entity_id":%q,"relationship_type":"SELECTS"}`, tt.entity.EntityID), false, false},
					{fmt.Sprintf(`{"entity_id":%q,"direction":%q}`, tt.entity.EntityID, other), false, false},
				} {
					filtered := contentFallbackRelationships(t, store, filter.body)
					requireContentTruncationFlags(t, filtered, filter.wantOut, filter.wantIn)
				}
			})
		}
	}
}

// TestContentRelationshipSetLookupClipKeepsScanTelemetryFlag pins the entity
// route's k8s telemetry: it reads scanTruncated, which must stay true only for
// the SELECTS candidate scan. A clipped name lookup is not that scan.
func TestContentRelationshipSetLookupClipKeepsScanTelemetryFlag(t *testing.T) {
	t.Parallel()

	implBlock := EntityContent{
		EntityID: "impl-1", RepoID: "repo-1", RelativePath: "src/a.rs",
		EntityType: "ImplBlock", EntityName: "Point",
	}
	store := lookupCapContentStore{
		entity: implBlock,
		candidates: lookupCandidates(contentRelationshipLimit+1, func(_ int, e *EntityContent) {
			e.EntityType = "Function"
			e.Metadata = map[string]any{"impl_context": "Point"}
		}),
	}
	set, err := buildContentRelationshipSet(context.Background(), store, implBlock, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v", err)
	}
	if !set.outgoingTruncated {
		t.Fatalf("outgoingTruncated = false, want true for a clipped rust lookup")
	}
	if set.scanTruncated {
		t.Fatalf("scanTruncated = true, want false: only the k8s SELECTS scan feeds the entity route's k8s telemetry")
	}
}
