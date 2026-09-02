// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIDeadCodeMentionsHaskellRootsAndLanguageFilter(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	deadCodePath := querytestutil.MustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := querytestutil.MustMapField(t, deadCodePath, "post")
	description, ok := deadCodePost["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code description = %T, want string", deadCodePost["description"])
	}
	if !strings.Contains(description, "Haskell") {
		t.Fatalf("code/dead-code description = %q, want Haskell root coverage", description)
	}

	requestBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, deadCodePost, "requestBody"), "content")
	requestJSON := querytestutil.MustMapField(t, requestBody, "application/json")
	schema := querytestutil.MustMapField(t, querytestutil.MustMapField(t, requestJSON, "schema"), "properties")
	candidateKind := querytestutil.MustMapField(t, schema, "candidate_kind")
	if got, ok := candidateKind["enum"].([]any); !ok || len(got) != len(deadCodeCandidateLabels) {
		t.Fatalf("code/dead-code candidate_kind enum = %#v, want %d advertised labels", candidateKind["enum"], len(deadCodeCandidateLabels))
	}
	language := querytestutil.MustMapField(t, schema, "language")
	languageDescription, ok := language["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code language description = %T, want string", language["description"])
	}
	if !strings.Contains(languageDescription, "haskell") {
		t.Fatalf("code/dead-code language description = %q, want haskell example", languageDescription)
	}

	responses := querytestutil.MustMapField(t, deadCodePost, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, okResponse, "content")
	responseJSON := querytestutil.MustMapField(t, content, "application/json")
	responseProperties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, responseJSON, "schema"), "properties")
	analysis := querytestutil.MustMapField(t, responseProperties, "analysis")
	analysisProperties := querytestutil.MustMapField(t, analysis, "properties")
	if _, ok := analysisProperties["reflection_modeled_languages"]; !ok {
		t.Fatal("code/dead-code analysis schema missing reflection_modeled_languages")
	}
}

func TestOpenAPIDeadCodeInvestigationDocumentsReturnedFields(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	investigationPath := querytestutil.MustMapField(t, paths, "/api/v0/code/dead-code/investigate")
	investigationPost := querytestutil.MustMapField(t, investigationPath, "post")
	responses := querytestutil.MustMapField(t, investigationPost, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, okResponse, "content")
	responseJSON := querytestutil.MustMapField(t, content, "application/json")
	properties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, responseJSON, "schema"), "properties")

	for _, field := range []string{
		"display_truncated",
		"candidate_scan_truncated",
		"candidate_scan_limit",
		"candidate_scan_limit_per_label",
		"candidate_scan_pages",
		"candidate_scan_rows",
		"suppressed_truncated",
		"next_offset",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("dead-code investigation response schema missing %s", field)
		}
	}
}

func TestOpenAPICrossRepoDeadCodeDocumentsEvidenceBuckets(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	crossRepoPath := querytestutil.MustMapField(t, paths, "/api/v0/code/dead-code/cross-repo")
	post := querytestutil.MustMapField(t, crossRepoPath, "post")
	description, ok := post["description"].(string)
	if !ok {
		t.Fatalf("description type = %T, want string", post["description"])
	}
	for _, want := range []string{"live_by_consumer", "unknown_needs_evidence", "stale generations"} {
		if !strings.Contains(description, want) {
			t.Fatalf("description = %q, want %q", description, want)
		}
	}
	requestProperties := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, post, "requestBody"), "content"), "application/json"),
		"schema",
	)
	requestFields := querytestutil.MustMapField(t, requestProperties, "properties")
	for _, field := range []string{"repo_id", "consumer_repo_ids", "language", "limit"} {
		if _, ok := requestFields[field]; !ok {
			t.Fatalf("cross-repo dead-code request schema missing %s", field)
		}
	}
	responses := querytestutil.MustMapField(t, post, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	properties := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json"), "schema"),
		"properties",
	)
	for _, field := range []string{"query_shape", "candidate_buckets", "bucket_counts"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("cross-repo dead-code response schema missing %s", field)
		}
	}
}
