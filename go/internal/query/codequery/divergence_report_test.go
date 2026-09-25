// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Divergence-report handler proofs. These stay in codequery: they exercise
// the CodeHandler and DivergenceReportRequest through the shared fixture
// fake store (one exact group plus one drifted pair), with no root-owned
// ContentReader SQL.

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// reportEnvelope decodes the report answer packet: counts by kind, the
// total, the top findings per kind, per-kind truncation, and suppressions.
type reportEnvelope struct {
	Data struct {
		RepoID string         `json:"repo_id"`
		Counts map[string]int `json:"counts"`
		Total  int            `json:"total"`
		Top    map[string][]struct {
			FindingID string `json:"finding_id"`
			Score     int    `json:"score"`
			Kind      string `json:"kind"`
		} `json:"top"`
		TopPerKind   int             `json:"top_per_kind"`
		Truncated    map[string]bool `json:"truncated"`
		Suppressions map[string]int  `json:"suppressions"`
	} `json:"data"`
	Truth struct {
		Level string `json:"level"`
	} `json:"truth"`
}

func serveDivergenceReport(t *testing.T, body string) (int, reportEnvelope) {
	t.Helper()

	handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/report",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var envelope reportEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	return w.Code, envelope
}

// TestCodeHandlerDivergenceReportRollsUpAllKinds pins the rollup contract:
// one call returns counts by kind plus the top findings per kind. The
// fixture fake serves the same exact group under the exact and renamed
// families plus one drifted pair: three content findings. The two-member
// fixture family is below the wrapper-family floor, so wrapper_bypass
// counts zero with a not_wrapper_family suppression; the outlier track has
// no graph reader, so convention_outlier counts zero with an
// outlier_unavailable suppression instead of failing the report.
func TestCodeHandlerDivergenceReportRollsUpAllKinds(t *testing.T) {
	t.Parallel()

	code, envelope := serveDivergenceReport(t, `{"repo_id":"repo-x"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	wantCounts := map[string]int{
		"parallel_implementation.exact":              1,
		"parallel_implementation.renamed":            1,
		"parallel_implementation.drifted":            1,
		"parallel_implementation.wrapper_bypass":     0,
		"parallel_implementation.convention_outlier": 0,
	}
	for kind, want := range wantCounts {
		if got := envelope.Data.Counts[kind]; got != want {
			t.Fatalf("counts[%s] = %d, want %d", kind, got, want)
		}
	}
	if envelope.Data.Total != 3 {
		t.Fatalf("total = %d, want 3", envelope.Data.Total)
	}
	if envelope.Data.TopPerKind != DivergenceReportDefaultTop {
		t.Fatalf("top_per_kind = %d, want default %d", envelope.Data.TopPerKind, DivergenceReportDefaultTop)
	}
	exactTop := envelope.Data.Top["parallel_implementation.exact"]
	if len(exactTop) != 1 {
		t.Fatalf("top exact findings = %d, want 1", len(exactTop))
	}
	if exactTop[0].FindingID == "" {
		t.Fatal("top finding_id must be set")
	}
	if want := 2 * 210; exactTop[0].Score != want {
		t.Fatalf("top exact score = %d, want %d", exactTop[0].Score, want)
	}
	if got := envelope.Data.Top["parallel_implementation.drifted"]; len(got) != 1 {
		t.Fatalf("top drifted findings = %d, want 1", len(got))
	}
	if got := envelope.Data.Top["parallel_implementation.wrapper_bypass"]; len(got) != 0 {
		t.Fatalf("top wrapper_bypass findings = %d, want 0", len(got))
	}
	for kind, truncated := range envelope.Data.Truncated {
		if truncated {
			t.Fatalf("truncated[%s] must be false on the fixture repo", kind)
		}
	}
	if got := envelope.Data.Suppressions["generated_file"]; got < 1 {
		t.Fatalf("generated_file suppressions = %d, want >= 1", got)
	}
	if got := envelope.Data.Suppressions["outlier_graph_unavailable"]; got != 1 {
		t.Fatalf("outlier_graph_unavailable suppressions = %d, want 1", got)
	}
	if envelope.Truth.Level != "derived" {
		t.Fatalf("truth.level = %q, want derived", envelope.Truth.Level)
	}
}

// TestCodeHandlerDivergenceReportTopPerKindBoundsTheTopSlice proves the
// top slice honors an explicit top_per_kind while the counts still cover
// the whole scanned window.
func TestCodeHandlerDivergenceReportTopPerKindBoundsTheTopSlice(t *testing.T) {
	t.Parallel()

	code, envelope := serveDivergenceReport(t, `{"repo_id":"repo-x","top_per_kind":1}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if envelope.Data.TopPerKind != 1 {
		t.Fatalf("top_per_kind = %d, want 1", envelope.Data.TopPerKind)
	}
	for kind, top := range envelope.Data.Top {
		if len(top) > 1 {
			t.Fatalf("top[%s] = %d findings, want <= 1", kind, len(top))
		}
	}
	if envelope.Data.Total != 3 {
		t.Fatalf("total = %d, want 3 (counts ignore the top slice)", envelope.Data.Total)
	}
}

// TestCodeHandlerDivergenceReportRejectsBadInput pins the 400s: a missing
// repo_id and an out-of-range top_per_kind fail before any read.
func TestCodeHandlerDivergenceReportRejectsBadInput(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{}`, `{"repo_id":""}`, `{"repo_id":"repo-x","top_per_kind":99}`, `{"repo_id":"repo-x","top_per_kind":-1}`} {
		if code, _ := serveDivergenceReport(t, body); code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want %d", body, code, http.StatusBadRequest)
		}
	}
}

// deadlineGraph returns the bounded graph-read deadline for every logical
// read: the deterministic stand-in for a sweep too slow for the 10-second
// budget (proven on the own-repo dogfood stack in #6840, where one outlier
// seed enumeration alone cost 9.6s).
type deadlineGraph struct{}

func (deadlineGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, querycontract.ErrGraphReadDeadline
}

func (deadlineGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, querycontract.ErrGraphReadDeadline
}

// shapedDivergenceStore serves one five-copy same-name family: shaped, so
// the exact assembly suppresses it under wrapper_family and the wrapper
// track must qualify it over the graph.
type shapedDivergenceStore struct {
	content.FakePortContentStore
}

func shapedDivergenceMembers() []codedivergence.Member {
	dirs := []string{"a", "b", "c", "d", "e"}
	members := make([]codedivergence.Member, 0, len(dirs))
	for i, dir := range dirs {
		members = append(members, codedivergence.Member{
			EntityID: fmt.Sprintf("w%d", i+1), EntityName: "recordAPICall",
			EntityType: "Function", RelativePath: dir + "/client.go",
			Language: "go", StartLine: 10, EndLine: 60, TokenCount: 120,
		})
	}
	return members
}

func (shapedDivergenceStore) DivergenceGroupStats(
	_ context.Context, _ string, kind codedivergence.Kind, _ int,
) ([]codedivergence.GroupStat, error) {
	if kind != codedivergence.KindExact {
		return nil, nil
	}
	return []codedivergence.GroupStat{
		{Kind: kind, Fingerprint: "fp-shaped", Members: 5, Tokens: 120},
	}, nil
}

func (shapedDivergenceStore) DivergenceMembers(
	_ context.Context, _ string, _ codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	for _, fingerprint := range fingerprints {
		if fingerprint == "fp-shaped" {
			out[fingerprint] = shapedDivergenceMembers()
		}
	}
	return out, nil
}

func (shapedDivergenceStore) DivergenceMembersByEntityID(
	context.Context, string, []string,
) (map[string]codedivergence.Member, error) {
	return map[string]codedivergence.Member{}, nil
}

func (shapedDivergenceStore) DriftedFindingStats(
	context.Context, string,
) ([]codedivergence.GroupStat, error) {
	return nil, nil
}

func (shapedDivergenceStore) DriftedFindingRows(
	context.Context, string, []string,
) (map[string]codedivergence.DriftedRow, error) {
	return map[string]codedivergence.DriftedRow{}, nil
}

// TestCodeHandlerDivergenceReportDegradesSlowGraphTracks pins the rollup
// robustness contract: a graph track the bounded read budget cuts short
// degrades to a counted *_graph_timeout suppression with its kind truncated
// instead of failing the other four kinds. The shaped family suppresses as
// exact (wrapper_family), both graph tracks time out, and the call still
// answers 200 with derived truth.
func TestCodeHandlerDivergenceReportDegradesSlowGraphTracks(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: shapedDivergenceStore{}, Neo4j: deadlineGraph{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/report",
		bytes.NewBufferString(`{"repo_id":"repo-x"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope reportEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if envelope.Data.Total != 0 {
		t.Fatalf("total = %d, want 0 (shaped family suppresses as exact, graph tracks time out)", envelope.Data.Total)
	}
	if got := envelope.Data.Suppressions["wrapper_family"]; got != 5 {
		t.Fatalf("wrapper_family suppressions = %d, want 5", got)
	}
	if got := envelope.Data.Suppressions["wrapper_graph_timeout"]; got != 5 {
		t.Fatalf("wrapper_graph_timeout suppressions = %d, want 5", got)
	}
	if got := envelope.Data.Suppressions["outlier_graph_timeout"]; got != 1 {
		t.Fatalf("outlier_graph_timeout suppressions = %d, want 1", got)
	}
	if !envelope.Data.Truncated["parallel_implementation.wrapper_bypass"] {
		t.Fatal("wrapper_bypass must report truncated after its track times out")
	}
	if !envelope.Data.Truncated["parallel_implementation.convention_outlier"] {
		t.Fatal("convention_outlier must report truncated after its sweep times out")
	}
	if envelope.Data.Truncated["parallel_implementation.exact"] {
		t.Fatal("exact must not report truncated: its window was complete")
	}
	if envelope.Truth.Level != "derived" {
		t.Fatalf("truth.level = %q, want derived", envelope.Truth.Level)
	}
}
