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

// wrapperFixtureStore nominates one thin wrapper family: five 12-token
// recordAPICall copies across packages. Twelve tokens sit below the floor
// on purpose, so the test proves the floor never applies to nomination.
type wrapperFixtureStore struct {
	content.FakePortContentStore
	membersByID map[string]codedivergence.Member
}

func (wrapperFixtureStore) DivergenceGroupStats(
	context.Context, string, codedivergence.Kind, int,
) ([]codedivergence.GroupStat, error) {
	return []codedivergence.GroupStat{
		{Fingerprint: "fp-wrap", Members: 5, Tokens: 12},
	}, nil
}

func (wrapperFixtureStore) DivergenceMembers(
	_ context.Context, _ string, _ codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	for _, fingerprint := range fingerprints {
		if fingerprint != "fp-wrap" {
			continue
		}
		for _, pkg := range []string{"a", "b", "c", "d", "e"} {
			suffix := pkg
			out[fingerprint] = append(out[fingerprint], codedivergence.Member{
				EntityID: "w-" + suffix, EntityName: "recordAPICall", EntityType: "Function",
				RelativePath: "svc-" + pkg + "/telemetry.go", Language: "go",
				StartLine: 10, EndLine: 16, TokenCount: 12,
			})
		}
	}
	return out, nil
}

func (s wrapperFixtureStore) DivergenceMembersByEntityID(
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

func (wrapperFixtureStore) DriftedFindingStats(
	context.Context, string,
) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (wrapperFixtureStore) DriftedFindingRows(
	context.Context, string, []string,
) (map[string]codedivergence.DriftedRow, error) {
	return map[string]codedivergence.DriftedRow{}, nil
}

// wrapperGraphFixture serves one target's canned graph evidence, filtered
// by the request's id params so unknown targets read empty.
type wrapperGraphFixture struct {
	callers map[string][]map[string]any
	fanIn   map[string]int
	callees map[string][]string
}

func (f wrapperGraphFixture) run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "UNWIND $source_ids AS sid"):
		rows := []map[string]any{}
		for _, id := range stringListParam(params, "source_ids") {
			for _, callee := range f.callees[id] {
				rows = append(rows, map[string]any{"source_id": id, "id": callee})
			}
		}
		return rows, nil
	case strings.Contains(cypher, "UNWIND $target_ids AS tid"):
		rows := []map[string]any{}
		for _, id := range stringListParam(params, "target_ids") {
			for _, row := range f.callers[id] {
				// The family callers read always projects its demux
				// columns; the fake injects them like the query does.
				shaped := map[string]any{"target_id": id, "target_name": "targetName-" + id}
				for key, value := range row {
					shaped[key] = value
				}
				rows = append(rows, shaped)
			}
		}
		return rows, nil
	case strings.Contains(cypher, "UNWIND $entity_ids AS eid"):
		rows := []map[string]any{}
		for _, id := range stringListParam(params, "entity_ids") {
			if fanIn, ok := f.fanIn[id]; ok {
				rows = append(rows, map[string]any{"id": id, "fan_in": fanIn})
			}
		}
		return rows, nil
	case strings.Contains(cypher, "$entity_id"):
		// Single-target callees read (investigate winner stats).
		rows := []map[string]any{}
		if id, _ := params["entity_id"].(string); id != "" {
			for _, callee := range f.callees[id] {
				rows = append(rows, map[string]any{"id": callee})
			}
		}
		return rows, nil
	default:
		return nil, nil
	}
}

func stringListParam(params map[string]any, key string) []string {
	ids, _ := params[key].([]string)
	return ids
}

func wrapperCallerRow(id, name, file string, complexity int, method string, confidence float64) map[string]any {
	return map[string]any{
		"id": id, "name": name, "file_path": file,
		"edge_method": method, "edge_confidence": confidence, "complexity": complexity,
	}
}

// positiveWrapperGraph is the issue's positive fixture: the wrapper w-a
// fronts target-t with fan-in 5 while x calls target-t directly from
// another package.
func positiveWrapperGraph(edgeMethod string, edgeConfidence float64) wrapperGraphFixture {
	return wrapperGraphFixture{
		callees: map[string][]string{
			"w-a": {"target-t"}, "w-b": {"target-t"}, "w-c": {"target-t"},
			"w-d": {"target-t"}, "w-e": {"target-t"},
		},
		callers: map[string][]map[string]any{
			"target-t": {
				wrapperCallerRow("w-a", "recordAPICall", "pkg/wrap/w.go", 2, edgeMethod, edgeConfidence),
				wrapperCallerRow("x-bypass", "callSite", "other/client.go", 30, string(codeprovenance.MethodDeclared), 0.85),
			},
		},
		fanIn: map[string]int{"w-a": 5, "x-bypass": 1},
	}
}

