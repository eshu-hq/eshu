// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetServiceStorySurfacesTargetLinkedExternalDocumentation(t *testing.T) {
	t.Parallel()

	var captured documentationFindingFilter
	handler := &EntityHandler{
		Neo4j: serviceStoryExternalDocsGraphReader{
			t:      t,
			repoID: "repo-payments-api",
		},
		Content: fakePortContentStore{
			documentationFindingsFilter: &captured,
			documentationFindingsModel:  targetLinkedDocumentationReadModel(),
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/services/payments-api/story?service_id=workload%3Apayments-api",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "payments-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repo-payments-api"; got != want {
		t.Fatalf("documentation filter Repository = %q, want %q", got, want)
	}
	if got, want := captured.ServiceID, "workload:payments-api"; got != want {
		t.Fatalf("documentation filter ServiceID = %q, want %q", got, want)
	}

	data := serviceStoryEnvelopeData(t, w.Body.Bytes())
	overview := mapValue(data, "documentation_overview")
	targetDocumentation := mapValue(overview, "target_documentation")
	findings := mapSliceValue(targetDocumentation, "findings")
	if got, want := len(findings), 1; got != want {
		t.Fatalf("len(target_documentation.findings) = %d, want %d: %#v", got, want, targetDocumentation)
	}
	if got, want := StringVal(findings[0], "finding_id"), "finding:payments-runbook"; got != want {
		t.Fatalf("finding_id = %q, want %q", got, want)
	}
	coverage := mapValue(targetDocumentation, "coverage")
	if got, want := IntVal(coverage, "target_fact_count"), 1; got != want {
		t.Fatalf("coverage.target_fact_count = %d, want %d", got, want)
	}
	if got := mapSliceValue(targetDocumentation, "missing_evidence"); len(got) != 0 {
		t.Fatalf("target_documentation.missing_evidence = %#v, want empty for proven target docs", got)
	}
}

func TestGetRepositoryStorySurfacesTargetLinkedExternalDocumentation(t *testing.T) {
	t.Parallel()

	var captured documentationFindingFilter
	handler := &RepositoryHandler{
		Neo4j: repositoryStoryExternalDocsGraphReader{t: t, repoID: "repo-payments-api"},
		Content: fakePortContentStore{
			documentationFindingsFilter: &captured,
			documentationFindingsModel:  repositoryLinkedDocumentationReadModel(),
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-payments-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("repo_id", "repo-payments-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repo-payments-api"; got != want {
		t.Fatalf("documentation filter Repository = %q, want %q", got, want)
	}
	if got, want := captured.TargetKind, "repository"; got != want {
		t.Fatalf("documentation filter TargetKind = %q, want %q", got, want)
	}
	if got, want := captured.TargetID, "repo-payments-api"; got != want {
		t.Fatalf("documentation filter TargetID = %q, want %q", got, want)
	}

	data := serviceStoryEnvelopeData(t, w.Body.Bytes())
	overview := mapValue(data, "documentation_overview")
	targetDocumentation := mapValue(overview, "target_documentation")
	if got, want := IntVal(targetDocumentation, "finding_count"), 1; got != want {
		t.Fatalf("target_documentation.finding_count = %d, want %d", got, want)
	}
	if got, want := len(mapSliceValue(targetDocumentation, "findings")), 1; got != want {
		t.Fatalf("len(target_documentation.findings) = %d, want %d", got, want)
	}
}

func TestGetServiceStoryPreservesMissingExternalDocumentationCorrelation(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: serviceStoryExternalDocsGraphReader{
			t:      t,
			repoID: "repo-payments-api",
		},
		Content: fakePortContentStore{
			documentationFindingsModel: missingCorrelationDocumentationReadModel(),
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/services/payments-api/story?service_id=workload%3Apayments-api",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "payments-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	data := serviceStoryEnvelopeData(t, w.Body.Bytes())
	targetDocumentation := mapValue(mapValue(data, "documentation_overview"), "target_documentation")
	if got := len(mapSliceValue(targetDocumentation, "findings")); got != 0 {
		t.Fatalf("len(target_documentation.findings) = %d, want 0", got)
	}
	missing := mapSliceValue(targetDocumentation, "missing_evidence")
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(target_documentation.missing_evidence) = %d, want %d", got, want)
	}
	if got, want := StringVal(missing[0], "reason"), "documentation_findings_absent"; got != want {
		t.Fatalf("missing_evidence[0].reason = %q, want %q", got, want)
	}
	if got, want := len(mapSliceValue(targetDocumentation, "related_facts")), 1; got != want {
		t.Fatalf("len(target_documentation.related_facts) = %d, want %d", got, want)
	}
}

func TestDocumentationPayloadDoesNotMatchGenericMentionWithoutTargetRef(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"mention_text": "payments-api runbook",
		"mention_kind": "service_like_text",
	}
	if documentationPayloadMatchesTargetRef(payload, documentationTargetRef{kind: "service", id: "workload:payments-api"}) {
		t.Fatalf("generic documentation mention matched target without candidate_refs, evidence_refs, or linked_entities")
	}
}

type serviceStoryExternalDocsGraphReader struct {
	t      *testing.T
	repoID string
}

func (g serviceStoryExternalDocsGraphReader) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	switch {
	case strings.Contains(cypher, "w.id = $service_id"):
		if got, want := params["service_id"], "workload:payments-api"; got != want {
			g.t.Fatalf("params[service_id] = %#v, want %#v", got, want)
		}
		return []map[string]any{{
			"id":      "workload:payments-api",
			"name":    "payments-api",
			"kind":    "service",
			"repo_id": g.repoID,
		}}, nil
	case strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})"):
		return []map[string]any{}, nil
	default:
		return []map[string]any{}, nil
	}
}

func (g serviceStoryExternalDocsGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	switch {
	case strings.Contains(cypher, "w.id = $workload_id"):
		if got, want := params["workload_id"], "workload:payments-api"; got != want {
			g.t.Fatalf("params[workload_id] = %#v, want %#v", got, want)
		}
		return map[string]any{
			"id":      "workload:payments-api",
			"name":    "payments-api",
			"kind":    "service",
			"repo_id": g.repoID,
		}, nil
	case strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})"):
		if got, want := params["repo_id"], g.repoID; got != want {
			g.t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
		}
		return map[string]any{
			"id":         g.repoID,
			"name":       "payments-api",
			"path":       "/repos/payments-api",
			"local_path": "/repos/payments-api",
			"repo_slug":  "example/payments-api",
			"has_remote": true,
		}, nil
	default:
		return nil, nil
	}
}

type repositoryStoryExternalDocsGraphReader struct {
	t      *testing.T
	repoID string
}

func (g repositoryStoryExternalDocsGraphReader) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	if got := params["repo_id"]; got != g.repoID {
		g.t.Fatalf("params[repo_id] = %#v, want %#v", got, g.repoID)
	}
	switch {
	case strings.Contains(cypher, "RETURN count(DISTINCT f) AS count"):
		return []map[string]any{{"count": int64(9)}}, nil
	case strings.Contains(cypher, "RETURN f.language AS language"):
		return []map[string]any{{"language": "go", "file_count": int64(9)}}, nil
	case strings.Contains(cypher, "RETURN w.name AS workload_name"):
		return []map[string]any{{"workload_name": "payments-api"}}, nil
	case strings.Contains(cypher, "RETURN p.type AS platform_type"):
		return []map[string]any{}, nil
	case strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count"):
		return []map[string]any{{"count": int64(0)}}, nil
	default:
		return []map[string]any{}, nil
	}
}

func (g repositoryStoryExternalDocsGraphReader) RunSingle(
	_ context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		g.t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
	}
	if got := params["repo_id"]; got != g.repoID {
		g.t.Fatalf("params[repo_id] = %#v, want %#v", got, g.repoID)
	}
	return map[string]any{
		"id":         g.repoID,
		"name":       "payments-api",
		"path":       "/repos/payments-api",
		"local_path": "/repos/payments-api",
		"repo_slug":  "example/payments-api",
		"has_remote": true,
	}, nil
}

