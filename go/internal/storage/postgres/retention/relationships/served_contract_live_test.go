// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 served-contract proof: the real evidence drilldown handler over the real ContentReader read model answers 200 for the resolved_id of a retained generation and, after a sweep, the existing 404 (relationship evidence not found) for the resolved_id of the swept generation, whose resolved_relationships rows this slice leaves in place; the swept id answered 200 before the sweep, so the 404 is the sweep's doing and not a seeding mistake
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// getEvidence calls the real relationship evidence drilldown, the route a graph
// edge's resolved_id is followed through, on the real read model.
func (f *liveFixture) getEvidence(t *testing.T, resolvedID string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&query.EvidenceHandler{Content: query.NewContentReader(f.db)}).Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(f.ctx, http.MethodGet, "/api/v0/evidence/relationships/"+resolvedID, nil))
	return rec
}

// TestSweptResolvedIDAnswersTheExistingNotFoundLive pins the served contract the
// docs state: after the sweep removes a generation row, a resolved_id that
// belonged to it answers the existing 404, byte for byte what an id that never
// existed answers, with no new status. The resolved rows themselves stay (this
// slice never deletes one); the read model's inner join to
// relationship_generations is what turns them into a 404, so a change of that
// join to an outer join fails here.
func TestSweptResolvedIDAnswersTheExistingNotFoundLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "served-scope")
	f.scopeGeneration(t, "served-scope", "served-kept", "active")
	f.pointer(t, "served-scope", "served-kept")
	f.generation(t, "served-scope", "served-kept", "active")
	f.generation(t, "served-scope", "served-swept", "superseded")
	for _, generationID := range []string{"served-kept", "served-swept"} {
		f.evidence(t, generationID, 2)
		f.candidates(t, generationID, 1)
		f.resolved(t, generationID, 2)
	}

	for _, resolvedID := range []string{"served-kept-res-1", "served-swept-res-1", "served-swept-res-2"} {
		rec := f.getEvidence(t, resolvedID)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), resolvedID) {
			t.Fatalf("before the sweep GET %s = %d %s, want 200 with the row", resolvedID, rec.Code, rec.Body.String())
		}
	}

	if result := f.sweep(t); result.GenerationsSwept != 1 {
		t.Fatalf("swept %d generations, want only served-swept", result.GenerationsSwept)
	}

	if rec := f.getEvidence(t, "served-kept-res-1"); rec.Code != http.StatusOK {
		t.Fatalf("after the sweep the retained generation's resolved_id answered %d %s, want 200", rec.Code, rec.Body.String())
	}
	unknown := f.getEvidence(t, "served-never-existed")
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), "relationship evidence not found") {
		t.Fatalf("GET of an id that never existed = %d %s, want the 404 \"relationship evidence not found\"", unknown.Code, unknown.Body.String())
	}
	for _, resolvedID := range []string{"served-swept-res-1", "served-swept-res-2"} {
		rec := f.getEvidence(t, resolvedID)
		if rec.Code != unknown.Code || rec.Body.String() != unknown.Body.String() {
			t.Errorf("after the sweep GET %s = %d %s, want exactly what an id that never existed answers: %d %s",
				resolvedID, rec.Code, rec.Body.String(), unknown.Code, unknown.Body.String())
		}
	}
	if got, want := f.of(t, "served-swept"), (counts{0, 0, 0, 2}); got != want {
		t.Errorf("served-swept after the sweep = %+v, want %+v: the generation row goes and the resolved rows stay", got, want)
	}
}
