// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// outlierFixtureStore resolves finding members by entity id; group stats
// stay empty because the outlier track sweeps the graph, never the content
// store.
type outlierFixtureStore struct {
	content.FakePortContentStore
	membersByID map[string]codedivergence.Member
}

func (outlierFixtureStore) DivergenceGroupStats(
	context.Context, string, codedivergence.Kind, int,
) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (outlierFixtureStore) DivergenceMembers(
	context.Context, string, codedivergence.Kind, []string,
) (map[string][]codedivergence.Member, error) {
	return map[string][]codedivergence.Member{}, nil
}

func (s outlierFixtureStore) DivergenceMembersByEntityID(
	_ context.Context, _ string, entityIDs []string,
) (map[string]codedivergence.Member, error) {
	out := map[string]codedivergence.Member{}
	for _, id := range entityIDs {
		if member, ok := s.membersByID[id]; ok {
			out[id] = member
		}
	}
	return out, nil
}

func (outlierFixtureStore) DriftedFindingStats(
	context.Context, string,
) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (outlierFixtureStore) DriftedFindingRows(
	context.Context, string, []string,
) (map[string]codedivergence.DriftedRow, error) {
	return map[string]codedivergence.DriftedRow{}, nil
}

// outlierGraphFixture serves canned cohort rows per enumeration shape plus
// outgoing CALLS edges per member, filtered by the request's id params so
// unknown cohorts read empty.
type outlierGraphFixture struct {
	seeds map[codedivergence.CohortSource][]map[string]any
	edges map[string][]map[string]any
}

func (f outlierGraphFixture) run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "IMPLEMENTS"):
		return f.seeds[codedivergence.CohortInterface], nil
	case strings.Contains(cypher, "HANDLES_ROUTE"):
		return f.seeds[codedivergence.CohortRouter], nil
	case strings.Contains(cypher, "containerFile"):
		return f.seeds[codedivergence.CohortPackage], nil
	case strings.Contains(cypher, "UNWIND $member_ids AS mid"):
		rows := []map[string]any{}
		for _, id := range stringListParam(params, "member_ids") {
			rows = append(rows, f.edges[id]...)
		}
		return rows, nil
	default:
		return nil, nil
	}
}

// positiveOutlierGraph is the issue's positive fixture: five handlers on
// the /widgets mount, four calling requireAuth, served as router seeds plus
// outgoing edges.
func positiveOutlierGraph() outlierGraphFixture {
	seeds := map[codedivergence.CohortSource][]map[string]any{}
	rows := []map[string]any{}
	for _, id := range []string{"h-1", "h-2", "h-3", "h-4", "h-5"} {
		rows = append(rows, map[string]any{
			"endpoint_path": "/widgets", "member_id": id, "member_name": "handle",
		})
	}
	seeds[codedivergence.CohortRouter] = rows
	edges := map[string][]map[string]any{}
	for _, id := range []string{"h-1", "h-2", "h-3", "h-4"} {
		edges[id] = []map[string]any{{
			"member_id": id, "callee_id": "guard", "callee_name": "requireAuth",
			"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
		}}
	}
	return outlierGraphFixture{seeds: seeds, edges: edges}
}

func positiveOutlierMembers() map[string]codedivergence.Member {
	out := map[string]codedivergence.Member{}
	for i, id := range []string{"h-1", "h-2", "h-3", "h-4", "h-5"} {
		out[id] = codedivergence.Member{
			EntityID: id, EntityName: "handle", EntityType: "Function",
			RelativePath: "api/widgets.go", Language: "go",
			StartLine: 10 + i*20, EndLine: 25 + i*20, TokenCount: 120,
		}
	}
	return out
}

type outlierFindingEnvelope struct {
	Data struct {
		Findings []struct {
			FindingID   string  `json:"finding_id"`
			Kind        string  `json:"kind"`
			Fingerprint string  `json:"fingerprint"`
			Score       int     `json:"score"`
			Confidence  float64 `json:"confidence"`
			Share       float64 `json:"share"`
			Inferred    bool    `json:"inferred"`
			Cohort      struct {
				Source    string `json:"source"`
				Key       string `json:"key"`
				Truncated bool   `json:"truncated"`
			} `json:"cohort"`
			MajorityCallee struct {
				EntityID string `json:"entity_id"`
			} `json:"majority_callee"`
			Outliers []string `json:"outliers"`
			Members  []struct {
				EntityID string `json:"entity_id"`
			} `json:"members"`
			Reasons []struct {
				Code  string `json:"code"`
				Value int    `json:"value"`
			} `json:"reasons"`
		} `json:"findings"`
		Suppressions  map[string]int `json:"suppressions"`
		SourceBackend string         `json:"source_backend"`
		Truncated     bool           `json:"truncated"`
		NextOffset    *int           `json:"next_offset"`
	} `json:"data"`
	Truth struct {
		Level string `json:"level"`
	} `json:"truth"`
}

