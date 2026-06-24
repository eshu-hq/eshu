// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetServiceStorySurfacesTargetLinkedSupportEvidence(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: serviceStoryExternalDocsGraphReader{
			t:      t,
			repoID: "repo-payments-api",
		},
		Content: fakePortContentStore{
			targetSupportModel: targetLinkedSupportReadModel(),
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
	targetSupport := mapValue(mapValue(data, "support_overview"), "target_support")
	if got, want := IntVal(targetSupport, "evidence_count"), 2; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d: %#v", got, want, targetSupport)
	}
	if got, want := IntVal(targetSupport, "work_item_count"), 1; got != want {
		t.Fatalf("target_support.work_item_count = %d, want %d", got, want)
	}
	if got, want := IntVal(targetSupport, "incident_routing_count"), 1; got != want {
		t.Fatalf("target_support.incident_routing_count = %d, want %d", got, want)
	}
	if got := mapSliceValue(targetSupport, "missing_evidence"); len(got) != 0 {
		t.Fatalf("target_support.missing_evidence = %#v, want empty for proven support evidence", got)
	}
}

func TestGetServiceStoryPreservesMissingSupportCorrelation(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: serviceStoryExternalDocsGraphReader{
			t:      t,
			repoID: "repo-payments-api",
		},
		Content: fakePortContentStore{
			targetSupportModel: missingSupportReadModel(),
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
	targetSupport := mapValue(mapValue(data, "support_overview"), "target_support")
	if got := len(mapSliceValue(targetSupport, "evidence")); got != 0 {
		t.Fatalf("len(target_support.evidence) = %d, want 0", got)
	}
	missing := mapSliceValue(targetSupport, "missing_evidence")
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(target_support.missing_evidence) = %d, want %d", got, want)
	}
	if got, want := StringVal(missing[0], "reason"), "support_target_facts_absent"; got != want {
		t.Fatalf("missing_evidence[0].reason = %q, want %q", got, want)
	}
}

func TestGetRepositoryStorySurfacesTargetLinkedSupportEvidence(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: repositoryStoryExternalDocsGraphReader{t: t, repoID: "repo-payments-api"},
		Content: fakePortContentStore{
			targetSupportModel: targetLinkedSupportReadModel(),
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
	data := serviceStoryEnvelopeData(t, w.Body.Bytes())
	targetSupport := mapValue(mapValue(data, "support_overview"), "target_support")
	if got, want := IntVal(targetSupport, "evidence_count"), 2; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d: %#v", got, want, targetSupport)
	}
}

func TestBuildStoryTargetSupportKeepsAmbiguousCandidateRefsSeparate(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{{
		"fact_id":   "jira-ambiguous",
		"fact_kind": "work_item.external_link",
		"payload": map[string]any{
			"candidate_refs": []any{
				map[string]any{"kind": "service", "id": "workload:payments-api"},
				map[string]any{"kind": "service", "id": "workload:payments-worker"},
			},
		},
	}}, false)

	if gotCount := IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for ambiguous support fact", gotCount)
	}
	if gotCount := IntVal(got, "ambiguous_count"); gotCount != 1 {
		t.Fatalf("ambiguous_count = %d, want 1", gotCount)
	}
	missing := mapSliceValue(got, "missing_evidence")
	if gotReason := StringVal(missing[0], "reason"); gotReason != "support_correlation_ambiguous" {
		t.Fatalf("missing_evidence[0].reason = %q, want support_correlation_ambiguous", gotReason)
	}
}

func TestBuildStoryTargetSupportMatchesExplicitSupportTargetAliases(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{{
		"fact_id":   "jira-workload-ref",
		"fact_kind": "work_item.record",
		"payload": map[string]any{
			"candidate_refs": []any{map[string]any{
				"kind": "workload",
				"id":   "workload:payments-api",
			}},
			"work_item_key": "PAY-123",
		},
	}, {
		"fact_id":   "pd-workload-ref",
		"fact_kind": "incident_routing.observed_pagerduty_service",
		"payload": map[string]any{
			"linked_entities": []any{map[string]any{
				"entity_type": "workload",
				"entity_id":   "workload:payments-api",
			}},
			"service_id": "PAGERDUTY_SERVICE_ID",
		},
	}}, false)

	if gotCount, want := IntVal(got, "evidence_count"), 2; gotCount != want {
		t.Fatalf("evidence_count = %d, want %d: %#v", gotCount, want, got)
	}
	if gotCount, want := IntVal(got, "work_item_count"), 1; gotCount != want {
		t.Fatalf("work_item_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := IntVal(got, "incident_routing_count"), 1; gotCount != want {
		t.Fatalf("incident_routing_count = %d, want %d", gotCount, want)
	}
	if gotMissing := mapSliceValue(got, "missing_evidence"); len(gotMissing) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", gotMissing)
	}
}

func TestBuildStoryTargetSupportMatchesExplicitRepositoryAlias(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{{
		"fact_id":   "jira-repo-ref",
		"fact_kind": "work_item.external_link",
		"payload": map[string]any{
			"evidence_refs": []any{map[string]any{
				"kind": "repo",
				"id":   "repo-payments-api",
			}},
			"work_item_key": "PAY-456",
		},
	}}, false)

	if gotCount, want := IntVal(got, "evidence_count"), 1; gotCount != want {
		t.Fatalf("evidence_count = %d, want %d: %#v", gotCount, want, got)
	}
	if gotMissing := mapSliceValue(got, "missing_evidence"); len(gotMissing) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", gotMissing)
	}
}

