// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIDeadCodeMentionsHaskellRootsAndLanguageFilter(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	deadCodePath := mustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := mustMapField(t, deadCodePath, "post")
	description, ok := deadCodePost["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code description = %T, want string", deadCodePost["description"])
	}
	if !strings.Contains(description, "Haskell") {
		t.Fatalf("code/dead-code description = %q, want Haskell root coverage", description)
	}

	requestBody := mustMapField(t, mustMapField(t, deadCodePost, "requestBody"), "content")
	requestJSON := mustMapField(t, requestBody, "application/json")
	schema := mustMapField(t, mustMapField(t, requestJSON, "schema"), "properties")
	language := mustMapField(t, schema, "language")
	languageDescription, ok := language["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code language description = %T, want string", language["description"])
	}
	if !strings.Contains(languageDescription, "haskell") {
		t.Fatalf("code/dead-code language description = %q, want haskell example", languageDescription)
	}

	responses := mustMapField(t, deadCodePost, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	responseJSON := mustMapField(t, content, "application/json")
	responseProperties := mustMapField(t, mustMapField(t, responseJSON, "schema"), "properties")
	analysis := mustMapField(t, responseProperties, "analysis")
	analysisProperties := mustMapField(t, analysis, "properties")
	if _, ok := analysisProperties["reflection_modeled_languages"]; !ok {
		t.Fatal("code/dead-code analysis schema missing reflection_modeled_languages")
	}
}

func TestOpenAPIDeadCodeInvestigationDocumentsReturnedFields(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	investigationPath := mustMapField(t, paths, "/api/v0/code/dead-code/investigate")
	investigationPost := mustMapField(t, investigationPath, "post")
	responses := mustMapField(t, investigationPost, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	responseJSON := mustMapField(t, content, "application/json")
	properties := mustMapField(t, mustMapField(t, responseJSON, "schema"), "properties")

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

	paths := mustMapField(t, spec, "paths")
	crossRepoPath := mustMapField(t, paths, "/api/v0/code/dead-code/cross-repo")
	post := mustMapField(t, crossRepoPath, "post")
	description, ok := post["description"].(string)
	if !ok {
		t.Fatalf("description type = %T, want string", post["description"])
	}
	for _, want := range []string{"live_by_consumer", "unknown_needs_evidence", "stale generations"} {
		if !strings.Contains(description, want) {
			t.Fatalf("description = %q, want %q", description, want)
		}
	}
	requestProperties := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, post, "requestBody"), "content"), "application/json"),
		"schema",
	)
	requestFields := mustMapField(t, requestProperties, "properties")
	for _, field := range []string{"repo_id", "consumer_repo_ids", "language", "limit"} {
		if _, ok := requestFields[field]; !ok {
			t.Fatalf("cross-repo dead-code request schema missing %s", field)
		}
	}
	responses := mustMapField(t, post, "responses")
	okResponse := mustMapField(t, responses, "200")
	properties := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, okResponse, "content"), "application/json"), "schema"),
		"properties",
	)
	for _, field := range []string{"query_shape", "candidate_buckets", "bucket_counts"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("cross-repo dead-code response schema missing %s", field)
		}
	}
}
