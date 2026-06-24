// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationHandlerExplainsSourceOnlyDocumentationFacts(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFindingsModel: documentationFindingListReadModel{
				Findings:     []map[string]any{},
				RelatedFacts: []map[string]any{},
				Coverage: documentationTargetCoverage{
					Target: documentationTargetScope{
						Repository: "repo-payments-api",
						TargetKind: "repository",
						TargetID:   "repo-payments-api",
					},
					SourceOnlyCount: 42,
					SourceOnlyFactKinds: map[string]int{
						"documentation_document": 7,
						"documentation_section":  35,
					},
				},
				MissingEvidence: []documentationMissingEvidence{{
					Reason: "target_link_not_modeled",
					Detail: "external documentation facts exist, but none carry structured refs for the selected target scope",
				}},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/findings?repo=repo-payments-api&target_kind=repository&target_id=repo-payments-api&limit=5",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got := len(data["findings"].([]any)); got != 0 {
		t.Fatalf("len(findings) = %d, want 0", got)
	}
	if got := len(data["related_facts"].([]any)); got != 0 {
		t.Fatalf("len(related_facts) = %d, want 0", got)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["source_only_count"], float64(42); got != want {
		t.Fatalf("coverage.source_only_count = %#v, want %#v", got, want)
	}
	missing := data["missing_evidence"].([]any)
	if got, want := missing[0].(map[string]any)["reason"], "target_link_not_modeled"; got != want {
		t.Fatalf("missing_evidence[0].reason = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationFindingsReportsSourceOnlyDocumentationFacts(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
		{
			columns: []string{
				"documentation_source_only_count",
				"documentation_source_fact_count",
				"documentation_document_fact_count",
				"documentation_section_fact_count",
				"documentation_link_fact_count",
			},
			rows: [][]driver.Value{{int64(1150), int64(1), int64(99), int64(250), int64(800)}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if gotCount, want := got.Coverage.SourceOnlyCount, 1150; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := got.Coverage.SourceOnlyFactKinds["documentation_link"], 800; gotCount != want {
		t.Fatalf("coverage.source_only_fact_kinds.documentation_link = %d, want %d", gotCount, want)
	}
	if gotReason := got.MissingEvidence[0].Reason; gotReason != "target_link_not_modeled" {
		t.Fatalf("missing_evidence[0].reason = %q, want target_link_not_modeled", gotReason)
	}
	if gotRelated := got.RelatedFacts; len(gotRelated) != 0 {
		t.Fatalf("related_facts = %#v, want no target facts for source-only documentation", gotRelated)
	}
}

func TestBuildStoryTargetDocumentationExplainsSourceOnlyDocumentationFacts(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetDocumentation(documentationFindingFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      documentationStoryReadLimit,
	}, documentationFindingListReadModel{
		Coverage: documentationTargetCoverage{
			Target: documentationTargetScope{
				Repository: "repo-payments-api",
				TargetKind: "repository",
				TargetID:   "repo-payments-api",
			},
			SourceOnlyCount: 3,
			SourceOnlyFactKinds: map[string]int{
				"documentation_document": 1,
				"documentation_section":  2,
			},
		},
	})

	if gotCount := IntVal(got, "finding_count"); gotCount != 0 {
		t.Fatalf("finding_count = %d, want 0 for source-only documentation facts", gotCount)
	}
	if gotCount := IntVal(got, "related_fact_count"); gotCount != 0 {
		t.Fatalf("related_fact_count = %d, want 0 for source-only documentation facts", gotCount)
	}
	coverage := mapValue(got, "coverage")
	if gotCount, want := IntVal(coverage, "source_only_count"), 3; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
	}
	missing := mapSliceValue(got, "missing_evidence")
	if gotReason := StringVal(missing[0], "reason"); gotReason != "target_link_not_modeled" {
		t.Fatalf("missing_evidence[0].reason = %q, want target_link_not_modeled", gotReason)
	}
}

func TestBuildDocumentationSourceOnlySQLStaysAggregateOnly(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationSourceOnlySQL(documentationFindingFilter{
		ScopeID:    "docs-scope",
		SourceID:   "confluence-prod",
		DocumentID: "doc-runbook",
	})

	for _, fragment := range []string{
		"COUNT(*) AS documentation_source_only_count",
		"COUNT(*) FILTER (WHERE fact.fact_kind = 'documentation_document') AS documentation_document_fact_count",
		"fact.fact_kind = ANY($1::text[])",
		"generation.status = 'active'",
		"fact.scope_id = $2",
		"fact.payload->>'source_id' = $3",
		"fact.payload->>'document_id' = $4",
		"jsonb_array_length",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("source-only documentation SQL missing fragment %q:\n%s", fragment, query)
		}
	}
	for _, forbidden := range []string{"fact.payload AS", "source_record_id", "ORDER BY", "LIMIT"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("source-only documentation SQL leaked row-shaped fragment %q:\n%s", forbidden, query)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args len = %d, want fact kind array plus scoped filters", len(args))
	}
}
