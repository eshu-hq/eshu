// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchretrieval

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

const maxLimit = 100

// ScopeKind names the anchor used to bound one retrieval request.
type ScopeKind string

const (
	// ScopeKindService scopes retrieval to one service.
	ScopeKindService ScopeKind = "service"
	// ScopeKindWorkload scopes retrieval to one workload.
	ScopeKindWorkload ScopeKind = "workload"
	// ScopeKindRepo scopes retrieval to one repository.
	ScopeKindRepo ScopeKind = "repository"
	// ScopeKindEnvironment scopes retrieval to one environment.
	ScopeKindEnvironment ScopeKind = "environment"
)

// Scope constrains retrieval before any backend search runs.
type Scope struct {
	ServiceID   string `json:"service_id,omitempty"`
	WorkloadID  string `json:"workload_id,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// Anchor is the smallest available scope selected for a request.
type Anchor struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

// Anchor returns the smallest available scope for bounded retrieval.
func (scope Scope) Anchor() Anchor {
	for _, candidate := range []Anchor{
		{Kind: ScopeKindService, ID: strings.TrimSpace(scope.ServiceID)},
		{Kind: ScopeKindWorkload, ID: strings.TrimSpace(scope.WorkloadID)},
		{Kind: ScopeKindRepo, ID: strings.TrimSpace(scope.RepoID)},
		{Kind: ScopeKindEnvironment, ID: strings.TrimSpace(scope.Environment)},
	} {
		if candidate.ID != "" {
			return candidate
		}
	}
	return Anchor{}
}

// Request is one bounded internal semantic-evaluation retrieval request.
type Request struct {
	QueryID string           `json:"query_id,omitempty"`
	Query   string           `json:"query"`
	Scope   Scope            `json:"scope"`
	Mode    searchbench.Mode `json:"mode"`
	Limit   int              `json:"limit"`
	Timeout time.Duration    `json:"timeout_ns"`
}

// Candidate is one backend-produced search-document candidate.
type Candidate struct {
	Document searchdocs.Document        `json:"document"`
	Score    float64                    `json:"score"`
	Failures []searchbench.FailureClass `json:"failures,omitempty"`
	Metadata map[string]string          `json:"metadata,omitempty"`
}

// Result is one normalized retrieval result.
type Result struct {
	Document   searchdocs.Document        `json:"document"`
	Rank       int                        `json:"rank"`
	Score      float64                    `json:"score"`
	TruthScope searchdocs.TruthScope      `json:"truth_scope"`
	Freshness  searchdocs.Freshness       `json:"freshness"`
	Handles    []searchdocs.GraphHandle   `json:"graph_handles"`
	Failures   []searchbench.FailureClass `json:"failures,omitempty"`
	Metadata   map[string]string          `json:"metadata,omitempty"`
}

// Response is the normalized bounded retrieval output.
type Response struct {
	QueryID                  string           `json:"query_id,omitempty"`
	Query                    string           `json:"query"`
	Anchor                   Anchor           `json:"anchor"`
	Mode                     searchbench.Mode `json:"mode"`
	Limit                    int              `json:"limit"`
	Timeout                  time.Duration    `json:"timeout_ns"`
	Results                  []Result         `json:"results"`
	Truncated                bool             `json:"truncated"`
	FalseCanonicalClaimCount int              `json:"false_canonical_claim_count"`
}

// ValidateRequest checks that a retrieval request is bounded before backend use.
func ValidateRequest(req Request) error {
	var problems []string
	if strings.TrimSpace(req.Query) == "" {
		problems = append(problems, "query is required")
	}
	if req.Scope.Anchor().Kind == "" {
		problems = append(problems, "scope is required")
	}
	if !validMode(req.Mode) {
		problems = append(problems, "mode is invalid")
	}
	if req.Limit <= 0 {
		problems = append(problems, "limit is required")
	} else if req.Limit > maxLimit {
		problems = append(problems, fmt.Sprintf("limit exceeds maximum of %d", maxLimit))
	}
	if req.Timeout <= 0 {
		problems = append(problems, "timeout is required")
	}
	return joinedValidationError(problems)
}

// BuildResponse validates a request and normalizes ranked backend candidates.
func BuildResponse(req Request, candidates []Candidate) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Document.ID) == "" {
			return Response{}, errors.New("candidate document.id is required")
		}
		if !hasGraphHandle(candidate.Document.GraphHandles) {
			return Response{}, fmt.Errorf("candidate document.graph_handles are required: document_id=%q", candidate.Document.ID)
		}
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			return Response{}, fmt.Errorf("candidate score must be finite: document_id=%q", candidate.Document.ID)
		}
	}

	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(i int, j int) bool {
		if ordered[i].Score != ordered[j].Score {
			return ordered[i].Score > ordered[j].Score
		}
		return ordered[i].Document.ID < ordered[j].Document.ID
	})

	truncated := len(ordered) > req.Limit
	if truncated {
		ordered = ordered[:req.Limit]
	}

	results := make([]Result, 0, len(ordered))
	falseCanonicalClaims := 0
	for i, candidate := range ordered {
		doc := cloneDocument(candidate.Document)
		if doc.TruthScope.Level != searchdocs.TruthLevelDerived {
			falseCanonicalClaims++
		}
		results = append(results, Result{
			Document:   doc,
			Rank:       i + 1,
			Score:      candidate.Score,
			TruthScope: doc.TruthScope,
			Freshness:  doc.Freshness,
			Handles:    append([]searchdocs.GraphHandle(nil), doc.GraphHandles...),
			Failures:   append([]searchbench.FailureClass(nil), candidate.Failures...),
			Metadata:   cloneStringMap(candidate.Metadata),
		})
	}

	return Response{
		QueryID:                  strings.TrimSpace(req.QueryID),
		Query:                    strings.TrimSpace(req.Query),
		Anchor:                   req.Scope.Anchor(),
		Mode:                     req.Mode,
		Limit:                    req.Limit,
		Timeout:                  req.Timeout,
		Results:                  results,
		Truncated:                truncated,
		FalseCanonicalClaimCount: falseCanonicalClaims,
	}, nil
}

// SearchbenchResults converts retrieval output into benchmark scoring input.
func (response Response) SearchbenchResults() []searchbench.Result {
	results := make([]searchbench.Result, 0, len(response.Results))
	for _, result := range response.Results {
		results = append(results, searchbench.Result{
			Document: result.Document,
			Rank:     result.Rank,
		})
	}
	return results
}

func hasGraphHandle(handles []searchdocs.GraphHandle) bool {
	for _, handle := range handles {
		if strings.TrimSpace(handle.Kind) != "" && strings.TrimSpace(handle.ID) != "" {
			return true
		}
	}
	return false
}

func cloneDocument(doc searchdocs.Document) searchdocs.Document {
	doc.EntityRefs = append([]searchdocs.EntityRef(nil), doc.EntityRefs...)
	doc.GraphHandles = append([]searchdocs.GraphHandle(nil), doc.GraphHandles...)
	doc.Labels = append([]string(nil), doc.Labels...)
	doc.Provenance.SourceIDs = append([]string(nil), doc.Provenance.SourceIDs...)
	return doc
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func validMode(mode searchbench.Mode) bool {
	switch mode {
	case searchbench.ModeKeyword, searchbench.ModeSemantic, searchbench.ModeHybrid:
		return true
	default:
		return false
	}
}

func joinedValidationError(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	errs := make([]error, 0, len(problems))
	for _, problem := range problems {
		errs = append(errs, errors.New(problem))
	}
	return errors.Join(errs...)
}