func positiveWrapperMembers() map[string]codedivergence.Member {
	return map[string]codedivergence.Member{
		"w-a": {
			EntityID: "w-a", EntityName: "recordAPICall", EntityType: "Function",
			RelativePath: "pkg/wrap/w.go", Language: "go", StartLine: 10, EndLine: 16, TokenCount: 12,
		},
		"x-bypass": {
			EntityID: "x-bypass", EntityName: "callSite", EntityType: "Function",
			RelativePath: "other/client.go", Language: "go", StartLine: 40, EndLine: 90, TokenCount: 200,
		},
	}
}

type wrapperFindingEnvelope struct {
	Data struct {
		Findings []struct {
			FindingID   string  `json:"finding_id"`
			Kind        string  `json:"kind"`
			Fingerprint string  `json:"fingerprint"`
			Score       int     `json:"score"`
			Confidence  float64 `json:"confidence"`
			Members     []struct {
				EntityID string `json:"entity_id"`
			} `json:"members"`
			Reasons []struct {
				Code  string `json:"code"`
				Value int    `json:"value"`
			} `json:"reasons"`
		} `json:"findings"`
		Suppressions  map[string]int `json:"suppressions"`
		SourceBackend string         `json:"source_backend"`
	} `json:"data"`
	Truth struct {
		Level string `json:"level"`
	} `json:"truth"`
}

func postWrapperFindings(t *testing.T, handler *CodeHandler, body string) (int, wrapperFindingEnvelope) {
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
	var envelope wrapperFindingEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	return rec.Code, envelope
}

func wrapperHandler(graph wrapperGraphFixture, members map[string]codedivergence.Member, backend GraphBackend) *CodeHandler {
	return &CodeHandler{
		Content:      wrapperFixtureStore{membersByID: members},
		Neo4j:        fakeGraphReader{run: graph.run},
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: backend,
	}
}

