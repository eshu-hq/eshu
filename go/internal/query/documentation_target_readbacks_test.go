// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDocumentationHandlerExplainsRepoScopedTargetFactsWithoutFindings(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFindingsModel: documentationFindingListReadModel{
				RelatedFacts: []map[string]any{{
					"fact_id":   "fact:mention:payment-api",
					"fact_kind": "documentation_entity_mention",
					"payload": map[string]any{
						"mention_text": "payment-api",
						"candidate_refs": []any{map[string]any{
							"kind": "service",
							"id":   "service:payment-api",
						}},
					},
				}},
				Coverage: documentationTargetCoverage{
					Target: documentationTargetScope{
						Repository: "repo:platform-api",
						ServiceID:  "service:payment-api",
						TargetKind: "service",
						TargetID:   "service:payment-api",
					},
					FindingsReturned: 0,
					TargetFactCount:  1,
					TargetFactKinds:  map[string]int{"documentation_entity_mention": 1},
				},
				MissingEvidence: []documentationMissingEvidence{{
					Reason: "documentation_findings_absent",
					Detail: "target documentation facts exist but no admissible documentation findings matched the target scope",
				}},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/findings?repo=repo:platform-api&service_id=service:payment-api&limit=5",
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
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["target_fact_count"], float64(1); got != want {
		t.Fatalf("coverage.target_fact_count = %#v, want %#v", got, want)
	}
	relatedFacts := data["related_facts"].([]any)
	if got, want := len(relatedFacts), 1; got != want {
		t.Fatalf("len(related_facts) = %d, want %d", got, want)
	}
	missing := data["missing_evidence"].([]any)
	if got, want := missing[0].(map[string]any)["reason"], "documentation_findings_absent"; got != want {
		t.Fatalf("missing_evidence[0].reason = %#v, want %#v", got, want)
	}
}

func TestDocumentationHandlerPassesTargetScopeFilters(t *testing.T) {
	t.Parallel()

	var captured documentationFindingFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{documentationFindingsFilter: &captured},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/findings?repo=repo:platform-api&target_kind=service&target_id=service:payment-api&service_id=service:payment-api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repo:platform-api"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := captured.TargetKind, "service"; got != want {
		t.Fatalf("TargetKind = %q, want %q", got, want)
	}
	if got, want := captured.TargetID, "service:payment-api"; got != want {
		t.Fatalf("TargetID = %q, want %q", got, want)
	}
	if got, want := captured.ServiceID, "service:payment-api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
}

func TestDocumentationFactsRequestAcceptsTargetScopeAsAnchor(t *testing.T) {
	t.Parallel()

	var captured documentationFactFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{documentationFactsFilter: &captured},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?fact_kind=entity_mention&repo=repo:platform-api&service_id=service:payment-api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repo:platform-api"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := captured.ServiceID, "service:payment-api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
}

func TestBuildDocumentationFindingsSQLMatchesSourceOrTargetRepo(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFindingsSQL(documentationFindingFilter{
		Repository: "repo:platform-api",
		ServiceID:  "service:payment-api",
		Limit:      50,
	})

	for _, fragment := range []string{
		"ingestion_scopes.payload->>'repo' = $1",
		"fact_records.payload @>",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("documentation findings SQL missing fragment %q: %s", fragment, query)
		}
	}
	if got, want := args[0], "repo:platform-api"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
	joinedArgs := documentationArgsString(args)
	for _, fragment := range []string{"linked_entities", "evidence_refs", "service:payment-api", "repo:platform-api"} {
		if !strings.Contains(joinedArgs, fragment) {
			t.Fatalf("documentation findings SQL args missing fragment %q: %#v", fragment, args)
		}
	}
}

func TestBuildDocumentationTargetFactsSQLIsTargetScopedAndBounded(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationTargetFactsSQL(documentationFindingFilter{
		ScopeID:      "docs-scope",
		GenerationID: "docs-generation",
		Repository:   "repo:platform-api",
		ServiceID:    "service:payment-api",
		SourceID:     "source:runbook",
		DocumentID:   "doc:payment",
		Limit:        50,
	})

	for _, fragment := range []string{
		"fact_records.fact_kind IN ('documentation_entity_mention', 'documentation_claim_candidate', 'semantic.documentation_observation')",
		"fact_records.scope_id = $",
		"fact_records.generation_id = $",
		"fact_records.payload->>'source_id' = $",
		"fact_records.payload->>'document_id' = $",
		"fact_records.payload @>",
		"ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC",
		"LIMIT $",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("documentation target facts SQL missing fragment %q: %s", fragment, query)
		}
	}
	if len(args) < 3 {
		t.Fatalf("args = %#v, want target predicates plus limit", args)
	}
	joinedArgs := documentationArgsString(args)
	for _, fragment := range []string{"docs-scope", "docs-generation", "source:runbook", "doc:payment"} {
		if !strings.Contains(joinedArgs, fragment) {
			t.Fatalf("documentation target facts SQL args missing fragment %q: %#v", fragment, args)
		}
	}
}

