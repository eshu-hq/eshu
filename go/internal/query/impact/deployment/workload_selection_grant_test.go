// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestResolveWorkloadSelectorIDDenialThenNameAdmitCountsNoDenial covers the
// #6786 review finding on the selector's id-then-name fallback: the id lookup
// hits a workload the caller has no grant to, the name lookup hits one it
// does. The request resolves, so counting a grant_denied would report a
// denial the caller never saw.
func TestResolveWorkloadSelectorIDDenialThenNameAdmitCountsNoDenial(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		graph.AssertCypherHasNoBrokenAndOr(t, cypher)
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return []map[string]any{{"id": "api", "name": "legacy", "repo_id": "repo-b", "defining": []string{}}}, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			return []map[string]any{{"id": "workload:api", "name": "api", "repo_id": "repo-a", "defining": []string{}}}, nil
		default:
			return nil, nil
		}
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "api", nil, instruments)
	if err != nil || got != "workload:api" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want workload:api", got, err)
	}
	if points := scopedGrantDeniedDataPoints(t, reader); len(points) != 0 {
		t.Fatalf("%s data points = %+v, want none when the name lookup admitted a workload", queryScopedGrantDeniedMetric, points)
	}
}

// TestResolveWorkloadSelectorDeniedByBothLookupsCountsOneDenial pins the
// other side: when neither lookup admits a row, the request is one denial.
func TestResolveWorkloadSelectorDeniedByBothLookupsCountsOneDenial(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return []map[string]any{{"id": "api", "name": "legacy", "repo_id": "repo-b", "defining": []string{}}}, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			return []map[string]any{{"id": "workload:api", "name": "api", "repo_id": "repo-c", "defining": []string{}}}, nil
		default:
			return nil, nil
		}
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "api", nil, instruments)
	if err != nil || got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want not-found", got, err)
	}
	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 || points[0].Value != 1 {
		t.Fatalf("%s data points = %+v, want exactly one denial", queryScopedGrantDeniedMetric, points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "grant_denied"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestResolveWorkloadSelectorOverflowReturnsTypedErrorWithoutCount covers
// the #6786 review finding that the overflow error leaked the raw row count.
// The rows are counted before the grant filter, so the count told a scoped
// caller how many ungranted workloads share a name.
func TestResolveWorkloadSelectorOverflowReturnsTypedErrorWithoutCount(t *testing.T) {
	t.Parallel()

	overBound := make([]map[string]any, querycontract.WorkloadSelectorCandidateBound+1)
	for i := range overBound {
		overBound[i] = map[string]any{"id": "workload:orders", "name": "orders", "repo_id": "repo-b", "defining": []string{}}
	}
	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		return overBound, nil
	}}

	_, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "orders", nil, nil)
	if !errors.Is(err, querycontract.ErrWorkloadSelectorCandidatesExceedBound) {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want querycontract.ErrWorkloadSelectorCandidatesExceedBound", err)
	}
	if strings.ContainsAny(err.Error(), "0123456789") {
		t.Fatalf("ResolveWorkloadSelector() error = %q, want no row count in the message", err.Error())
	}
}

// TestResolveWorkloadSelectorAmbiguityUsesDistinctIDs covers the #6786
// review finding that ambiguity compared only the first two admitted rows. A
// backend can return one workload twice; the second distinct id then sits
// at position three and was missed.
func TestResolveWorkloadSelectorAmbiguityUsesDistinctIDs(t *testing.T) {
	t.Parallel()

	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		return []map[string]any{
			{"id": "workload:orders-a", "name": "orders", "repo_id": "repo-a", "defining": []string{}},
			{"id": "workload:orders-a", "name": "orders", "repo_id": "repo-a", "defining": []string{}},
			{"id": "workload:orders-b", "name": "orders", "repo_id": "repo-a", "defining": []string{}},
		}, nil
	}}

	_, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "orders", nil, nil)
	if !errors.Is(err, ErrAmbiguousWorkloadSelector) {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want ambiguity across distinct ids", err)
	}
}

// TestResolveWorkloadSelectorDuplicateRowsForOneIDResolve pins that
// duplicate rows for a single workload id are not ambiguous.
func TestResolveWorkloadSelectorDuplicateRowsForOneIDResolve(t *testing.T) {
	t.Parallel()

	graph := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		return []map[string]any{
			{"id": "workload:orders-a", "name": "orders", "repo_id": "repo-a", "defining": []string{}},
			{"id": "workload:orders-a", "name": "orders", "repo_id": "repo-a", "defining": []string{}},
			{"id": "workload:orders-b", "name": "orders", "repo_id": "repo-b", "defining": []string{}},
		}, nil
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "orders", nil, nil)
	if err != nil || got != "workload:orders-a" {
		t.Fatalf("ResolveWorkloadSelector() = %q, %v, want workload:orders-a", got, err)
	}
}