func TestBuildStoryTargetSupportDoesNotMatchGenericServiceName(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{{
		"fact_id":   "jira-generic",
		"fact_kind": "work_item.record",
		"payload": map[string]any{
			"summary_present": true,
			"mention_text":    "payments-api",
		},
	}}, false)

	if gotCount := IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for generic name-only support fact", gotCount)
	}
	if gotCount := IntVal(got, "ambiguous_count"); gotCount != 0 {
		t.Fatalf("ambiguous_count = %d, want 0 for generic name-only support fact", gotCount)
	}
	if gotReason := StringVal(mapSliceValue(got, "missing_evidence")[0], "reason"); gotReason != "support_target_facts_absent" {
		t.Fatalf("missing reason = %q, want support_target_facts_absent", gotReason)
	}
}

func TestBuildStoryTargetSupportPayloadKeepsSensitiveFieldsOut(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{{
		"fact_id":   "jira-123",
		"fact_kind": "work_item.record",
		"payload": map[string]any{
			"candidate_refs":  []any{map[string]any{"kind": "service", "id": "workload:payments-api"}},
			"work_item_key":   "PAY-123",
			"url_fingerprint": "urlfp-1",
			"summary":         "raw issue summary must not surface",
			"assignee":        "user@example.test",
			"raw_url":         "https://jira.example.test/browse/PAY-123",
		},
	}}, false)

	payload := mapValue(mapSliceValue(got, "evidence")[0], "payload")
	if got, want := StringVal(payload, "work_item_key"), "PAY-123"; got != want {
		t.Fatalf("payload.work_item_key = %q, want %q", got, want)
	}
	for _, blocked := range []string{"summary", "assignee", "raw_url", "candidate_refs"} {
		if _, ok := payload[blocked]; ok {
			t.Fatalf("payload includes blocked field %q: %#v", blocked, payload)
		}
	}
}

func TestBuildServiceStoryTargetSupportSQLIsTargetScopedAndBounded(t *testing.T) {
	t.Parallel()

	query, args := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	})

	assertSupportSQLContainsAll(
		t, query,
		"FROM fact_records AS fact",
		"fact.fact_kind = ANY($1::text[])",
		"fact.is_tombstone = FALSE",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.payload @>",
		"ORDER BY fact.observed_at DESC, fact.fact_id DESC",
		"LIMIT $",
	)
	if len(args) < 3 {
		t.Fatalf("args len = %d, want fact kinds, target predicates, and limit", len(args))
	}
	if strings.Contains(query, "service_name") || strings.Contains(query, "mention_text") ||
		strings.Contains(strings.ToLower(query), " like ") || strings.Contains(strings.ToLower(query), "lower(") {
		t.Fatalf("support SQL must not use name-only predicates:\n%s", query)
	}
	joinedArgs := documentationArgsString(args)
	for _, fragment := range []string{"workload:payments-api", `"kind":"workload"`} {
		if !strings.Contains(joinedArgs, fragment) {
			t.Fatalf("support SQL args missing explicit ref alias %q: %#v", fragment, args)
		}
	}
}

func TestBuildRepositoryStoryTargetSupportSQLIncludesRepositoryAlias(t *testing.T) {
	t.Parallel()

	_, args := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	})

	joinedArgs := documentationArgsString(args)
	for _, fragment := range []string{"repo-payments-api", `"kind":"repository"`, `"kind":"repo"`} {
		if !strings.Contains(joinedArgs, fragment) {
			t.Fatalf("support SQL args missing explicit repository ref alias %q: %#v", fragment, args)
		}
	}
}

func assertSupportSQLContainsAll(t *testing.T, value string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(value, want) {
			t.Fatalf("value missing %q:\n%s", want, value)
		}
	}
}

func targetLinkedSupportReadModel() serviceStoryTargetSupportReadModel {
	return serviceStoryTargetSupportReadModel{
		Support: map[string]any{
			"evidence": []map[string]any{
				{"fact_id": "jira-123", "fact_kind": "work_item.record"},
				{"fact_id": "pd-service", "fact_kind": "incident_routing.observed_pagerduty_service"},
			},
			"evidence_count":         2,
			"work_item_count":        1,
			"incident_routing_count": 1,
			"missing_evidence":       []map[string]any{},
		},
	}
}

func missingSupportReadModel() serviceStoryTargetSupportReadModel {
	return serviceStoryTargetSupportReadModel{
		Support: map[string]any{
			"evidence": []map[string]any{},
			"coverage": map[string]any{
				"target_fact_count": 0,
			},
			"missing_evidence": []map[string]any{{
				"reason": "support_target_facts_absent",
			}},
		},
	}
}

func (f fakePortContentStore) serviceStoryTargetSupportEvidence(
	context.Context,
	serviceStoryTargetSupportFilter,
) (serviceStoryTargetSupportReadModel, error) {
	return f.targetSupportModel, f.targetSupportErr
}