func TestBuildDocumentationTargetFactsSQLIncludesSemanticObservationProvenance(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationTargetFactsSQL(documentationFindingFilter{
		Repository: "repo:payments",
		TargetKind: "service",
		TargetID:   "service:payments-api",
		Limit:      5,
	})

	for _, want := range []string{
		facts.DocumentationEntityMentionFactKind,
		facts.DocumentationClaimCandidateFactKind,
		facts.SemanticDocumentationObservationFactKind,
	} {
		if !strings.Contains(query, "'"+want+"'") {
			t.Fatalf("documentation target facts SQL missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, facts.SemanticCodeHintFactKind) {
		t.Fatalf("documentation target facts SQL included code hints, want documentation provenance only:\n%s", query)
	}
}

func TestBuildStoryTargetDocumentationKeepsSemanticObservationProvenanceOnly(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetDocumentation(documentationFindingFilter{
		Repository: "repo:payments",
		TargetKind: "service",
		TargetID:   "service:payments-api",
		Limit:      documentationStoryReadLimit,
	}, documentationFindingListReadModel{
		RelatedFacts: []map[string]any{{
			"fact_id":   "fact:semantic-doc-observation",
			"fact_kind": facts.SemanticDocumentationObservationFactKind,
			"payload": map[string]any{
				"observation_type": "runbook_step",
				"admission_state":  facts.SemanticAdmissionPartial,
				"freshness_state":  facts.SemanticFreshnessFresh,
				"provider": map[string]any{
					"provider_profile_id": "semantic-docs-test",
				},
				"evidence_refs": []any{map[string]any{
					"kind": "service",
					"id":   "service:payments-api",
				}},
			},
		}},
		Coverage: documentationTargetCoverage{
			Target: documentationTargetScope{
				Repository: "repo:payments",
				TargetKind: "service",
				TargetID:   "service:payments-api",
			},
			FindingsReturned: 0,
			TargetFactCount:  1,
			TargetFactKinds:  map[string]int{facts.SemanticDocumentationObservationFactKind: 1},
		},
	})

	if gotCount := IntVal(got, "finding_count"); gotCount != 0 {
		t.Fatalf("finding_count = %d, want 0 for semantic observations before finding admission", gotCount)
	}
	if gotCount := IntVal(got, "related_fact_count"); gotCount != 1 {
		t.Fatalf("related_fact_count = %d, want one semantic observation fact", gotCount)
	}
	coverage := mapValue(got, "coverage")
	kinds, ok := coverage["target_fact_kinds"].(map[string]int)
	if !ok {
		t.Fatalf("target_fact_kinds type = %T, want map[string]int", coverage["target_fact_kinds"])
	}
	if gotCount := kinds[facts.SemanticDocumentationObservationFactKind]; gotCount != 1 {
		t.Fatalf("semantic observation target_fact_kinds count = %d, want 1", gotCount)
	}
	if gotSummary, want := storyTargetDocumentationSummary(got), "External documentation has 1 target-related fact(s) but no admitted finding for this target."; gotSummary != want {
		t.Fatalf("story summary = %q, want %q", gotSummary, want)
	}
}

func TestDocumentationTargetRefsDoNotCrossPairRepoOrServiceIDs(t *testing.T) {
	t.Parallel()

	refs := documentationTargetRefsFromFindingFilter(documentationFindingFilter{
		Repository: "repo:platform-api",
		TargetKind: "service",
	})

	if got, want := len(refs), 1; got != want {
		t.Fatalf("len(refs) = %d, want %d: %#v", got, want, refs)
	}
	if got, want := refs[0], (documentationTargetRef{kind: "repository", id: "repo:platform-api"}); got != want {
		t.Fatalf("refs[0] = %#v, want %#v", got, want)
	}

	scope := documentationTargetScopeFromFindingFilter(documentationFindingFilter{
		TargetKind: "workload",
		ServiceID:  "service:payment-api",
	})
	if got, want := scope.TargetKind, "service"; got != want {
		t.Fatalf("TargetKind = %q, want %q", got, want)
	}
	if got, want := scope.TargetID, "service:payment-api"; got != want {
		t.Fatalf("TargetID = %q, want %q", got, want)
	}
}

func TestDocumentationTargetScopeDropsKindWithoutCanonicalTargetID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     documentationFindingFilter
		targetKind string
		targetID   string
	}{
		{
			name: "bare target kind",
			filter: documentationFindingFilter{
				TargetKind: "service",
			},
		},
		{
			name: "repo with mismatched bare target kind",
			filter: documentationFindingFilter{
				Repository: "repo:platform-api",
				TargetKind: "service",
			},
		},
		{
			name: "repo target kind",
			filter: documentationFindingFilter{
				Repository: "repo:platform-api",
				TargetKind: "repository",
			},
			targetKind: "repository",
			targetID:   "repo:platform-api",
		},
		{
			name: "explicit target id",
			filter: documentationFindingFilter{
				TargetKind: "workload",
				TargetID:   "workload:payments",
			},
			targetKind: "workload",
			targetID:   "workload:payments",
		},
		{
			name: "service id",
			filter: documentationFindingFilter{
				TargetKind: "workload",
				ServiceID:  "service:payment-api",
			},
			targetKind: "service",
			targetID:   "service:payment-api",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := documentationTargetScopeFromFindingFilter(tt.filter)
			if got := scope.TargetKind; got != tt.targetKind {
				t.Fatalf("TargetKind = %q, want %q", got, tt.targetKind)
			}
			if got := scope.TargetID; got != tt.targetID {
				t.Fatalf("TargetID = %q, want %q", got, tt.targetID)
			}
		})
	}
}

