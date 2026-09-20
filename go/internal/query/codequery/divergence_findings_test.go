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
		if fingerprint != "fp-fixture" {
			continue
		}
		out[fingerprint] = []codedivergence.Member{
			{EntityID: "e1", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "a/maps.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: 210},
			{EntityID: "e2", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "b/maps.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: 210},
			{EntityID: "e3", EntityName: "cloneStringMap", EntityType: "Function", RelativePath: "gen/maps.pb.go", Language: "go", StartLine: 8, EndLine: 58, TokenCount: 210},
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
					EntityID string `json:"entity_id"`
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
