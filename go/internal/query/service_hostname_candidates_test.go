// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/contentrefs"
)

func TestExactHostnameCandidateReasonPrefersURLReference(t *testing.T) {
	t.Parallel()

	candidates := []contentrefs.HostnameCandidate{
		{
			Value:          "sample-service-api.prod.example.test",
			Classification: "exact_hostname",
			Reason:         "hostname_key_reference",
		},
		{
			Value:          "sample-service-api.prod.example.test",
			Classification: "exact_hostname",
			Reason:         "url_hostname_reference",
		},
	}

	if got, want := exactHostnameCandidateReason(candidates, "sample-service-api.prod.example.test"), "url_hostname_reference"; got != want {
		t.Fatalf("exactHostnameCandidateReason() = %q, want %q", got, want)
	}
}

func TestLoadServiceQueryEvidenceClassifiesNonEntrypointHostnameCandidates(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-sample-service-api", RelativePath: "deploy/prod/ingress.yaml"},
		},
		fileContent: map[string]string{
			"deploy/prod/ingress.yaml": `
public_url: "https://sample-service-api.prod.example.test/v1"
hostname: "app.config.retry.count"
endpoint: "fixture.response.body.items.id"
url: "search.fields.title.keyword"
host: "catalog.service"
docs: "/docs"
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-sample-service-api", "sample-service-api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v, want nil", err)
	}

	gotHostnames := make([]string, 0, len(evidence.Hostnames))
	for _, row := range evidence.Hostnames {
		gotHostnames = append(gotHostnames, row.Hostname)
	}
	wantHostnames := []string{"sample-service-api.prod.example.test"}
	if !reflect.DeepEqual(gotHostnames, wantHostnames) {
		t.Fatalf("hostnames = %#v, want %#v", gotHostnames, wantHostnames)
	}

	if got, want := evidence.DocsRoutes, []ServiceDocsRouteEvidence{
		{Route: "/docs", RelativePath: "deploy/prod/ingress.yaml", Reason: "docs_route_reference"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docs_routes = %#v, want %#v", got, want)
	}

	gotCandidates := evidence.EntrypointCandidates
	wantCandidates := []ServiceEntrypointCandidateEvidence{
		{
			Candidate:      "app.config.retry.count",
			Classification: "rejected_config_key",
			RelativePath:   "deploy/prod/ingress.yaml",
			Reason:         "dotted_config_key",
		},
		{
			Candidate:      "catalog.service",
			Classification: "ambiguous",
			RelativePath:   "deploy/prod/ingress.yaml",
			Reason:         "two_label_hostname_candidate",
		},
		{
			Candidate:      "fixture.response.body.items.id",
			Classification: "rejected_field_path",
			RelativePath:   "deploy/prod/ingress.yaml",
			Reason:         "dotted_field_path",
		},
		{
			Candidate:      "search.fields.title.keyword",
			Classification: "rejected_field_path",
			RelativePath:   "deploy/prod/ingress.yaml",
			Reason:         "dotted_field_path",
		},
	}
	if !reflect.DeepEqual(gotCandidates, wantCandidates) {
		t.Fatalf("entrypoint candidates = %#v, want %#v", gotCandidates, wantCandidates)
	}
}

func TestBuildServiceStoryResponseExposesNonEntrypointCandidates(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"hostnames": []map[string]any{
			{
				"hostname":      "sample-service-api.prod.example.test",
				"relative_path": "deploy/prod/ingress.yaml",
				"reason":        "url_hostname_reference",
			},
		},
		"entrypoints": []map[string]any{
			{
				"type":          "docs_route",
				"target":        "/docs",
				"visibility":    "internal",
				"relative_path": "deploy/prod/ingress.yaml",
				"reason":        "docs_route_reference",
			},
			{
				"type":          "hostname",
				"target":        "sample-service-api.prod.example.test",
				"visibility":    "public",
				"relative_path": "deploy/prod/ingress.yaml",
				"reason":        "url_hostname_reference",
			},
		},
		"entrypoint_candidates": []map[string]any{
			{
				"candidate":      "app.config.retry.count",
				"classification": "rejected_config_key",
				"relative_path":  "deploy/prod/ingress.yaml",
				"reason":         "dotted_config_key",
			},
		},
	}

	got := buildServiceStoryResponse("sample-service-api", workloadContext)
	entrypoints := mapSliceValue(got, "entrypoints")
	if len(entrypoints) != 2 {
		t.Fatalf("entrypoints = %#v, want docs route and exact hostname only", entrypoints)
	}
	for _, entrypoint := range entrypoints {
		if StringVal(entrypoint, "target") == "app.config.retry.count" {
			t.Fatalf("rejected config key was promoted to service entrypoint: %#v", entrypoints)
		}
	}

	candidates := mapSliceValue(got, "entrypoint_candidates")
	if len(candidates) != 1 {
		t.Fatalf("entrypoint_candidates = %#v, want rejected supporting evidence", candidates)
	}
	if got, want := StringVal(candidates[0], "reason"), "dotted_config_key"; got != want {
		t.Fatalf("entrypoint_candidates[0].reason = %q, want %q", got, want)
	}
}

func TestRepositoryStoryReadbackKeepsDocsRoutesWithoutHostnameEntrypoints(t *testing.T) {
	t.Parallel()

	response := buildRepositoryStoryResponse(
		RepoRef{ID: "repo-sample-service-api", Name: "sample-service-api"},
		1,
		[]string{"yaml"},
		nil,
		nil,
		0,
		nil,
		nil,
	)
	enrichRepositoryStoryResponseWithEvidence(response, nil, []FileContent{
		{
			RepoID:       "repo-sample-service-api",
			RelativePath: "docs/routes.md",
			Content: `
hostname: "app.config.retry.count"
docs route: "/docs"
`,
		},
	})

	documentation := mapValue(response, "documentation_overview")
	if got, want := StringSliceVal(documentation, "docs_routes"), []string{"/docs"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("documentation_overview.docs_routes = %#v, want %#v", got, want)
	}
	if _, ok := mapValue(response, "deployment_overview")["hostnames"]; ok {
		t.Fatalf("repository story deployment_overview exposed hostnames: %#v", response["deployment_overview"])
	}
}
