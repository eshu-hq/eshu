// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Divergence-findings handler proofs. These stay in codequery: they exercise
// the CodeHandler and DivergenceFindingsRequest through a fake store and
// pure validation, with no root-owned ContentReader SQL. The SQL-backed
// grouping proofs live in package query under the same basename.

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fakeDivergenceStore returns one fixture exact group: the same 210-token
// body in two files plus a generated third copy the assembly must count
// but not report.
type fakeDivergenceStore struct {
	querytestutil.FakePortContentStore
}

func (fakeDivergenceStore) DivergenceGroupStats(
	context.Context, string, codedivergence.Kind, int,
) ([]codedivergence.GroupStat, error) {
	return []codedivergence.GroupStat{
		{Fingerprint: "fp-fixture", Members: 3, Tokens: 210},
	}, nil
}

func (fakeDivergenceStore) DivergenceMembers(
	_ context.Context, _ string, _ codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	for _, fingerprint := range fingerprints {
		switch fingerprint {
		case "fp-fixture":
			out[fingerprint] = []codedivergence.Member{
				{EntityID: "e1", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "a/maps.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
				{EntityID: "e2", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "b/maps.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
				{EntityID: "e3", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "gen/maps.pb.go", Language: "go", StartLine: 8, EndLine: 58, TokenCount: 210},
			}
		case "fp-tests":
			out[fingerprint] = []codedivergence.Member{
				{EntityID: "t1", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "a/maps_test.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
				{EntityID: "t2", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "b/maps_test.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
			}
		}
	}
	return out, nil
}

// TestCodeHandlerDivergenceFindingsAgreesWithFixture pins the API/MCP
// agreement leg of the fixture: stored rows (the fake) assemble into the
// exact HTTP answer — two reported members, score 420, reasons summing to
// the score, one generated suppression counted, truth derived.
func TestCodeHandlerDivergenceFindingsAgreesWithFixture(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(`{"repo_id":"repo-x","kind":"exact"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Findings []struct {
				FindingID string `json:"finding_id"`
				Score     int    `json:"score"`
				Reasons   []struct {
					Value int `json:"value"`
				} `json:"reasons"`
				Members []struct {
					EntityID  string `json:"entity_id"`
					StartLine int    `json:"start_line"`
					EndLine   int    `json:"end_line"`
				} `json:"members"`
			} `json:"findings"`
			Suppressions map[string]int `json:"suppressions"`
			Truncated    bool           `json:"truncated"`
		} `json:"data"`
		Truth struct {
			Level string `json:"level"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if got, want := len(envelope.Data.Findings), 1; got != want {
		t.Fatalf("findings = %d, want %d body=%s", got, want, w.Body.String())
	}
	finding := envelope.Data.Findings[0]
	if finding.FindingID == "" {
		t.Fatal("finding_id must be set")
	}
	if want := 2 * 210; finding.Score != want {
		t.Fatalf("score = %d, want %d", finding.Score, want)
	}
	sum := 0
	for _, reason := range finding.Reasons {
		sum += reason.Value
	}
	if sum != finding.Score {
		t.Fatalf("reasons sum = %d, want score = %d", sum, finding.Score)
	}
	if got, want := len(finding.Members), 2; got != want {
		t.Fatalf("members = %d, want %d (generated copy suppressed)", got, want)
	}
	// P0 (#6877 review): member line spans must reach the response or
	// investigate follow-ups point at line 0-0.
	for i, want := range [][2]int{{10, 60}, {12, 62}} {
		if got := finding.Members[i].StartLine; got != want[0] {
			t.Fatalf("member %d start_line = %d, want %d", i, got, want[0])
		}
		if got := finding.Members[i].EndLine; got != want[1] {
			t.Fatalf("member %d end_line = %d, want %d", i, got, want[1])
		}
	}
	if got := envelope.Data.Suppressions["generated_file"]; got != 1 {
		t.Fatalf("generated_file suppressions = %d, want 1", got)
	}
	if envelope.Data.Truncated {
		t.Fatal("single-group page must not report truncated")
	}
	if envelope.Truth.Level != "derived" {
		t.Fatalf("truth.level = %q, want derived", envelope.Truth.Level)
	}
}

// crossKindCollisionStore returns the same fingerprint in both equality
// families: the P2 case from the #6877 owner review.
type crossKindCollisionStore struct {
	querytestutil.FakePortContentStore
}

func (crossKindCollisionStore) DivergenceGroupStats(
	_ context.Context, _ string, kind codedivergence.Kind, _ int,
) ([]codedivergence.GroupStat, error) {
	return []codedivergence.GroupStat{
		{Kind: kind, Fingerprint: "fp-shared", Members: 2, Tokens: 100},
	}, nil
}

func (crossKindCollisionStore) DivergenceMembers(
	_ context.Context, _ string, kind codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	for _, fingerprint := range fingerprints {
		if fingerprint != "fp-shared" {
			continue
		}
		prefix := "x"
		if kind == codedivergence.KindRenamed {
			prefix = "r"
		}
		out[fingerprint] = []codedivergence.Member{
			{EntityID: prefix + "1", EntityName: "shared", EntityType: "Function", RelativePath: "a/s.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 100},
			{EntityID: prefix + "2", EntityName: "shared", EntityType: "Function", RelativePath: "b/s.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 100},
		}
	}
	return out, nil
}

// TestCodeHandlerDivergenceFindingsKeepsCrossKindCollision pins the P2 fix
// end to end: a fingerprint present in both families assembles once per
// kind instead of colliding on the fingerprint alone.
func TestCodeHandlerDivergenceFindingsKeepsCrossKindCollision(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: crossKindCollisionStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/findings",
		bytes.NewBufferString(`{"repo_id":"repo-x","kind":""}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Findings []struct {
				FindingID string `json:"finding_id"`
				Kind      string `json:"kind"`
			} `json:"findings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if got, want := len(envelope.Data.Findings), 2; got != want {
		t.Fatalf("findings = %d, want %d body=%s", got, want, w.Body.String())
	}
	seen := map[string]string{}
	for _, finding := range envelope.Data.Findings {
		if prev, dup := seen[finding.FindingID]; dup {
			t.Fatalf("duplicate finding id %q for kinds %q and %q", finding.FindingID, prev, finding.Kind)
		}
		seen[finding.FindingID] = finding.Kind
	}
	if len(seen) != 2 {
		t.Fatalf("kinds seen = %v, want one exact and one renamed finding", seen)
	}
}

// suppressionPagingStore serves three groups for the paging and ordering
// legs: fp-dead (2 generated copies, fully suppressed, stat 600),
// fp-big (2 good + 1 generated at 100 tokens: stat 300, final 200), and
// fp-small (2 good at 120 tokens: stat and final 240).
type suppressionPagingStore struct {
	querytestutil.FakePortContentStore
}

func (suppressionPagingStore) DivergenceGroupStats(
	_ context.Context, _ string, kind codedivergence.Kind, _ int,
) ([]codedivergence.GroupStat, error) {
	// Exact-only groups (the renamed stream is empty here; cross-kind
	// coverage lives in the collision test).
	if kind != codedivergence.KindExact {
		return nil, nil
	}
	return []codedivergence.GroupStat{
		{Kind: kind, Fingerprint: "fp-dead", Members: 2, Tokens: 300},
		{Kind: kind, Fingerprint: "fp-big", Members: 3, Tokens: 100},
		{Kind: kind, Fingerprint: "fp-small", Members: 2, Tokens: 120},
	}, nil
}

func (suppressionPagingStore) DivergenceMembers(
	_ context.Context, _ string, _ codedivergence.Kind, fingerprints []string,
) (map[string][]codedivergence.Member, error) {
	out := map[string][]codedivergence.Member{}
	for _, fingerprint := range fingerprints {
		switch fingerprint {
		case "fp-dead":
			out[fingerprint] = []codedivergence.Member{
				{EntityID: "d1", EntityName: "dead", EntityType: "Function", RelativePath: "gen/a.pb.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 300},
				{EntityID: "d2", EntityName: "dead", EntityType: "Function", RelativePath: "gen/b.pb.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 300},
			}
		case "fp-big":
			out[fingerprint] = []codedivergence.Member{
				{EntityID: "b1", EntityName: "big", EntityType: "Function", RelativePath: "a/b.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 100},
				{EntityID: "b2", EntityName: "big", EntityType: "Function", RelativePath: "b/b.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 100},
				{EntityID: "b3", EntityName: "big", EntityType: "Function", RelativePath: "gen/c.pb.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 100},
			}
		case "fp-small":
			out[fingerprint] = []codedivergence.Member{
				{EntityID: "s1", EntityName: "small", EntityType: "Function", RelativePath: "a/s.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 120},
				{EntityID: "s2", EntityName: "small", EntityType: "Function", RelativePath: "b/s.go", Language: "go", StartLine: 1, EndLine: 60, TokenCount: 120},
			}
		}
	}
	return out, nil
}

type divergencePage struct {
	Findings []struct {
		FindingID string `json:"finding_id"`
		Score     int    `json:"score"`
	} `json:"findings"`
	Truncated  bool `json:"truncated"`
	NextOffset any  `json:"next_offset"`
}

func postDivergenceFindings(t *testing.T, store ContentStore, body string) divergencePage {
	t.Helper()
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/divergence/findings", bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope struct {
		Data divergencePage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	return envelope.Data
}

// TestCodeHandlerDivergenceFindingsNextOffsetChainsPastSuppression pins
// the P1 leg: next_offset advances by consumed stats, so a
// fully-suppressed window still moves the cursor and chained pages never
// overlap or repeat.
func TestCodeHandlerDivergenceFindingsNextOffsetChainsPastSuppression(t *testing.T) {
	t.Parallel()

	store := suppressionPagingStore{}
	page1 := postDivergenceFindings(t, store, `{"repo_id":"repo-x","limit":1,"offset":0}`)
	if got, want := len(page1.Findings), 0; got != want {
		t.Fatalf("page 1 findings = %d, want %d (dead group suppresses fully)", got, want)
	}
	if !page1.Truncated {
		t.Fatal("page 1 must report truncated with stats behind the window")
	}
	if got, want := page1.NextOffset, float64(1); got != want {
		t.Fatalf("page 1 next_offset = %v, want %v (must advance past the consumed window)", got, want)
	}
	page2 := postDivergenceFindings(t, store, `{"repo_id":"repo-x","limit":1,"offset":1}`)
	if got, want := len(page2.Findings), 1; got != want {
		t.Fatalf("page 2 findings = %d, want %d", got, want)
	}
	if got, want := page2.NextOffset, float64(2); got != want {
		t.Fatalf("page 2 next_offset = %v, want %v", got, want)
	}
	page3 := postDivergenceFindings(t, store, `{"repo_id":"repo-x","limit":1,"offset":2}`)
	if got, want := len(page3.Findings), 1; got != want {
		t.Fatalf("page 3 findings = %d, want %d", got, want)
	}
	if page3.Truncated || page3.NextOffset != nil {
		t.Fatalf("page 3 must end the chain, got truncated=%v next=%v", page3.Truncated, page3.NextOffset)
	}
	seen := map[string]bool{}
	for _, page := range []divergencePage{page1, page2, page3} {
		for _, finding := range page.Findings {
			if seen[finding.FindingID] {
				t.Fatalf("duplicate finding %q across chained pages", finding.FindingID)
			}
			seen[finding.FindingID] = true
		}
	}
}

// TestCodeHandlerDivergenceFindingsEmitFinalScoreOrder pins the P2 leg:
// emission follows final post-suppression scores (240 before 200) even
// though the stat window ranked the 300-stat group first.
func TestCodeHandlerDivergenceFindingsEmitFinalScoreOrder(t *testing.T) {
	t.Parallel()

	page := postDivergenceFindings(t, suppressionPagingStore{}, `{"repo_id":"repo-x","limit":10}`)
	if got, want := len(page.Findings), 2; got != want {
		t.Fatalf("findings = %d, want %d (dead group drops)", got, want)
	}
	if got, want := page.Findings[0].Score, 240; got != want {
		t.Fatalf("first score = %d, want %d (final order, not stat order)", got, want)
	}
	if got, want := page.Findings[1].Score, 200; got != want {
		t.Fatalf("second score = %d, want %d", got, want)
	}
}

// TestDivergenceFindingsValidationBounds pins request validation: repo_id
// is required, kind is closed, limit/offset are bounded.
func TestDivergenceFindingsValidationBounds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		req  DivergenceFindingsRequest
		want string
	}{
		{"missing repo", DivergenceFindingsRequest{Kind: "exact"}, "repo_id is required"},
		{"bad kind", DivergenceFindingsRequest{RepoID: "r", Kind: "drifted"}, "kind must be one of"},
		{"over limit", DivergenceFindingsRequest{RepoID: "r", Limit: 101}, "limit must be <="},
		{"negative offset", DivergenceFindingsRequest{RepoID: "r", Offset: -1}, "offset must be >="},
		{"over offset", DivergenceFindingsRequest{RepoID: "r", Offset: 10001}, "offset must be <="},
	} {
		if err := tc.req.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: validate() = %v, want error containing %q", tc.name, err, tc.want)
		}
	}
}
