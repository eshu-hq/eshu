// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestValidateQuerySuiteRejectsMissingRequiredShape(t *testing.T) {
	t.Parallel()

	err := ValidateQuerySuite(QuerySuite{
		Queries: []Query{{ID: "q-1"}},
	})
	if err == nil {
		t.Fatal("ValidateQuerySuite() error = nil, want required shape errors")
	}
	for _, want := range []string{
		"version is required",
		"at least 15 queries are required",
		"queries[0].text is required",
		"queries[0].mode is invalid",
		"queries[0].limit is required",
		"queries[0].scope is required",
		"queries[0].expected_handles are required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateQuerySuite() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateQuerySuiteRejectsDuplicateIDsAndMissingScope(t *testing.T) {
	t.Parallel()

	suite := validQuerySuiteFixture()
	suite.Queries[1].ID = suite.Queries[0].ID
	suite.Queries[2].RepoID = ""

	err := ValidateQuerySuite(suite)
	if err == nil {
		t.Fatal("ValidateQuerySuite() error = nil, want duplicate and missing scope errors")
	}
	for _, want := range []string{
		"queries[1].id duplicates q-semantic-01",
		"queries[2].scope is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateQuerySuite() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateQuerySuiteRejectsLimitAboveMaximum(t *testing.T) {
	t.Parallel()

	suite := validQuerySuiteFixture()
	suite.Queries[0].Limit = MaximumQueryLimit + 1

	err := ValidateQuerySuite(suite)
	if err == nil {
		t.Fatal("ValidateQuerySuite() error = nil, want max-limit error")
	}
	if want := "queries[0].limit exceeds maximum of 100"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateQuerySuite() error = %q, want substring %q", err, want)
	}
}

func TestValidateQuerySuiteAcceptsFifteenCaseSuite(t *testing.T) {
	t.Parallel()

	if err := ValidateQuerySuite(validQuerySuiteFixture()); err != nil {
		t.Fatalf("ValidateQuerySuite() error = %v, want nil", err)
	}
}

func TestValidateQuerySuiteAcceptsTrimmedVersion(t *testing.T) {
	t.Parallel()

	suite := validQuerySuiteFixture()
	suite.Version = "  " + QuerySuiteVersion + "\n"

	if err := ValidateQuerySuite(suite); err != nil {
		t.Fatalf("ValidateQuerySuite() error = %v, want nil", err)
	}
}

func TestScoreQuerySuiteAggregatesMetricsInSuiteOrder(t *testing.T) {
	t.Parallel()

	suite := validQuerySuiteFixture()
	results := make(map[string][]Result)
	for i, query := range suite.Queries {
		if i >= 10 {
			continue
		}
		doc := derivedResultDoc(query.ExpectedHandles[0])
		if i == 0 {
			doc.TruthScope.Level = searchdocs.TruthLevel("canonical")
		}
		results[query.ID] = []Result{{Document: doc, Rank: 1}}
	}

	score, err := ScoreQuerySuite(suite, results)
	if err != nil {
		t.Fatalf("ScoreQuerySuite() error = %v, want nil", err)
	}

	if got, want := score.QueryCount, 15; got != want {
		t.Fatalf("score.QueryCount = %d, want %d", got, want)
	}
	if got, want := score.Metrics.Recall, 2.0/3.0; got != want {
		t.Fatalf("score.Metrics.Recall = %v, want %v", got, want)
	}
	if got, want := score.Metrics.Precision, 2.0/3.0; got != want {
		t.Fatalf("score.Metrics.Precision = %v, want %v", got, want)
	}
	if got, want := score.Metrics.NDCG, 2.0/3.0; got != want {
		t.Fatalf("score.Metrics.NDCG = %v, want %v", got, want)
	}
	if score.Metrics.FalseCanonicalClaimCount == nil {
		t.Fatal("FalseCanonicalClaimCount = nil, want aggregate count")
	}
	if got, want := *score.Metrics.FalseCanonicalClaimCount, 1; got != want {
		t.Fatalf("FalseCanonicalClaimCount = %d, want %d", got, want)
	}
	if got, want := len(score.PerQuery), 15; got != want {
		t.Fatalf("len(score.PerQuery) = %d, want %d", got, want)
	}
	if got, want := score.PerQuery[0].QueryID, "q-semantic-01"; got != want {
		t.Fatalf("score.PerQuery[0].QueryID = %q, want %q", got, want)
	}
}

func TestScoreQuerySuiteUsesTrimmedQueryIDs(t *testing.T) {
	t.Parallel()

	suite := validQuerySuiteFixture()
	suite.Queries[0].ID = "  " + suite.Queries[0].ID + "  "
	results := map[string][]Result{
		"q-semantic-01": {
			{Document: derivedResultDoc(suite.Queries[0].ExpectedHandles[0]), Rank: 1},
		},
	}

	score, err := ScoreQuerySuite(suite, results)
	if err != nil {
		t.Fatalf("ScoreQuerySuite() error = %v, want nil", err)
	}

	if got, want := score.PerQuery[0].QueryID, "q-semantic-01"; got != want {
		t.Fatalf("score.PerQuery[0].QueryID = %q, want %q", got, want)
	}
	if got, want := score.PerQuery[0].Metrics.Recall, 1.0; got != want {
		t.Fatalf("score.PerQuery[0].Metrics.Recall = %v, want %v", got, want)
	}
}

func validQuerySuiteFixture() QuerySuite {
	queries := make([]Query, 0, MinimumQuerySuiteSize)
	for i := 1; i <= MinimumQuerySuiteSize; i++ {
		id := queryID(i)
		queries = append(queries, Query{
			ID:              id,
			Text:            "Which service owns checkout capability " + id + "?",
			RepoID:          "repo-checkout",
			Mode:            ModeHybrid,
			Limit:           10,
			ExpectedHandles: []string{"service:checkout-" + id},
		})
	}
	return QuerySuite{
		Version: QuerySuiteVersion,
		Queries: queries,
	}
}

func derivedResultDoc(handle string) searchdocs.Document {
	kind, id, _ := strings.Cut(handle, ":")
	return searchdocs.Document{
		ID:           "searchdoc:" + handle,
		GraphHandles: []searchdocs.GraphHandle{{Kind: kind, ID: id}},
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisContentIndex,
		},
	}
}

func queryID(index int) string {
	return fmt.Sprintf("q-semantic-%02d", index)
}
