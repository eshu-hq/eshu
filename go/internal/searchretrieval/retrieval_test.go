// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchretrieval

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestValidateRequestRejectsUnboundedInputs(t *testing.T) {
	t.Parallel()

	req := Request{
		Query: "who owns the checkout service",
		Mode:  searchbench.ModeSemantic,
	}

	err := ValidateRequest(req)
	if err == nil {
		t.Fatal("ValidateRequest() error = nil, want bounded request errors")
	}
	for _, want := range []string{
		"scope is required",
		"limit is required",
		"timeout is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRequest() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateRequestRejectsInvalidModeAndEmptyQuery(t *testing.T) {
	t.Parallel()

	req := Request{
		Scope:   Scope{RepoID: "repo-checkout"},
		Mode:    searchbench.Mode("whole_graph"),
		Limit:   10,
		Timeout: 2 * time.Second,
	}

	err := ValidateRequest(req)
	if err == nil {
		t.Fatal("ValidateRequest() error = nil, want query and mode errors")
	}
	for _, want := range []string{
		"query is required",
		"mode is invalid",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRequest() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateRequestRejectsLimitAboveMaximum(t *testing.T) {
	t.Parallel()

	req := validRequestFixture()
	req.Limit = maxLimit + 1

	err := ValidateRequest(req)
	if err == nil {
		t.Fatal("ValidateRequest() error = nil, want max-limit error")
	}
	if want := "limit exceeds maximum of 100"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRequest() error = %q, want substring %q", err, want)
	}
}

func TestScopeAnchorPrefersSmallestAvailableScope(t *testing.T) {
	t.Parallel()

	anchor := Scope{
		RepoID:      "repo-checkout",
		ServiceID:   "svc-checkout",
		WorkloadID:  "workload-checkout",
		Environment: "prod",
	}.Anchor()

	if got, want := anchor.Kind, ScopeKindService; got != want {
		t.Fatalf("anchor.Kind = %q, want %q", got, want)
	}
	if got, want := anchor.ID, "svc-checkout"; got != want {
		t.Fatalf("anchor.ID = %q, want %q", got, want)
	}
}

func TestBuildResponseNormalizesTopKAndTruncation(t *testing.T) {
	t.Parallel()

	req := validRequestFixture()
	req.Limit = 2
	response, err := BuildResponse(req, []Candidate{
		{Document: documentFixture("searchdoc:3", "repo-checkout:README.md"), Score: 0.5},
		{Document: documentFixture("searchdoc:1", "service:checkout"), Score: 0.9},
		{Document: documentFixture("searchdoc:2", "workload:checkout-api"), Score: 0.9},
	})
	if err != nil {
		t.Fatalf("BuildResponse() error = %v, want nil", err)
	}

	if !response.Truncated {
		t.Fatal("response.Truncated = false, want true")
	}
	if got, want := len(response.Results), 2; got != want {
		t.Fatalf("len(response.Results) = %d, want %d", got, want)
	}
	for i, wantID := range []string{"searchdoc:1", "searchdoc:2"} {
		if got := response.Results[i].Document.ID; got != wantID {
			t.Fatalf("response.Results[%d].Document.ID = %q, want %q", i, got, wantID)
		}
		if got, want := response.Results[i].Rank, i+1; got != want {
			t.Fatalf("response.Results[%d].Rank = %d, want %d", i, got, want)
		}
	}
	if got, want := response.Mode, searchbench.ModeHybrid; got != want {
		t.Fatalf("response.Mode = %q, want %q", got, want)
	}
}

func TestBuildResponseCountsFalseCanonicalClaims(t *testing.T) {
	t.Parallel()

	canonical := documentFixture("searchdoc:canonical", "service:checkout")
	canonical.TruthScope.Level = searchdocs.TruthLevel("canonical")

	response, err := BuildResponse(validRequestFixture(), []Candidate{
		{Document: canonical, Score: 1},
		{Document: documentFixture("searchdoc:derived", "workload:checkout-api"), Score: 0.8},
	})
	if err != nil {
		t.Fatalf("BuildResponse() error = %v, want nil", err)
	}

	if got, want := response.FalseCanonicalClaimCount, 1; got != want {
		t.Fatalf("FalseCanonicalClaimCount = %d, want %d", got, want)
	}
	if got, want := response.Results[0].TruthScope.Level, searchdocs.TruthLevel("canonical"); got != want {
		t.Fatalf("result truth level = %q, want %q", got, want)
	}
}

func TestBuildResponseRejectsCandidatesWithoutDocumentIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate Candidate
		want      string
	}{
		{
			name:      "missing document id",
			candidate: Candidate{Document: documentFixture("", "service:checkout"), Score: 0.9},
			want:      "candidate document.id is required",
		},
		{
			name: "missing graph handles",
			candidate: Candidate{
				Document: func() searchdocs.Document {
					doc := documentFixture("searchdoc:checkout", "service:checkout")
					doc.GraphHandles = nil
					return doc
				}(),
				Score: 0.9,
			},
			want: "candidate document.graph_handles are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := BuildResponse(validRequestFixture(), []Candidate{tt.candidate})
			if err == nil {
				t.Fatal("BuildResponse() error = nil, want candidate identity error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildResponse() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestBuildResponseCopiesDocumentGraphHandles(t *testing.T) {
	t.Parallel()

	candidate := Candidate{
		Document: documentFixture("searchdoc:checkout", "service:checkout"),
		Score:    0.9,
		Metadata: map[string]string{"search_method": "bm25"},
	}
	candidate.Document.EntityRefs = []searchdocs.EntityRef{{ID: "entity:checkout", Name: "Checkout"}}
	candidate.Document.Labels = []string{"service", "owner"}

	response, err := BuildResponse(validRequestFixture(), []Candidate{candidate})
	if err != nil {
		t.Fatalf("BuildResponse() error = %v, want nil", err)
	}

	candidate.Document.GraphHandles[0].ID = "mutated"
	candidate.Document.EntityRefs[0].ID = "mutated"
	candidate.Document.Labels[0] = "mutated"
	candidate.Metadata["search_method"] = "mutated"

	if got, want := response.Results[0].Document.GraphHandles[0].ID, "checkout"; got != want {
		t.Fatalf("response document graph handle ID = %q, want %q", got, want)
	}
	if got, want := response.Results[0].Handles[0].ID, "checkout"; got != want {
		t.Fatalf("response handles ID = %q, want %q", got, want)
	}
	if got, want := response.Results[0].Document.EntityRefs[0].ID, "entity:checkout"; got != want {
		t.Fatalf("response document entity ref ID = %q, want %q", got, want)
	}
	if got, want := response.Results[0].Document.Labels[0], "service"; got != want {
		t.Fatalf("response document label = %q, want %q", got, want)
	}
	if got, want := response.Results[0].Metadata["search_method"], "bm25"; got != want {
		t.Fatalf("response metadata search_method = %q, want %q", got, want)
	}
}

func TestBuildResponseRejectsNonFiniteScores(t *testing.T) {
	t.Parallel()

	_, err := BuildResponse(validRequestFixture(), []Candidate{
		{Document: documentFixture("searchdoc:nan", "service:checkout"), Score: math.NaN()},
	})
	if err == nil {
		t.Fatal("BuildResponse() error = nil, want non-finite score error")
	}
	if !strings.Contains(err.Error(), "candidate score must be finite") {
		t.Fatalf("BuildResponse() error = %q, want finite score error", err)
	}
}

func TestSearchbenchResultsFeedScoring(t *testing.T) {
	t.Parallel()

	response, err := BuildResponse(validRequestFixture(), []Candidate{
		{Document: documentFixture("searchdoc:service", "service:checkout"), Score: 1},
		{Document: documentFixture("searchdoc:file", "file:repo-checkout:cmd/api/main.go"), Score: 0.8},
	})
	if err != nil {
		t.Fatalf("BuildResponse() error = %v, want nil", err)
	}

	metrics := searchbench.ScoreQueryResults(searchbench.Query{
		ID:              "q-checkout-owner",
		ExpectedHandles: []string{"service:checkout"},
	}, response.SearchbenchResults())

	if got, want := metrics.Recall, 1.0; got != want {
		t.Fatalf("metrics.Recall = %v, want %v", got, want)
	}
	if metrics.FalseCanonicalClaimCount == nil {
		t.Fatal("FalseCanonicalClaimCount = nil, want measured count")
	}
	if got, want := *metrics.FalseCanonicalClaimCount, 0; got != want {
		t.Fatalf("FalseCanonicalClaimCount = %d, want %d", got, want)
	}
}

func validRequestFixture() Request {
	return Request{
		QueryID: "q-checkout-owner",
		Query:   "who owns the checkout service",
		Scope:   Scope{RepoID: "repo-checkout"},
		Mode:    searchbench.ModeHybrid,
		Limit:   10,
		Timeout: 2 * time.Second,
	}
}

func documentFixture(id string, handle string) searchdocs.Document {
	kind, handleID, _ := strings.Cut(handle, ":")
	return searchdocs.Document{
		ID:         id,
		RepoID:     "repo-checkout",
		SourceKind: searchdocs.SourceKindRuntimeSummary,
		Title:      id,
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisReadModel,
		},
		Freshness: searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		GraphHandles: []searchdocs.GraphHandle{
			{Kind: kind, ID: handleID},
		},
	}
}
