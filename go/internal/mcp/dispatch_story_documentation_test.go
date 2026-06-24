// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolServiceStoryPreservesTargetDocumentationReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/payments-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("service_id"), "workload:payments-api"; got != want {
			t.Fatalf("service_id query = %q, want %q", got, want)
		}
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"documentation_overview": map[string]any{
				"target_documentation": map[string]any{
					"finding_count": 1,
					"findings": []map[string]any{{
						"finding_id": "finding:payments-runbook",
						"title":      "Payments API Runbook",
					}},
					"coverage":         map[string]any{"target_fact_count": 1},
					"missing_evidence": []map[string]any{},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_service_story", map[string]any{
		"workload_id": "workload:payments-api",
	})
	targetDocumentation := mcpMapValue(
		mcpMapValue(mcpEnvelopeData(t, result), "documentation_overview"),
		"target_documentation",
	)
	if got, want := query.IntVal(targetDocumentation, "finding_count"), 1; got != want {
		t.Fatalf("target_documentation.finding_count = %d, want %d", got, want)
	}
	if got, want := len(mcpMapSliceValue(targetDocumentation, "findings")), 1; got != want {
		t.Fatalf("len(target_documentation.findings) = %d, want %d", got, want)
	}
	if got := len(mcpMapSliceValue(targetDocumentation, "missing_evidence")); got != 0 {
		t.Fatalf("target_documentation.missing_evidence = %#v, want empty", got)
	}
}

func TestDispatchToolServiceStoryPreservesMissingDocumentationReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/payments-api/story", func(w http.ResponseWriter, _ *http.Request) {
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"documentation_overview": map[string]any{
				"target_documentation": map[string]any{
					"finding_count": 0,
					"findings":      []map[string]any{},
					"related_facts": []map[string]any{{
						"fact_id": "fact:payments-generic-mention",
					}},
					"missing_evidence": []map[string]any{{
						"reason": "documentation_findings_absent",
						"detail": "target documentation facts exist but no admissible documentation finding matched the target scope",
					}},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_service_story", map[string]any{
		"workload_id": "workload:payments-api",
	})
	targetDocumentation := mcpMapValue(
		mcpMapValue(mcpEnvelopeData(t, result), "documentation_overview"),
		"target_documentation",
	)
	if got := len(mcpMapSliceValue(targetDocumentation, "findings")); got != 0 {
		t.Fatalf("len(target_documentation.findings) = %d, want 0", got)
	}
	missing := mcpMapSliceValue(targetDocumentation, "missing_evidence")
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(target_documentation.missing_evidence) = %d, want %d", got, want)
	}
	if got, want := query.StringVal(missing[0], "reason"), "documentation_findings_absent"; got != want {
		t.Fatalf("missing_evidence[0].reason = %q, want %q", got, want)
	}
}

func TestDispatchToolServiceStoryPreservesSourceOnlyDocumentationReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/payments-api/story", func(w http.ResponseWriter, _ *http.Request) {
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"documentation_overview": map[string]any{
				"documentation_file_count": 2,
				"target_documentation": map[string]any{
					"finding_count":      0,
					"findings":           []map[string]any{},
					"related_fact_count": 0,
					"related_facts":      []map[string]any{},
					"coverage": map[string]any{
						"source_only_count": 42,
						"source_only_fact_kinds": map[string]any{
							"documentation_document": 7,
							"documentation_section":  35,
						},
					},
					"missing_evidence": []map[string]any{{
						"reason": "target_link_not_modeled",
						"detail": "external documentation facts exist, but none carry structured refs for the selected target scope",
					}},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_service_story", map[string]any{
		"workload_id": "workload:payments-api",
	})
	overview := mcpMapValue(mcpEnvelopeData(t, result), "documentation_overview")
	if got, want := query.IntVal(overview, "documentation_file_count"), 2; got != want {
		t.Fatalf("documentation_overview.documentation_file_count = %d, want %d", got, want)
	}
	targetDocumentation := mcpMapValue(overview, "target_documentation")
	if got := len(mcpMapSliceValue(targetDocumentation, "findings")); got != 0 {
		t.Fatalf("len(target_documentation.findings) = %d, want 0", got)
	}
	coverage := mcpMapValue(targetDocumentation, "coverage")
	if got, want := query.IntVal(coverage, "source_only_count"), 42; got != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", got, want)
	}
	missing := mcpMapSliceValue(targetDocumentation, "missing_evidence")
	if got, want := query.StringVal(missing[0], "reason"), "target_link_not_modeled"; got != want {
		t.Fatalf("missing_evidence[0].reason = %q, want %q", got, want)
	}
}

