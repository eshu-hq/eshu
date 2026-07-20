// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestFetchCloudResourceResultUsesUniqueSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "LIMIT $cloud_observation_limit") {
			t.Fatalf("cloud resource observation query is unbounded: %s", cypher)
		}
		if got, want := IntVal(params, "cloud_observation_limit"), cloudResourceObservationLimit+1; got != want {
			t.Fatalf("cloud_observation_limit = %d, want %d", got, want)
		}
		rows := make([]map[string]any, 0, serviceStoryItemLimit+1)
		for index := range serviceStoryItemLimit + 1 {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("resource:%03d", index)})
		}
		return rows, nil
	}}
	handler := &ImpactHandler{Neo4j: reader}

	result, err := handler.fetchCloudResourceResult(t.Context(), "repository:orders", "workload:orders")
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), serviceStoryItemLimit; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want lower-bound truncation", result.limits)
	}
}

func TestFetchCloudResourceResultOmitsUnownedEvidenceForScopedTokens(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		calls++
		return []map[string]any{{"id": "cloud:unowned"}}, nil
	}}
	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:allowed"},
	})

	result, err := (&ImpactHandler{Neo4j: reader}).fetchCloudResourceResult(
		ctx,
		"repository:allowed",
		"workload:orders",
	)
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if len(result.rows) != 0 {
		t.Fatalf("rows = %#v, want no repository-unowned cloud evidence", result.rows)
	}
	if len(result.limits) != 0 {
		t.Fatalf("limits = %#v, want completeness metadata withheld with cloud evidence", result.limits)
	}
	if calls != 0 {
		t.Fatalf("graph calls = %d, want zero for scoped token", calls)
	}
}

func TestFetchCloudResourceResultBindsExactRepository(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		for _, want := range []string{
			"MATCH (repo:Repository)-[:DEFINES]->(w:Workload {id: $workload_id})",
			"WHERE repo.id = $repo_id",
		} {
			if !strings.Contains(cypher, want) {
				t.Fatalf("cloud resource query missing exact repository anchor %q: %s", want, cypher)
			}
		}
		if got, want := StringVal(params, "repo_id"), "repository:orders"; got != want {
			t.Fatalf("repo_id = %q, want %q", got, want)
		}
		return []map[string]any{{"id": "cloud:orders"}}, nil
	}}

	result, err := (&ImpactHandler{Neo4j: reader}).fetchCloudResourceResult(
		t.Context(),
		"repository:orders",
		"workload:orders",
	)
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("rows = %#v, want %d resource", result.rows, want)
	}
}

func TestFetchCloudResourceResultReturnsBoundedObservationRowsWithoutAggregation(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		rowOrder := strings.Index(cypher, "ORDER BY sort_name, sort_id, sort_confidence DESC")
		rowLimit := strings.Index(cypher, "LIMIT $cloud_observation_limit")
		if rowOrder < 0 || rowLimit < 0 {
			t.Fatalf("cloud resource query must bound relationship observations: %s", cypher)
		}
		returnRows := strings.Index(cypher, "RETURN c.id as id")
		if !strings.Contains(cypher, "WITH c, i, rel,") ||
			!strings.Contains(cypher, "c.name AS sort_name") ||
			returnRows < 0 || rowOrder > rowLimit || rowLimit > returnRows {
			t.Fatalf("cloud resource query must deterministically order and cap traversal rows before terminal projection: %s", cypher)
		}
		if strings.Contains(cypher, "collect(") {
			t.Fatalf("cloud resource query must return bounded observation rows for Go aggregation: %s", cypher)
		}
		if got, want := IntVal(params, "cloud_observation_limit"), serviceStoryItemLimit*serviceStoryItemLimit+1; got != want {
			t.Fatalf("cloud_observation_limit = %d, want %d", got, want)
		}
		return []map[string]any{
			{"id": "cloud:orders", "name": "orders", "observation": map[string]any{"confidence": 0.5, "source_fact_id": "fact:lower"}},
			{"id": "cloud:orders", "name": "orders", "observation": map[string]any{"confidence": 0.9, "source_fact_id": "fact:selected"}},
		}, nil
	}}

	result, err := (&ImpactHandler{Neo4j: reader}).fetchCloudResourceResult(
		t.Context(),
		"repository:orders",
		"workload:orders",
	)
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("resources = %#v, want %d grouped resource", result.rows, want)
	}
	if got, want := floatVal(result.rows[0], "confidence"), 0.9; got != want {
		t.Fatalf("selected confidence = %v, want %v", got, want)
	}
	if got, want := IntVal(result.limits, "observation_count"), 2; got != want {
		t.Fatalf("observation_count = %d, want %d", got, want)
	}
}

func TestFetchCloudResourceResultReportsObservationSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"id":                "cloud:orders",
			"observation_count": serviceStoryItemLimit*serviceStoryItemLimit + 1,
			"observations":      []map[string]any{{"confidence": 0.9}},
		}}, nil
	}}

	result, err := (&ImpactHandler{Neo4j: reader}).fetchCloudResourceResult(
		t.Context(),
		"repository:orders",
		"workload:orders",
	)
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if !BoolVal(result.limits, "truncated") || !BoolVal(result.limits, "observed_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want observation lower-bound truncation", result.limits)
	}
	if got, want := IntVal(result.limits, "observation_limit"), cloudResourceObservationLimit; got != want {
		t.Fatalf("observation_limit = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "observation_query_sentinel_limit"), cloudResourceObservationLimit+1; got != want {
		t.Fatalf("observation_query_sentinel_limit = %d, want %d", got, want)
	}
	if got, want := IntVal(result.limits, "observation_count"), cloudResourceObservationLimit+1; got != want {
		t.Fatalf("observation_count = %d, want %d", got, want)
	}
	if !BoolVal(result.limits, "observation_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want observation_count_is_lower_bound", result.limits)
	}
}

func TestFetchCloudResourceResultMarksObservationCountLowerBoundAtResourceSentinel(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		rows := make([]map[string]any, 0, serviceStoryItemLimit+1)
		for index := range serviceStoryItemLimit + 1 {
			rows = append(rows, map[string]any{
				"id":                fmt.Sprintf("resource:%03d", index),
				"observation_count": 1,
			})
		}
		return rows, nil
	}}

	result, err := (&ImpactHandler{Neo4j: reader}).fetchCloudResourceResult(
		t.Context(),
		"repository:orders",
		"workload:orders",
	)
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := IntVal(result.limits, "observation_count"), serviceStoryItemLimit+1; got != want {
		t.Fatalf("observation_count = %d, want returned-resource lower bound %d", got, want)
	}
	if !BoolVal(result.limits, "observation_count_is_lower_bound") {
		t.Fatalf("limits = %#v, want global observation lower bound", result.limits)
	}
}

func TestFetchCloudResourceResultKeepsOneProvenanceObservationIntact(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "properties(rel) as observation") {
			t.Fatalf("cloud resource query must return complete provenance observations: %s", cypher)
		}
		return []map[string]any{
			{
				"id":                   "cloud:orders",
				"name":                 "orders",
				"resource_environment": "prod",
				"instance_environment": "staging",
				"observation": map[string]any{
					"confidence":         0.50,
					"reason":             "lower-confidence-reason",
					"relationship_basis": "lower-confidence-basis",
					"source_fact_id":     "fact:lower",
				},
			},
			{
				"id":                   "cloud:orders",
				"name":                 "orders",
				"resource_environment": "prod",
				"instance_environment": "staging",
				"observation": map[string]any{
					"confidence":         0.95,
					"reason":             "selected-reason",
					"relationship_basis": "selected-basis",
					"source_fact_id":     "fact:selected",
				},
			},
		}, nil
	}}
	handler := &ImpactHandler{Neo4j: reader}

	result, err := handler.fetchCloudResourceResult(t.Context(), "repository:orders", "workload:orders")
	if err != nil {
		t.Fatalf("fetchCloudResourceResult() error = %v", err)
	}
	if got, want := len(result.rows), 1; got != want {
		t.Fatalf("rows = %#v, want %d", result.rows, want)
	}
	row := result.rows[0]
	if got, want := floatVal(row, "confidence"), 0.95; got != want {
		t.Fatalf("confidence = %v, want %v", got, want)
	}
	if got, want := StringVal(row, "reason"), "selected-reason"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "relationship_basis"), "selected-basis"; got != want {
		t.Fatalf("relationship_basis = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "source_fact_id"), "fact:selected"; got != want {
		t.Fatalf("source_fact_id = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "environment"), "prod"; got != want {
		t.Fatalf("environment = %q, want resource fallback %q", got, want)
	}
}

func TestFetchCloudResourceResultRejectsNonFiniteObservationConfidence(t *testing.T) {
	t.Parallel()

	testCases := map[string]float64{
		"nan":          math.NaN(),
		"positive_inf": math.Inf(1),
		"negative_inf": math.Inf(-1),
	}
	for name, confidence := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{{
					"id": "cloud:orders",
					"observations": []any{
						map[string]any{"confidence": confidence, "source_fact_id": "fact:invalid"},
					},
				}}, nil
			}}
			handler := &ImpactHandler{Neo4j: reader}

			_, err := handler.fetchCloudResourceResult(t.Context(), "repository:orders", "workload:orders")
			if err == nil {
				t.Fatal("fetchCloudResourceResult() error = nil, want non-finite confidence rejection")
			}
			if !strings.Contains(err.Error(), "cloud resource observation") || !strings.Contains(err.Error(), "confidence") {
				t.Fatalf("fetchCloudResourceResult() error = %q, want observation confidence context", err)
			}
		})
	}
}