func TestDocumentationTargetRefsPreferExplicitServiceOverRepoFallback(t *testing.T) {
	t.Parallel()

	refs := documentationTargetRefsFromFindingFilter(documentationFindingFilter{
		Repository: "repo:platform-api",
		ServiceID:  "service:payment-api",
	})

	if got, want := refs, []documentationTargetRef{{kind: "service", id: "service:payment-api"}}; !equalDocumentationTargetRefs(got, want) {
		t.Fatalf("refs = %#v, want %#v", got, want)
	}
}

func TestDocumentationCoverageReportsMissingWhenRepoFindingDoesNotMatchTarget(t *testing.T) {
	t.Parallel()

	filter := documentationFindingFilter{
		Repository: "repo:platform-api",
		ServiceID:  "service:payment-api",
	}
	findings := []map[string]any{{
		"finding_id":   "finding:unrelated-repo-doc",
		"finding_type": "runbook_gap",
	}}
	relatedFacts := []map[string]any{{
		"fact_kind": "documentation_entity_mention",
		"payload": map[string]any{
			"candidate_refs": []any{map[string]any{
				"kind": "service",
				"id":   "service:payment-api",
			}},
		},
	}}

	coverage := documentationTargetCoverageFromFacts(filter, findings, relatedFacts, false)
	missing := documentationMissingEvidenceForTarget(coverage)

	if got, want := coverage.FindingsReturned, 0; got != want {
		t.Fatalf("coverage.FindingsReturned = %d, want %d", got, want)
	}
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(missing) = %d, want %d", got, want)
	}
	if got, want := missing[0].Reason, "documentation_findings_absent"; got != want {
		t.Fatalf("missing[0].Reason = %q, want %q", got, want)
	}
}

func TestOpenAPISpecIncludesDocumentationTargetReadbacks(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	findings := paths["/api/v0/documentation/findings"].(map[string]any)["get"].(map[string]any)
	parameters := findings["parameters"].([]any)
	for _, name := range []string{"target_kind", "target_id", "service_id"} {
		if !openAPIParametersInclude(parameters, name) {
			t.Fatalf("documentation findings OpenAPI parameters missing %q", name)
		}
	}
	properties := findings["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"coverage", "related_facts", "missing_evidence"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("documentation findings OpenAPI response missing %q", name)
		}
	}
	facts := paths["/api/v0/documentation/facts"].(map[string]any)["get"].(map[string]any)
	for _, name := range []string{"repo", "target_kind", "target_id", "service_id"} {
		if !openAPIParametersInclude(facts["parameters"].([]any), name) {
			t.Fatalf("documentation facts OpenAPI parameters missing %q", name)
		}
	}
}

func documentationArgsString(args []any) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if value, ok := arg.(string); ok {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func equalDocumentationTargetRefs(got, want []documentationTargetRef) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func openAPIParametersInclude(parameters []any, want string) bool {
	for _, parameter := range parameters {
		row := parameter.(map[string]any)
		if row["name"] == want {
			return true
		}
	}
	return false
}
