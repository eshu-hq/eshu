// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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
		Profile: querycontract.ProfileProduction,
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
	targetSupport := querycontract.MapValue(querycontract.MapValue(data, "support_overview"), "target_support")
	if got, want := querycontract.IntVal(targetSupport, "evidence_count"), 1; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d: %#v", got, want, targetSupport)
	}
	if got, want := querycontract.IntVal(targetSupport, "work_item_count"), 1; got != want {
		t.Fatalf("target_support.work_item_count = %d, want %d", got, want)
	}
	if got, want := querycontract.IntVal(targetSupport, "incident_routing_count"), 0; got != want {
		t.Fatalf("target_support.incident_routing_count = %d, want %d until #7463 links PagerDuty rows", got, want)
	}
	if got := querycontract.MapSliceValue(targetSupport, "missing_evidence"); len(got) != 0 {
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
		Profile: querycontract.ProfileProduction,
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
	targetSupport := querycontract.MapValue(querycontract.MapValue(data, "support_overview"), "target_support")
	if got := len(querycontract.MapSliceValue(targetSupport, "evidence")); got != 0 {
		t.Fatalf("len(target_support.evidence) = %d, want 0", got)
	}
	missing := querycontract.MapSliceValue(targetSupport, "missing_evidence")
	if got, want := len(missing), 1; got != want {
		t.Fatalf("len(target_support.missing_evidence) = %d, want %d", got, want)
	}
	if got, want := querycontract.StringVal(missing[0], "reason"), "support_target_facts_absent"; got != want {
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
		Profile: querycontract.ProfileProduction,
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
	targetSupport := querycontract.MapValue(querycontract.MapValue(data, "support_overview"), "target_support")
	if got, want := querycontract.IntVal(targetSupport, "evidence_count"), 1; got != want {
		t.Fatalf("target_support.evidence_count = %d, want %d: %#v", got, want, targetSupport)
	}
}

func TestBuildStoryTargetSupportDoesNotMatchGenericServiceName(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository:              "repo-payments-api",
		TargetKind:              "service",
		TargetID:                "workload:payments-api",
		ServiceID:               "workload:payments-api",
		Limit:                   serviceStoryTargetSupportLimit,
		RepositoryWorkloadCount: 1,
		RepositoryDefinesTarget: true,
	}, []map[string]any{{
		"fact_id":   "jira-generic",
		"fact_kind": "work_item.record",
		"payload": map[string]any{
			"summary_present": true,
			"mention_text":    "payments-api",
		},
	}}, false)

	if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for generic name-only support fact", gotCount)
	}
	if gotCount := querycontract.IntVal(got, "ambiguous_count"); gotCount != 0 {
		t.Fatalf("ambiguous_count = %d, want 0 for generic name-only support fact", gotCount)
	}
	if gotReason := querycontract.StringVal(querycontract.MapSliceValue(got, "missing_evidence")[0], "reason"); gotReason != "support_target_facts_absent" {
		t.Fatalf("missing reason = %q, want support_target_facts_absent", gotReason)
	}
}

func TestBuildStoryTargetSupportPayloadKeepsSensitiveFieldsOut(t *testing.T) {
	t.Parallel()

	fact := writerLinkFact("jira-123", "repo-payments-api")
	payload := fact["payload"].(map[string]any)
	payload["summary"] = "raw issue summary must not surface"
	payload["assignee"] = "user@example.test"
	payload["raw_url"] = "https://jira.example.test/browse/PAY-123"
	payload["candidate_refs"] = []any{map[string]any{"kind": "service", "id": "workload:payments-api"}}
	got := buildStoryTargetSupport(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	}, []map[string]any{fact}, false)

	evidence := querycontract.MapSliceValue(got, "evidence")
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1: %#v", len(evidence), got)
	}
	shown := querycontract.MapValue(evidence[0], "payload")
	if got, want := querycontract.StringVal(shown, "work_item_key"), "OPS-1"; got != want {
		t.Fatalf("payload.work_item_key = %q, want %q", got, want)
	}
	for _, blocked := range []string{"summary", "assignee", "raw_url", "candidate_refs"} {
		if _, ok := shown[blocked]; ok {
			t.Fatalf("payload includes blocked field %q: %#v", blocked, shown)
		}
	}
}

func TestBuildServiceStoryTargetSupportSQLIsTargetScopedAndBounded(t *testing.T) {
	t.Parallel()

	query, args := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository:              "repo-payments-api",
		TargetKind:              "service",
		TargetID:                "workload:payments-api",
		ServiceID:               "workload:payments-api",
		Limit:                   serviceStoryTargetSupportLimit,
		RepositoryWorkloadCount: 1,
		RepositoryDefinesTarget: true,
	})

	assertSupportSQLContainsAll(
		t, query,
		"FROM fact_records AS fact",
		"fact.fact_kind = 'work_item.external_link'",
		"fact.is_tombstone = FALSE",
		"fact.generation_id = scope.active_generation_id",
		"generation.status = 'active'",
		"fact.payload->>'linked_repository_id' = $1",
		"ORDER BY fact.observed_at DESC, fact.fact_id DESC",
		"LIMIT $2",
	)
	if len(args) != 2 {
		t.Fatalf("args len = %d, want the repository id and the limit", len(args))
	}
	if args[0] != "repo-payments-api" {
		t.Fatalf("args[0] = %#v, want the service target's repository, not its workload id", args[0])
	}
	if strings.Contains(query, "service_name") || strings.Contains(query, "mention_text") ||
		strings.Contains(strings.ToLower(query), " like ") || strings.Contains(strings.ToLower(query), "lower(") {
		t.Fatalf("support SQL must not use name-only predicates:\n%s", query)
	}
}

func TestBuildRepositoryStoryTargetSupportSQLBindsTheRepositoryID(t *testing.T) {
	t.Parallel()

	_, args := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "repository",
		TargetID:   "repo-payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	})

	if len(args) != 2 || args[0] != "repo-payments-api" || args[1] != serviceStoryTargetSupportLimit+1 {
		t.Fatalf("args = %#v, want [repo-payments-api, %d]", args, serviceStoryTargetSupportLimit+1)
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
				{"fact_id": "jira-link-1", "fact_kind": "work_item.external_link", "link_basis": "repository_sole_workload"},
			},
			"evidence_count":         1,
			"work_item_count":        1,
			"incident_routing_count": 0,
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

func (f fakePortContentStore) ServiceStoryTargetSupportEvidence(
	ctx context.Context,
	filter serviceStoryTargetSupportFilter,
) (serviceStoryTargetSupportReadModel, error) {
	return f.promoted().ServiceStoryTargetSupportEvidence(ctx, filter)
}
