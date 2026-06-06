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
		"MATCH (c:CloudResource)",
		"LIMIT $limit",
		"toLower(coalesce(c.arn, '')) CONTAINS $service_token",
		"toLower(coalesce(c.id, '')) CONTAINS $service_token",
		"toLower(coalesce(c.source, c.source_system, '')) CONTAINS $service_token",
		"toLower(coalesce(c.config_path, '')) CONTAINS $service_token",
	} {
		if !strings.Contains(seenCypher, want) {
			t.Fatalf("candidate cypher missing %q: %s", want, seenCypher)
		}
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
