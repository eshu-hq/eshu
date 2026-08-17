// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import (
	"strings"
	"testing"
)

// TestRenderServiceSummaryWritesTheOperatorView pins the whole rendering byte
// for byte. The command's output is what operators and their scripts read, so a
// reordered or reworded line is a behavior change, not a formatting one.
func TestRenderServiceSummaryWritesTheOperatorView(t *testing.T) {
	t.Parallel()

	envelope := ServiceEnvelope{
		Truth: map[string]any{
			"freshness": map[string]any{"state": "fresh", "detail": "generation 42 active"},
		},
		Data: map[string]any{
			"service_identity": map[string]any{
				"service_name":           "checkout",
				"repo_id":                "repo-1",
				"repo_name":              "eshu-hq/checkout",
				"materialization_status": "materialized",
				"query_basis":            "graph",
				"limitations":            []any{"cloud evidence is 3 days old"},
			},
			"code_to_runtime_trace": map[string]any{
				"status": "complete",
				"segments": []any{
					map[string]any{"name": "source", "status": "complete", "evidence_count": float64(3), "basis": "graph"},
					map[string]any{"name": "image", "status": "complete"},
				},
			},
			"deployment_lanes":      []any{map[string]any{"name": "prod"}},
			"runtime_instances":     []any{map[string]any{"id": "i-1"}, map[string]any{"id": "i-2"}},
			"upstream_dependencies": []any{map[string]any{"name": "postgres"}},
			"downstream_consumers":  []any{map[string]any{"name": "web"}},
			"investigation": map[string]any{
				"coverage_summary": map[string]any{"state": "complete", "reason": "all segments resolved"},
			},
		},
	}

	want := strings.Join([]string{
		"Service: checkout",
		"Repository: repo-1 (eshu-hq/checkout)",
		"Materialization: materialized",
		"Basis: graph",
		"Truth freshness: fresh",
		"Freshness detail: generation 42 active",
		"Code to runtime:",
		"Trace status: complete",
		"- source: complete (3 evidence) via graph",
		"- image: complete",
		"Deployment lanes: 1",
		"Runtime instances: 2",
		"Upstream dependencies: 1",
		"Downstream consumers: 1",
		"Coverage: complete",
		"Coverage reason: all segments resolved",
		"What to worry about:",
		"- cloud evidence is 3 days old",
		"",
	}, "\n")

	var out strings.Builder
	if err := RenderServiceSummary(&out, envelope); err != nil {
		t.Fatalf("RenderServiceSummary returned %v, want nil", err)
	}
	if out.String() != want {
		t.Fatalf("rendered summary mismatch\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestRenderServiceSummaryFallsBackWhenIdentityIsEmpty pins what an operator
// sees when the API returned a service it could not name: the two leading lines
// still print with <unknown>, the optional lines are omitted entirely, and
// coverage reports "unknown" rather than an empty value.
func TestRenderServiceSummaryFallsBackWhenIdentityIsEmpty(t *testing.T) {
	t.Parallel()

	want := strings.Join([]string{
		"Service: <unknown>",
		"Repository: <unknown> (<unknown>)",
		"Deployment lanes: 0",
		"Runtime instances: 0",
		"Upstream dependencies: 0",
		"Downstream consumers: 0",
		"Coverage: unknown",
		"",
	}, "\n")

	var out strings.Builder
	if err := RenderServiceSummary(&out, ServiceEnvelope{Data: map[string]any{}}); err != nil {
		t.Fatalf("RenderServiceSummary returned %v, want nil", err)
	}
	if out.String() != want {
		t.Fatalf("rendered summary mismatch\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestRenderServiceSummaryReadsTopLevelLimitations pins the fallback: the API
// has attached limitations to the identity block and to data, and an operator
// must see them either way.
func TestRenderServiceSummaryReadsTopLevelLimitations(t *testing.T) {
	t.Parallel()

	envelope := ServiceEnvelope{Data: map[string]any{
		"limitations": []any{"runtime evidence missing"},
	}}
	var out strings.Builder
	if err := RenderServiceSummary(&out, envelope); err != nil {
		t.Fatalf("RenderServiceSummary returned %v, want nil", err)
	}
	if !strings.Contains(out.String(), "What to worry about:\n- runtime evidence missing\n") {
		t.Fatalf("top-level limitations were not rendered:\n%s", out.String())
	}
}

// TestRenderCodeToRuntimeOmitsAnEmptySection pins that a trace with no segments
// renders nothing at all, header included. An empty "Code to runtime:" heading
// would read as "we looked and found a path", which is the opposite of true.
func TestRenderCodeToRuntimeOmitsAnEmptySection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{name: "no trace block", data: map[string]any{}},
		{name: "trace without segments", data: map[string]any{"code_to_runtime_trace": map[string]any{"status": "partial"}}},
		{name: "empty segment list", data: map[string]any{"code_to_runtime_trace": map[string]any{"segments": []any{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			if err := renderCodeToRuntime(&out, tc.data); err != nil {
				t.Fatalf("renderCodeToRuntime returned %v, want nil", err)
			}
			if out.String() != "" {
				t.Fatalf("rendered %q, want nothing", out.String())
			}
		})
	}
}

// TestRenderCodeToRuntimeSkipsUnnamedSegmentsAndListsMissing pins two rules: a
// segment missing a name or a status is skipped rather than rendered half
// blank, and the missing-segment list is reported so the operator knows what
// evidence the trace never found.
func TestRenderCodeToRuntimeSkipsUnnamedSegmentsAndListsMissing(t *testing.T) {
	t.Parallel()

	data := map[string]any{"code_to_runtime_trace": map[string]any{
		"segments": []any{
			map[string]any{"name": "source", "status": "complete"},
			map[string]any{"name": "", "status": "complete"},
			map[string]any{"name": "image", "status": ""},
			"not an object",
		},
		"missing_segments": []any{"deploy", "runtime"},
	}}

	want := strings.Join([]string{
		"Code to runtime:",
		"- source: complete",
		"Missing evidence: deploy, runtime",
		"",
	}, "\n")

	var out strings.Builder
	if err := renderCodeToRuntime(&out, data); err != nil {
		t.Fatalf("renderCodeToRuntime returned %v, want nil", err)
	}
	if out.String() != want {
		t.Fatalf("rendered mismatch\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestRenderServiceErrorListsAmbiguousCandidates pins the disambiguation view,
// including that a candidate whose name repeats its id does not print the name
// twice.
func TestRenderServiceErrorListsAmbiguousCandidates(t *testing.T) {
	t.Parallel()

	envelope := ServiceEnvelope{Error: &ServiceError{
		Code: "ambiguous",
		Details: map[string]any{"candidates": []any{
			map[string]any{"service_id": "svc-1", "service_name": "checkout", "repo_id": "repo-1", "environment": "prod"},
			map[string]any{"id": "svc-2"},
			map[string]any{"service_id": "svc-3", "service_name": "svc-3"},
		}},
	}}

	want := strings.Join([]string{
		"Service selector is ambiguous. Add --service-id, --repo, or --env.",
		"- svc-1 name=checkout repo=repo-1 env=prod",
		"- svc-2",
		"- svc-3",
		"",
	}, "\n")

	var out strings.Builder
	if err := RenderServiceError(&out, envelope); err != nil {
		t.Fatalf("RenderServiceError returned %v, want nil", err)
	}
	if out.String() != want {
		t.Fatalf("rendered mismatch\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestRenderServiceErrorFallsBackToDataCandidates pins the second location the
// API has returned candidates from.
func TestRenderServiceErrorFallsBackToDataCandidates(t *testing.T) {
	t.Parallel()

	envelope := ServiceEnvelope{
		Data:  map[string]any{"candidates": []any{map[string]any{"service_id": "svc-9"}}},
		Error: &ServiceError{Code: "ambiguous"},
	}
	var out strings.Builder
	if err := RenderServiceError(&out, envelope); err != nil {
		t.Fatalf("RenderServiceError returned %v, want nil", err)
	}
	if !strings.Contains(out.String(), "- svc-9\n") {
		t.Fatalf("data candidates were not rendered:\n%s", out.String())
	}
}

// TestRenderServiceErrorIgnoresOtherCodes pins that only "ambiguous" renders
// here. go/cmd/eshu prints every other failure from the envelope message alone,
// so writing a header for them would duplicate the operator's error line.
func TestRenderServiceErrorIgnoresOtherCodes(t *testing.T) {
	t.Parallel()

	for _, envelope := range []ServiceEnvelope{
		{Error: nil},
		{Error: &ServiceError{Code: "not_found", Message: "no such service"}},
	} {
		var out strings.Builder
		if err := RenderServiceError(&out, envelope); err != nil {
			t.Fatalf("RenderServiceError returned %v, want nil", err)
		}
		if out.String() != "" {
			t.Fatalf("rendered %q, want nothing", out.String())
		}
	}
}

// TestRuntimeInstanceCountFallsBackToIdentityInstances pins the older API shape,
// which nested instances under service_identity.
func TestRuntimeInstanceCountFallsBackToIdentityInstances(t *testing.T) {
	t.Parallel()

	top := map[string]any{"runtime_instances": []any{1, 2, 3}}
	if got := runtimeInstanceCount(top); got != 3 {
		t.Fatalf("runtimeInstanceCount = %d, want 3", got)
	}
	nested := map[string]any{"service_identity": map[string]any{"instances": []any{1, 2}}}
	if got := runtimeInstanceCount(nested); got != 2 {
		t.Fatalf("runtimeInstanceCount fallback = %d, want 2", got)
	}
	if got := runtimeInstanceCount(map[string]any{}); got != 0 {
		t.Fatalf("runtimeInstanceCount of empty data = %d, want 0", got)
	}
}

// TestDownstreamConsumerCountCoversEveryShape pins all four arms. The object
// form is the one that bites: summing its two counts and falling back to items
// is what keeps a service with consumers from reporting zero.
func TestDownstreamConsumerCountCoversEveryShape(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data map[string]any
		want int
	}{
		{name: "list", data: map[string]any{"downstream_consumers": []any{1, 2}}, want: 2},
		{name: "typed list", data: map[string]any{"downstream_consumers": []map[string]any{{"a": 1}}}, want: 1},
		{
			name: "object with counts",
			data: map[string]any{"downstream_consumers": map[string]any{
				"graph_dependent_count":  float64(2),
				"content_consumer_count": float64(3),
			}},
			want: 5,
		},
		{
			name: "object without counts falls back to items",
			data: map[string]any{"downstream_consumers": map[string]any{"items": []any{1, 2, 3, 4}}},
			want: 4,
		},
		{name: "absent", data: map[string]any{}, want: 0},
		{name: "unexpected type", data: map[string]any{"downstream_consumers": "seven"}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := downstreamConsumerCount(tc.data); got != tc.want {
				t.Fatalf("downstreamConsumerCount = %d, want %d", got, tc.want)
			}
		})
	}
}