func targetLinkedDocumentationReadModel() documentationFindingListReadModel {
	return documentationFindingListReadModel{
		Findings: []map[string]any{{
			"finding_id":   "finding:payments-runbook",
			"finding_type": "runbook",
			"source_id":    "source:docs",
			"document_id":  "doc:payments-runbook",
			"title":        "Payments API Runbook",
			"linked_entities": []any{map[string]any{
				"entity_type": "service",
				"entity_id":   "workload:payments-api",
			}},
		}},
		RelatedFacts: []map[string]any{{
			"fact_id":   "fact:payments-mention",
			"fact_kind": "documentation_entity_mention",
			"payload": map[string]any{
				"document_id":  "doc:payments-runbook",
				"mention_text": "https://payments-api.example.test/healthz",
				"candidate_refs": []any{map[string]any{
					"kind": "service",
					"id":   "workload:payments-api",
				}},
			},
		}},
		Coverage: documentationTargetCoverage{
			Target: documentationTargetScope{
				Repository: "repo-payments-api",
				TargetKind: "service",
				TargetID:   "workload:payments-api",
				ServiceID:  "workload:payments-api",
			},
			FindingsReturned: 1,
			TargetFactCount:  1,
			TargetFactKinds:  map[string]int{"documentation_entity_mention": 1},
		},
	}
}

func repositoryLinkedDocumentationReadModel() documentationFindingListReadModel {
	return documentationFindingListReadModel{
		Findings: []map[string]any{{
			"finding_id":   "finding:payments-repo-docs",
			"finding_type": "architecture",
			"source_id":    "source:docs",
			"document_id":  "doc:payments-architecture",
			"title":        "Payments Repository Architecture",
			"linked_entities": []any{map[string]any{
				"entity_type": "repository",
				"entity_id":   "repo-payments-api",
			}},
		}},
		RelatedFacts: []map[string]any{{
			"fact_id":   "fact:payments-repo-mention",
			"fact_kind": "documentation_entity_mention",
			"payload": map[string]any{
				"document_id":  "doc:payments-architecture",
				"mention_text": "repo-payments-api",
				"candidate_refs": []any{map[string]any{
					"kind": "repository",
					"id":   "repo-payments-api",
				}},
			},
		}},
		Coverage: documentationTargetCoverage{
			Target: documentationTargetScope{
				Repository: "repo-payments-api",
				TargetKind: "repository",
				TargetID:   "repo-payments-api",
			},
			FindingsReturned: 1,
			TargetFactCount:  1,
			TargetFactKinds:  map[string]int{"documentation_entity_mention": 1},
		},
	}
}

func missingCorrelationDocumentationReadModel() documentationFindingListReadModel {
	return documentationFindingListReadModel{
		RelatedFacts: []map[string]any{{
			"fact_id":   "fact:payments-generic-mention",
			"fact_kind": "documentation_entity_mention",
			"payload": map[string]any{
				"document_id":  "doc:payments-overview",
				"mention_text": "payments-api",
				"candidate_refs": []any{map[string]any{
					"kind": "service",
					"id":   "workload:payments-api",
				}},
			},
		}},
		Coverage: documentationTargetCoverage{
			Target: documentationTargetScope{
				Repository: "repo-payments-api",
				TargetKind: "service",
				TargetID:   "workload:payments-api",
				ServiceID:  "workload:payments-api",
			},
			FindingsReturned: 0,
			TargetFactCount:  1,
			TargetFactKinds:  map[string]int{"documentation_entity_mention": 1},
		},
		MissingEvidence: []documentationMissingEvidence{{
			Reason: "documentation_findings_absent",
			Detail: "target documentation facts exist but no admissible documentation findings matched the target scope",
		}},
	}
}

func serviceStoryEnvelopeData(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Data == nil {
		t.Fatalf("envelope data is nil; error = %#v", envelope.Error)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
