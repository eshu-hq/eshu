package query

import (
	"context"
	"strings"
	"testing"
)

func TestCloudResourceCandidatesUseInfraSearchSourceFields(t *testing.T) {
	t.Parallel()

	var seenCypher string
	_, err := loadUncorrelatedCloudResourceCandidates(t.Context(), fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			seenCypher = cypher
			return nil, nil
		},
	}, "sample-service", 3)
	if err != nil {
		t.Fatalf("loadUncorrelatedCloudResourceCandidates() error = %v, want nil", err)
	}
	for _, want := range []string{
		"MATCH (n)",
		"WHERE (n:CloudResource)",
		"LIMIT $limit",
		"coalesce(n.arn, '') CONTAINS $query",
		"coalesce(n.id, '') CONTAINS $query",
		"coalesce(n.source, '') CONTAINS $query",
		"coalesce(n.config_path, '') CONTAINS $query",
	} {
		if !strings.Contains(seenCypher, want) {
			t.Fatalf("candidate cypher missing %q: %s", want, seenCypher)
		}
	}
	if strings.Contains(seenCypher, "toLower(") || strings.Contains(seenCypher, "$service_token") || strings.Contains(seenCypher, "$service_name") {
		t.Fatalf("candidate cypher must use infra-search-compatible parameterized CONTAINS shape: %s", seenCypher)
	}
	if strings.Contains(seenCypher, "CONTAINS $query OR\n") {
		t.Fatalf("candidate cypher must keep the broad CloudResource predicate on one line for NornicDB compatibility: %s", seenCypher)
	}
	for _, forbidden := range []string{
		"n.provider AS provider",
		"n.service_anchor_status AS service_anchor_status",
		"n.service_anchor_reason AS service_anchor_reason",
	} {
		if strings.Contains(seenCypher, forbidden) {
			t.Fatalf("candidate cypher must coalesce optional CloudResource projections, found %q in %s", forbidden, seenCypher)
		}
	}
	arnPredicate := "coalesce(n.arn, '') CONTAINS $query"
	resourceIDPredicate := "coalesce(n.resource_id, '') CONTAINS $query"
	if arnIndex, resourceIDIndex := strings.Index(seenCypher, arnPredicate), strings.Index(seenCypher, resourceIDPredicate); arnIndex < 0 || resourceIDIndex < 0 || arnIndex > resourceIDIndex {
		t.Fatalf("candidate cypher must preserve infra-search predicate order for NornicDB compatibility: %s", seenCypher)
	}
}

func TestCloudResourceCandidatesReturnSafeInfraSearchFields(t *testing.T) {
	t.Parallel()

	got, err := loadUncorrelatedCloudResourceCandidates(t.Context(), fakeRepoGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			return []map[string]any{
				{
					"id":             "cloud:ssm:/configd/sample-service/server/port",
					"name":           "",
					"resource_type":  "ssm_parameter",
					"provider":       "aws",
					"source":         "aws_cloud",
					"config_path":    "/configd/sample-service/server/port",
					"service_kind":   "ssm",
					"resource_id":    "",
					"candidate_note": "must not leak unknown fields",
				},
			}, nil
		},
	}, "sample-service", 5)
	if err != nil {
		t.Fatalf("loadUncorrelatedCloudResourceCandidates() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(got))
	}
	candidate := got[0]
	if got, want := StringVal(candidate, "id"), "cloud:ssm:/configd/sample-service/server/port"; got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	if got, want := StringVal(candidate, "config_path"), "/configd/sample-service/server/port"; got != want {
		t.Fatalf("config_path = %q, want %q", got, want)
	}
	if got, want := StringVal(candidate, "source"), "aws_cloud"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := StringVal(candidate, "service_kind"), "ssm"; got != want {
		t.Fatalf("service_kind = %q, want %q", got, want)
	}
	if _, ok := candidate["candidate_note"]; ok {
		t.Fatalf("candidate leaked unapproved field: %#v", candidate)
	}
}