func TestDispatchToolRepoStoryPreservesTargetDocumentationReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories/repo-payments-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"documentation_overview": map[string]any{
				"target_documentation": map[string]any{
					"finding_count": 1,
					"findings": []map[string]any{{
						"finding_id": "finding:payments-repo-docs",
						"title":      "Payments Repository Architecture",
					}},
					"coverage": map[string]any{"target_fact_count": 1},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_repo_story", map[string]any{
		"repo_id": "repo-payments-api",
	})
	targetDocumentation := mcpMapValue(
		mcpMapValue(mcpEnvelopeData(t, result), "documentation_overview"),
		"target_documentation",
	)
	if got, want := query.IntVal(targetDocumentation, "finding_count"), 1; got != want {
		t.Fatalf("target_documentation.finding_count = %d, want %d", got, want)
	}
	findings := mcpMapSliceValue(targetDocumentation, "findings")
	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(target_documentation.findings) = %d, want %d", got, want)
	}
	if got, want := query.StringVal(findings[0], "finding_id"), "finding:payments-repo-docs"; got != want {
		t.Fatalf("findings[0].finding_id = %q, want %q", got, want)
	}
}

func TestDispatchToolServiceStoryPreservesTargetSupportReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/payments-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("service_id"), "workload:payments-api"; got != want {
			t.Fatalf("service_id query = %q, want %q", got, want)
		}
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"support_overview": map[string]any{
				"target_support": map[string]any{
					"evidence_count":         2,
					"work_item_count":        1,
					"incident_routing_count": 1,
					"evidence": []map[string]any{{
						"fact_id":   "jira-123",
						"fact_kind": "work_item.record",
					}, {
						"fact_id":   "pd-service",
						"fact_kind": "incident_routing.observed_pagerduty_service",
					}},
					"missing_evidence": []map[string]any{},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_service_story", map[string]any{
		"workload_id": "workload:payments-api",
	})
	targetSupport := mcpMapValue(
		mcpMapValue(mcpEnvelopeData(t, result), "support_overview"),
		"target_support",
	)
	if got, want := query.IntVal(targetSupport, "evidence_count"), 2; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d", got, want)
	}
	if got := len(mcpMapSliceValue(targetSupport, "missing_evidence")); got != 0 {
		t.Fatalf("target_support.missing_evidence = %#v, want empty", got)
	}
}

func TestDispatchToolRepoStoryPreservesTargetSupportReadback(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories/repo-payments-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		writeMCPStoryDocumentationEnvelope(w, map[string]any{
			"support_overview": map[string]any{
				"target_support": map[string]any{
					"evidence_count": 1,
					"evidence": []map[string]any{{
						"fact_id":   "jira-repo-123",
						"fact_kind": "work_item.external_link",
					}},
					"missing_evidence": []map[string]any{},
				},
			},
		})
	})

	result := dispatchMCPStoryDocumentationTool(t, mux, "get_repo_story", map[string]any{
		"repo_id": "repo-payments-api",
	})
	targetSupport := mcpMapValue(
		mcpMapValue(mcpEnvelopeData(t, result), "support_overview"),
		"target_support",
	)
	if got, want := query.IntVal(targetSupport, "evidence_count"), 1; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d", got, want)
	}
}

func dispatchMCPStoryDocumentationTool(
	t *testing.T,
	mux *http.ServeMux,
	toolName string,
	args map[string]any,
) *dispatchResult {
	t.Helper()

	result, err := dispatchTool(
		context.Background(),
		mux,
		toolName,
		args,
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured story envelope")
	}
	return result
}

func writeMCPStoryDocumentationEnvelope(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"truth": map[string]any{
			"level":      "exact",
			"capability": "platform_impact.context_overview",
			"profile":    "production",
			"basis":      "hybrid",
			"freshness":  map[string]any{"state": "fresh"},
		},
		"error": nil,
	})
}

func mcpEnvelopeData(t *testing.T, result *dispatchResult) map[string]any {
	t.Helper()

	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	return data
}

func mcpMapSliceValue(row map[string]any, key string) []map[string]any {
	raw, ok := row[key]
	if !ok {
		return nil
	}
	if rows, ok := raw.([]map[string]any); ok {
		return rows
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if ok {
			rows = append(rows, row)
		}
	}
	return rows
}