// TestCodeHandlerWrapperBypassFindingsPositive pins the issue's positive
// fixture end to end: stored nominating rows plus graph evidence assemble
// into the exact HTTP answer, on the NornicDB backend.
func TestCodeHandlerWrapperBypassFindingsPositive(t *testing.T) {
	t.Parallel()

	graph := positiveWrapperGraph(string(codeprovenance.MethodDeclared), 0.9)
	handler := wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNornicDB)
	code, envelope := postWrapperFindings(t, handler, `{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if got, want := len(envelope.Data.Findings), 1; got != want {
		t.Fatalf("findings = %d, want %d suppressions=%v", got, want, envelope.Data.Suppressions)
	}
	finding := envelope.Data.Findings[0]
	if finding.Kind != string(codedivergence.KindWrapperBypass) {
		t.Errorf("kind = %q, want wrapper_bypass", finding.Kind)
	}
	if finding.Fingerprint != "target-t" {
		t.Errorf("fingerprint = %q, want the target entity id", finding.Fingerprint)
	}
	if got, want := len(finding.Members), 2; got != want {
		t.Fatalf("members = %d, want 2 (wrapper plus bypasser)", got)
	}
	if finding.Members[0].EntityID != "w-a" {
		t.Errorf("first member = %q, want canonical wrapper first", finding.Members[0].EntityID)
	}
	if want := 2 * 200; finding.Score != want {
		t.Errorf("score = %d, want %d", finding.Score, want)
	}
	sum := 0
	canonical := false
	for _, reason := range finding.Reasons {
		sum += reason.Value
		if reason.Code == codedivergence.ReasonWrapperCanonical {
			canonical = true
		}
	}
	if sum != finding.Score {
		t.Errorf("reasons sum = %d, want score %d", sum, finding.Score)
	}
	if !canonical {
		t.Errorf("no wrapper_canonical_selected reason in %+v", finding.Reasons)
	}
	if finding.Confidence != 0.85 {
		t.Errorf("confidence = %v, want 0.85 (weakest contributing edge)", finding.Confidence)
	}
	if envelope.Data.SourceBackend != "postgres_content_store+graph" {
		t.Errorf("source_backend = %q, want mixed content+graph", envelope.Data.SourceBackend)
	}
	if envelope.Truth.Level != "derived" {
		t.Errorf("truth level = %q, want derived", envelope.Truth.Level)
	}
}

// TestCodeHandlerWrapperBypassFindingsParity pins the backend contract:
// NornicDB and Neo4j return the same finding for the same evidence.
func TestCodeHandlerWrapperBypassFindingsParity(t *testing.T) {
	t.Parallel()

	graph := positiveWrapperGraph(string(codeprovenance.MethodDeclared), 0.9)
	_, nornic := postWrapperFindings(t,
		wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNornicDB),
		`{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
	_, neo := postWrapperFindings(t,
		wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNeo4j),
		`{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
	if len(nornic.Data.Findings) != 1 || len(neo.Data.Findings) != 1 {
		t.Fatalf("nornic=%d neo4j=%d findings, want 1 each",
			len(nornic.Data.Findings), len(neo.Data.Findings))
	}
	a, b := nornic.Data.Findings[0], neo.Data.Findings[0]
	if a.FindingID != b.FindingID || a.Score != b.Score || a.Confidence != b.Confidence ||
		len(a.Members) != len(b.Members) || a.Members[0].EntityID != b.Members[0].EntityID {
		t.Errorf("backend findings differ:\n%+v\n%+v", a, b)
	}
}

// TestCodeHandlerWrapperBypassFindingsNegative pins the issue's negative
// fixtures: same-package direct calls and peer wrappers suppress with
// counted reasons and no finding.
func TestCodeHandlerWrapperBypassFindingsNegative(t *testing.T) {
	t.Parallel()

	cases := map[string]wrapperGraphFixture{
		"same package direct": {
			callees: map[string][]string{"w-a": {"target-t"}, "w-b": {"target-t"}},
			callers: map[string][]map[string]any{
				"target-t": {
					wrapperCallerRow("w-a", "recordAPICall", "pkg/wrap/w.go", 2, string(codeprovenance.MethodDeclared), 0.9),
					wrapperCallerRow("w-a2", "recordAPICall", "pkg/wrap/other.go", 2, string(codeprovenance.MethodDeclared), 0.88),
				},
			},
			fanIn: map[string]int{"w-a": 5, "w-a2": 1},
		},
		"peer wrappers": {
			callees: map[string][]string{"w-a": {"target-t"}, "w-f": {"target-t"}},
			callers: map[string][]map[string]any{
				"target-t": {
					wrapperCallerRow("w-a", "recordAPICall", "pkg/wrap/w.go", 2, string(codeprovenance.MethodDeclared), 0.9),
					wrapperCallerRow("w-f", "recordAPICall", "pkg/other/w.go", 2, string(codeprovenance.MethodDeclared), 0.9),
				},
			},
			fanIn: map[string]int{"w-a": 5, "w-f": 5},
		},
	}
	for name, graph := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler := wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNornicDB)
			code, envelope := postWrapperFindings(t, handler, `{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want %d", code, http.StatusOK)
			}
			if len(envelope.Data.Findings) != 0 {
				t.Errorf("findings = %d, want 0", len(envelope.Data.Findings))
			}
			if envelope.Data.Suppressions[codedivergence.RuleWrapperUnqualified] == 0 {
				t.Errorf("missing wrapper_not_qualified count in %v", envelope.Data.Suppressions)
			}
		})
	}
}

