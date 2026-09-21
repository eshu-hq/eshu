// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
)

// multiMountOutlierGraph spreads the positive shape over three router
// mounts: each mount has five handlers, four calling the guard, so the
// sweep qualifies three findings with distinct cohort keys.
func multiMountOutlierGraph() (outlierGraphFixture, map[string]codedivergence.Member) {
	seeds := map[codedivergence.CohortSource][]map[string]any{}
	edges := map[string][]map[string]any{}
	members := map[string]codedivergence.Member{}
	for m := 1; m <= 3; m++ {
		mount := fmt.Sprintf("/m%d", m)
		for i := 1; i <= 5; i++ {
			id := fmt.Sprintf("m%d-h%d", m, i)
			seeds[codedivergence.CohortRouter] = append(seeds[codedivergence.CohortRouter], map[string]any{
				"endpoint_path": mount, "member_id": id, "member_name": "handle",
			})
			members[id] = codedivergence.Member{
				EntityID: id, EntityName: "handle", EntityType: "Function",
				RelativePath: fmt.Sprintf("api/m%d.go", m), Language: "go",
				StartLine: 10 + i*20, EndLine: 25 + i*20, TokenCount: 120,
			}
			if i <= 4 {
				edges[id] = []map[string]any{{
					"member_id": id, "callee_id": "guard", "callee_name": "requireAuth",
					"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
				}}
			}
		}
	}
	return outlierGraphFixture{seeds: seeds, edges: edges}, members
}

// TestCodeHandlerOutlierFindingsPageExactlyOnce is the paging regression:
// three qualified findings with limit=2 must partition into stable,
// disjoint pages (2 + 1) that terminate, never an unbounded page and never
// a re-emitted set.
func TestCodeHandlerOutlierFindingsPageExactlyOnce(t *testing.T) {
	t.Parallel()

	graph, members := multiMountOutlierGraph()
	handler := outlierHandler(graph, members)
	seen := map[string]int{}
	offset := 0
	pages := 0
	for {
		body := fmt.Sprintf(`{"repo_id":"repo-x","kind":"convention_outlier","limit":2,"offset":%d}`, offset)
		code, envelope := postOutlierFindings(t, handler, body)
		if code != http.StatusOK {
			t.Fatalf("status = %d, want %d", code, http.StatusOK)
		}
		pages++
		if pages > 5 {
			t.Fatalf("paging did not terminate after %d pages", pages)
		}
		for _, finding := range envelope.Data.Findings {
			if finding.Kind != string(codedivergence.KindConventionOutlier) {
				t.Errorf("kind = %q, want convention_outlier", finding.Kind)
			}
			seen[finding.Fingerprint]++
		}
		if !envelope.Data.Truncated {
			if envelope.Data.NextOffset != nil {
				t.Errorf("next_offset = %d on final page, want null", *envelope.Data.NextOffset)
			}
			break
		}
		if envelope.Data.NextOffset == nil {
			t.Fatalf("truncated page %d carries no next_offset", pages)
		}
		offset = *envelope.Data.NextOffset
	}
	if pages != 2 {
		t.Errorf("pages = %d, want 2 for 3 findings at limit 2", pages)
	}
	if len(seen) != 3 {
		t.Errorf("distinct findings = %d, want 3 (saw %v)", len(seen), seen)
	}
	for fingerprint, count := range seen {
		if count != 1 {
			t.Errorf("fingerprint %q emitted %d times, want exactly once", fingerprint, count)
		}
	}
}

// mixedOutlierStore layers exact-kind group stats over the outlier
// fixture store: the stat stream pages while the outlier sweep waits for
// the tail.
type mixedOutlierStore struct {
	outlierFixtureStore
}

func (mixedOutlierStore) DivergenceGroupStats(
	_ context.Context, _ string, kind codedivergence.Kind, _ int,
) ([]codedivergence.GroupStat, error) {
	if kind != codedivergence.KindExact {
		return nil, nil
	}
	return []codedivergence.GroupStat{
		{Fingerprint: "fp-x1", Members: 2, Tokens: 210},
		{Fingerprint: "fp-x2", Members: 2, Tokens: 210},
		{Fingerprint: "fp-x3", Members: 2, Tokens: 210},
	}, nil
}

func (mixedOutlierStore) DivergenceMembers(
	_ context.Context, _ string, kind codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	if kind != codedivergence.KindExact {
		return out, nil
	}
	for _, fingerprint := range fingerprints {
		// Distinct entity names per member: a shared name would read as a
		// wrapper family and divert the group to the wrapper track.
		out[fingerprint] = []codedivergence.Member{
			{EntityID: fingerprint + "-a", EntityName: "renderAlpha", EntityType: "Function", RelativePath: "a/" + fingerprint + ".go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
			{EntityID: fingerprint + "-b", EntityName: "renderBeta", EntityType: "Function", RelativePath: "b/" + fingerprint + ".go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
		}
	}
	return out, nil
}

// TestCodeHandlerOutlierFindingsDeferToStatStream pins the mixed-read
// contract: while the stat stream still truncates, the page carries stat
// findings and no outliers; the outlier emits exactly once on the tail
// page after the stats are exhausted.
func TestCodeHandlerOutlierFindingsDeferToStatStream(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content:      mixedOutlierStore{outlierFixtureStore{membersByID: positiveOutlierMembers()}},
		Neo4j:        fakeGraphReader{run: positiveOutlierGraph().run},
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
	}
	code, first := postOutlierFindings(t, handler, `{"repo_id":"repo-x","kind":"","limit":2,"offset":0}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	for _, finding := range first.Data.Findings {
		if finding.Kind == string(codedivergence.KindConventionOutlier) {
			t.Errorf("page 0 carries an outlier while the stat stream truncates: %+v", finding.Cohort)
		}
	}
	if !first.Data.Truncated || first.Data.NextOffset == nil {
		t.Fatalf("page 0 truncated=%v next_offset=%v, want truncated with cursor",
			first.Data.Truncated, first.Data.NextOffset)
	}
	code, second := postOutlierFindings(t, handler,
		fmt.Sprintf(`{"repo_id":"repo-x","kind":"","limit":2,"offset":%d}`, *first.Data.NextOffset))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	outliers := 0
	for _, finding := range second.Data.Findings {
		if finding.Kind == string(codedivergence.KindConventionOutlier) {
			outliers++
		}
	}
	if outliers != 1 {
		t.Errorf("tail page outliers = %d, want exactly 1", outliers)
	}
	if second.Data.Truncated {
		t.Errorf("tail page truncated = true, want false with the track drained")
	}
}
