// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestSelectServiceChangedSinceLineageCoversEveryBranch drives the pure lineage
// choice (#6475 part B) over every branch. "Active" means the lineage has a
// current active generation; a lineage without one has nothing to diff
// against, so it neither makes an active lineage ambiguous nor shadows an
// active legacy one.
func TestSelectServiceChangedSinceLineageCoversEveryBranch(t *testing.T) {
	t.Parallel()

	attributed := func(scopeID, current string) serviceChangedSinceLineage {
		return serviceChangedSinceLineage{scopeID: scopeID, currentGenerationID: current}
	}
	legacy := func(current string) serviceChangedSinceLineage {
		return serviceChangedSinceLineage{unattributed: true, currentGenerationID: current}
	}
	unscoped := statuspkg.ServiceChangedSinceFilter{ServiceID: "svc"}
	scoped := statuspkg.ServiceChangedSinceFilter{ServiceID: "svc", Scoped: true, AllowedScopeIDs: []string{"scope-a", "scope-b"}}
	selected := statuspkg.ServiceChangedSinceFilter{ServiceID: "svc", ScopeID: "scope-a"}

	for _, tc := range []struct {
		name          string
		filter        statuspkg.ServiceChangedSinceFilter
		lineages      []serviceChangedSinceLineage
		wantScope     string // served lineage's scope id; "" with wantLegacy for legacy
		wantLegacy    bool
		wantCurrent   string
		wantAmbiguous []string
		wantNone      bool
	}{
		{
			name: "one attributed active is served", filter: unscoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", "gen-a")},
			wantScope: "scope-a", wantCurrent: "gen-a",
		},
		{
			name: "two attributed active is ambiguous", filter: unscoped,
			lineages:      []serviceChangedSinceLineage{attributed("scope-b", "gen-b"), attributed("scope-a", "gen-a")},
			wantAmbiguous: []string{"scope-a", "scope-b"},
		},
		{
			name: "two attributed with one active serves the active one", filter: unscoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", ""), attributed("scope-b", "gen-b")},
			wantScope: "scope-b", wantCurrent: "gen-b",
		},
		{
			name: "one attributed none active and no legacy is served unavailable", filter: unscoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", "")},
			wantScope: "scope-a", wantCurrent: "",
		},
		{
			name: "several attributed none active is ambiguous", filter: unscoped,
			lineages:      []serviceChangedSinceLineage{attributed("scope-b", ""), attributed("scope-a", "")},
			wantAmbiguous: []string{"scope-a", "scope-b"},
		},
		{
			name: "legacy with no attributed is served", filter: unscoped,
			lineages:   []serviceChangedSinceLineage{legacy("gen-legacy")},
			wantLegacy: true, wantCurrent: "gen-legacy",
		},
		{
			name: "active attributed beats active legacy", filter: unscoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", "gen-a"), legacy("gen-legacy")},
			wantScope: "scope-a", wantCurrent: "gen-a",
		},
		{
			name: "active legacy beats inactive attributed", filter: unscoped,
			lineages:   []serviceChangedSinceLineage{attributed("scope-a", ""), legacy("gen-legacy")},
			wantLegacy: true, wantCurrent: "gen-legacy",
		},
		{
			name: "inactive legacy loses to inactive attributed", filter: unscoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", ""), legacy("")},
			wantScope: "scope-a", wantCurrent: "",
		},
		{
			name: "inactive legacy is served only when nothing else exists", filter: unscoped,
			lineages:   []serviceChangedSinceLineage{legacy("")},
			wantLegacy: true, wantCurrent: "",
		},
		{
			name: "scoped caller is never served legacy even beside inactive attributed", filter: scoped,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", ""), legacy("gen-legacy")},
			wantScope: "scope-a", wantCurrent: "",
		},
		{
			name: "scoped caller is never served a lone legacy lineage", filter: scoped,
			lineages: []serviceChangedSinceLineage{legacy("gen-legacy")},
			wantNone: true,
		},
		{
			name: "explicit selector is never served legacy even beside inactive attributed", filter: selected,
			lineages:  []serviceChangedSinceLineage{attributed("scope-a", ""), legacy("gen-legacy")},
			wantScope: "scope-a", wantCurrent: "",
		},
		{
			name: "explicit selector is never served a lone legacy lineage", filter: selected,
			lineages: []serviceChangedSinceLineage{legacy("gen-legacy")},
			wantNone: true,
		},
		{
			name: "nothing admitted is not found", filter: unscoped,
			wantNone: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ambiguous, ok := selectServiceChangedSinceLineage(tc.filter, tc.lineages)
			switch {
			case tc.wantAmbiguous != nil:
				if ok || !reflect.DeepEqual(ambiguous, tc.wantAmbiguous) {
					t.Fatalf("got lineage %+v ok=%t ambiguous=%v; want ambiguous %v", got, ok, ambiguous, tc.wantAmbiguous)
				}
			case tc.wantNone:
				if ok || ambiguous != nil {
					t.Fatalf("got lineage %+v ok=%t ambiguous=%v; want nothing", got, ok, ambiguous)
				}
			default:
				if !ok || ambiguous != nil || got.unattributed != tc.wantLegacy ||
					got.scopeID != tc.wantScope || got.currentGenerationID != tc.wantCurrent {
					t.Fatalf("got lineage %+v ok=%t ambiguous=%v; want scope %q legacy=%t current %q",
						got, ok, ambiguous, tc.wantScope, tc.wantLegacy, tc.wantCurrent)
				}
			}
		})
	}
}