func postOutlierFindings(t *testing.T, handler *CodeHandler, body string) (int, outlierFindingEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var envelope outlierFindingEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	return rec.Code, envelope
}

func outlierHandler(graph outlierGraphFixture, members map[string]codedivergence.Member) *CodeHandler {
	return &CodeHandler{
		Content:      outlierFixtureStore{membersByID: members},
		Neo4j:        fakeGraphReader{run: graph.run},
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
	}
}

// TestCodeHandlerOutlierFindingsPositive pins the issue's positive fixture
// end to end: five router handlers, four calling the guard, assemble into
// the exact HTTP answer naming the cohort, callee, share, and outlier.
func TestCodeHandlerOutlierFindingsPositive(t *testing.T) {
	t.Parallel()

	handler := outlierHandler(positiveOutlierGraph(), positiveOutlierMembers())
	code, envelope := postOutlierFindings(t, handler, `{"repo_id":"repo-x","kind":"convention_outlier"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if got, want := len(envelope.Data.Findings), 1; got != want {
		t.Fatalf("findings = %d, want %d suppressions=%v", got, want, envelope.Data.Suppressions)
	}
	finding := envelope.Data.Findings[0]
	if finding.Kind != string(codedivergence.KindConventionOutlier) {
		t.Errorf("kind = %q, want convention_outlier", finding.Kind)
	}
	if finding.Cohort.Source != "router" || finding.Cohort.Key != "widgets" {
		t.Errorf("cohort = %+v, want router/widgets", finding.Cohort)
	}
	if finding.MajorityCallee.EntityID != "guard" {
		t.Errorf("majority callee = %q, want guard", finding.MajorityCallee.EntityID)
	}
	if finding.Share != 0.8 {
		t.Errorf("share = %v, want 0.8", finding.Share)
	}
	if len(finding.Outliers) != 1 || finding.Outliers[0] != "h-5" {
		t.Errorf("outliers = %v, want [h-5]", finding.Outliers)
	}
	if len(finding.Members) != 5 || finding.Members[0].EntityID != "h-5" {
		t.Errorf("members lead with the outlier, got %+v", finding.Members)
	}
	if want := 5 * 120; finding.Score != want {
		t.Errorf("score = %d, want %d", finding.Score, want)
	}
	sum := 0
	majority := false
	for _, reason := range finding.Reasons {
		sum += reason.Value
		if reason.Code == codedivergence.ReasonOutlierMajority {
			majority = true
		}
	}
	if sum != finding.Score {
		t.Errorf("reasons sum = %d, want score %d", sum, finding.Score)
	}
	if !majority {
		t.Errorf("no outlier_majority_calls reason in %+v", finding.Reasons)
	}
	if finding.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", finding.Confidence)
	}
	if finding.Inferred {
		t.Errorf("inferred = true, want false on declared edges")
	}
	if envelope.Data.SourceBackend != "postgres_content_store+graph" {
		t.Errorf("source_backend = %q, want mixed content+graph", envelope.Data.SourceBackend)
	}
	if envelope.Truth.Level != "derived" {
		t.Errorf("truth level = %q, want derived", envelope.Truth.Level)
	}
}

// TestCodeHandlerOutlierFindingsNegatives pins both negative fixtures: a
// two-member cohort stays below the minimum size, and a helper called by
// two of five stays below the share floor. Both report quietly with counted
// suppressions where the catalogue applies.
func TestCodeHandlerOutlierFindingsNegatives(t *testing.T) {
	t.Parallel()

	seeds := map[codedivergence.CohortSource][]map[string]any{
		codedivergence.CohortPackage: {
			{"file_path": "pkg/a.go", "member_id": "p-1", "member_name": "one"},
			{"file_path": "pkg/a.go", "member_id": "p-2", "member_name": "two"},
			{"file_path": "pkg/a.go", "member_id": "p-3", "member_name": "three"},
			{"file_path": "pkg/a.go", "member_id": "p-4", "member_name": "four"},
			{"file_path": "pkg/a.go", "member_id": "p-5", "member_name": "five"},
		},
	}
	edges := map[string][]map[string]any{
		"p-1": {{
			"member_id": "p-1", "callee_id": "helper", "callee_name": "format",
			"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
		}},
		"p-2": {{
			"member_id": "p-2", "callee_id": "helper", "callee_name": "format",
			"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
		}},
	}
	graph := outlierGraphFixture{seeds: seeds, edges: edges}
	members := map[string]codedivergence.Member{}
	for i, id := range []string{"p-1", "p-2", "p-3", "p-4", "p-5"} {
		members[id] = codedivergence.Member{
			EntityID: id, EntityName: "fn", EntityType: "Function",
			RelativePath: "pkg/a.go", Language: "go",
			StartLine: 10 + i*10, EndLine: 15 + i*10, TokenCount: 120,
		}
	}
	handler := outlierHandler(graph, members)
	code, envelope := postOutlierFindings(t, handler, `{"repo_id":"repo-x","kind":"convention_outlier"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if len(envelope.Data.Findings) != 0 {
		t.Errorf("findings = %d, want 0 for below-share helper", len(envelope.Data.Findings))
	}
}

// TestCodeHandlerOutlierFindingsAmbiguous pins the ambiguous fixture: the
// outlier reaches the guard through a wrapper, so the finding still
// assembles but carries the wrapper-mediated signal.
func TestCodeHandlerOutlierFindingsAmbiguous(t *testing.T) {
	t.Parallel()

	graph := positiveOutlierGraph()
	graph.edges["h-5"] = []map[string]any{{
		"member_id": "h-5", "callee_id": "wrap", "callee_name": "checkedHandler",
		"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
	}}
	graph.edges["wrap"] = []map[string]any{{
		"member_id": "wrap", "callee_id": "guard", "callee_name": "requireAuth",
		"edge_method": string(codeprovenance.MethodDeclared), "edge_confidence": 0.9,
	}}
	members := positiveOutlierMembers()
	handler := outlierHandler(graph, members)
	code, envelope := postOutlierFindings(t, handler, `{"repo_id":"repo-x","kind":"convention_outlier"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if got, want := len(envelope.Data.Findings), 1; got != want {
		t.Fatalf("findings = %d, want %d", got, want)
	}
	mediated := false
	for _, reason := range envelope.Data.Findings[0].Reasons {
		if reason.Code == codedivergence.ReasonOutlierMediated {
			mediated = true
			if reason.Value != 0 {
				t.Errorf("mediated reason value = %d, want 0", reason.Value)
			}
		}
	}
	if !mediated {
		t.Errorf("no wrapper-mediated reason in %+v", envelope.Data.Findings[0].Reasons)
	}
}

// TestCodeHandlerOutlierInvestigateRoundTrip pins the point lookup: the
// fingerprint from a findings entry re-derives the same finding, and a
// foreign fingerprint 404s.
func TestCodeHandlerOutlierInvestigateRoundTrip(t *testing.T) {
	t.Parallel()

	handler := outlierHandler(positiveOutlierGraph(), positiveOutlierMembers())
	_, envelope := postOutlierFindings(t, handler, `{"repo_id":"repo-x","kind":"convention_outlier"}`)
	if len(envelope.Data.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(envelope.Data.Findings))
	}
	fingerprint := envelope.Data.Findings[0].Fingerprint
	mux := http.NewServeMux()
	handler.Mount(mux)
	post := func(fingerprint string) int {
		t.Helper()
		// Marshal the body: fingerprints carry NUL separators, which are
		// valid JSON only escaped (like any client library emits).
		body, err := json.Marshal(map[string]any{
			"repo_id": "repo-x", "kind": "convention_outlier", "fingerprint": fingerprint,
		})
		if err != nil {
			t.Fatalf("marshal investigate body: %v", err)
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v0/code/divergence/investigate",
			bytes.NewBuffer(body),
		)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(fingerprint); code != http.StatusOK {
		t.Errorf("investigate status = %d, want %d", code, http.StatusOK)
	}
	if code := post("fp-wrap"); code != http.StatusNotFound {
		t.Errorf("foreign fingerprint status = %d, want 404", code)
	}
}