// TestCodeHandlerWrapperBypassFindingsAmbiguous pins the issue's ambiguous
// fixture: a wrapper reached only through an inferred edge still reports,
// flagged by the zero-weight ambiguity signal.
func TestCodeHandlerWrapperBypassFindingsAmbiguous(t *testing.T) {
	t.Parallel()

	graph := positiveWrapperGraph(string(codeprovenance.MethodTypeInferred), 0.7)
	handler := wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNornicDB)
	code, envelope := postWrapperFindings(t, handler, `{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if got, want := len(envelope.Data.Findings), 1; got != want {
		t.Fatalf("findings = %d, want %d suppressions=%v", got, want, envelope.Data.Suppressions)
	}
	finding := envelope.Data.Findings[0]
	ambiguous := false
	for _, reason := range finding.Reasons {
		if reason.Code == codedivergence.ReasonCanonicalAmbiguous {
			ambiguous = true
			if reason.Value != 0 {
				t.Errorf("ambiguous reason value = %d, want 0", reason.Value)
			}
		}
	}
	if !ambiguous {
		t.Errorf("no canonical_ambiguous reason in %+v", finding.Reasons)
	}
	if finding.Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7 (weakest edge, inferred)", finding.Confidence)
	}
}

// TestCodeHandlerWrapperBypassFindingsGraphDown pins degradation: with no
// graph reader the wrapper track counts unavailability instead of failing
// the page.
func TestCodeHandlerWrapperBypassFindingsGraphDown(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: wrapperFixtureStore{membersByID: positiveWrapperMembers()},
		Profile: ProfileLocalAuthoritative,
	}
	code, envelope := postWrapperFindings(t, handler, `{"repo_id":"repo-x","kind":"wrapper_bypass"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if len(envelope.Data.Findings) != 0 {
		t.Errorf("findings = %d, want 0 (graph down)", len(envelope.Data.Findings))
	}
	if envelope.Data.Suppressions[codedivergence.RuleWrapperGraphUnavailable] == 0 {
		t.Errorf("missing wrapper_graph_unavailable count in %v", envelope.Data.Suppressions)
	}
}

// TestCodeHandlerWrapperBypassInvestigate pins the point lookup: the
// target fingerprint resolves to the same finding shape, and an
// unqualified target 404s.
func TestCodeHandlerWrapperBypassInvestigate(t *testing.T) {
	t.Parallel()

	graph := positiveWrapperGraph(string(codeprovenance.MethodDeclared), 0.9)
	handler := wrapperHandler(graph, positiveWrapperMembers(), GraphBackendNornicDB)
	mux := http.NewServeMux()
	handler.Mount(mux)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v0/code/divergence/investigate",
			bytes.NewBufferString(body),
		)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := post(`{"repo_id":"repo-x","kind":"wrapper_bypass","fingerprint":"target-t"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Finding struct {
				Kind       string  `json:"kind"`
				Score      int     `json:"score"`
				Confidence float64 `json:"confidence"`
			} `json:"finding"`
			NextSteps []any `json:"next_steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Data.Finding.Kind != string(codedivergence.KindWrapperBypass) {
		t.Errorf("kind = %q, want wrapper_bypass", envelope.Data.Finding.Kind)
	}
	if envelope.Data.Finding.Score != 2*200 {
		t.Errorf("score = %d, want %d", envelope.Data.Finding.Score, 2*200)
	}
	if envelope.Data.Finding.Confidence != 0.85 {
		t.Errorf("confidence = %v, want 0.85", envelope.Data.Finding.Confidence)
	}
	if got, want := len(envelope.Data.NextSteps), 4; got != want {
		t.Errorf("next_steps = %d, want %d (two calls per member)", got, want)
	}
	rec = post(`{"repo_id":"repo-x","kind":"wrapper_bypass","fingerprint":"no-such-target"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown target status = %d, want 404 body=%s", rec.Code, rec.Body.String())
	}
}

// TestWinnerStatsForFallsBackToOnDemandRead pins the P2 fix: when the
// fan-in winner is a direct caller but not a nominated wrapper, its ids
// are absent from batched evidence, so winnerStatsFor fetches them with
// the same single-target read the investigate path uses instead of
// reporting empty thinness inputs.
func TestWinnerStatsForFallsBackToOnDemandRead(t *testing.T) {
	t.Parallel()

	graph := wrapperGraphFixture{
		callees: map[string][]string{"outsider": {"target-t", "other-fn"}},
	}
	handler := wrapperHandler(graph, map[string]codedivergence.Member{}, GraphBackendNornicDB)
	evidence := &wrapperGraphEvidence{callees: map[string][]string{"w-a": {"target-t"}}}
	stats, err := handler.winnerStatsFor(context.Background(), "repo-x", "target-t", evidence)(
		WrapperCallerRow{EntityID: "outsider", Complexity: 7})
	if err != nil {
		t.Fatalf("winnerStatsFor = %v, want on-demand stats", err)
	}
	if stats.CalleeCount != 2 || stats.TargetCalls != 1 || stats.Complexity != 7 {
		t.Errorf("stats = %+v, want {CalleeCount:2 TargetCalls:1 Complexity:7}", stats)
	}
}
