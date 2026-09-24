// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIDeadCodeMentionsHaskellRootsAndLanguageFilter(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	deadCodePath := testutil.MustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := testutil.MustMapField(t, deadCodePath, "post")
	description, ok := deadCodePost["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code description = %T, want string", deadCodePost["description"])
	}
	if !strings.Contains(description, "Haskell") {
		t.Fatalf("code/dead-code description = %q, want Haskell root coverage", description)
	}

	requestBody := testutil.MustMapField(t, testutil.MustMapField(t, deadCodePost, "requestBody"), "content")
	requestJSON := testutil.MustMapField(t, requestBody, "application/json")
	schema := testutil.MustMapField(t, testutil.MustMapField(t, requestJSON, "schema"), "properties")
	candidateKind := testutil.MustMapField(t, schema, "candidate_kind")
	enum, ok := candidateKind["enum"].([]any)
	if !ok {
		t.Fatalf("code/dead-code candidate_kind enum = %T, want []any", candidateKind["enum"])
	}
	advertised := make([]string, 0, len(enum))
	for _, value := range enum {
		label, ok := value.(string)
		if !ok {
			t.Fatalf("code/dead-code candidate_kind enum value = %#v, want string", value)
		}
		advertised = append(advertised, label)
	}
	scanned := slices.Clone(code.DeadCodeCandidateLabels)
	slices.Sort(advertised)
	slices.Sort(scanned)
	if !slices.Equal(advertised, scanned) {
		t.Fatalf("code/dead-code candidate_kind enum = %v, want the scanned label set %v", advertised, scanned)
	}
	language := testutil.MustMapField(t, schema, "language")
	languageDescription, ok := language["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code language description = %T, want string", language["description"])
	}
	if !strings.Contains(languageDescription, "haskell") {
		t.Fatalf("code/dead-code language description = %q, want haskell example", languageDescription)
	}

	responses := testutil.MustMapField(t, deadCodePost, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, okResponse, "content")
	responseJSON := testutil.MustMapField(t, content, "application/json")
	responseProperties := testutil.MustMapField(t, testutil.MustMapField(t, responseJSON, "schema"), "properties")
	analysis := testutil.MustMapField(t, responseProperties, "analysis")
	analysisProperties := testutil.MustMapField(t, analysis, "properties")
	if _, ok := analysisProperties["reflection_modeled_languages"]; !ok {
		t.Fatal("code/dead-code analysis schema missing reflection_modeled_languages")
	}
}

func TestOpenAPIDeadCodeInvestigationDocumentsReturnedFields(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	investigationPath := testutil.MustMapField(t, paths, "/api/v0/code/dead-code/investigate")
	investigationPost := testutil.MustMapField(t, investigationPath, "post")
	responses := testutil.MustMapField(t, investigationPost, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, okResponse, "content")
	responseJSON := testutil.MustMapField(t, content, "application/json")
	properties := testutil.MustMapField(t, testutil.MustMapField(t, responseJSON, "schema"), "properties")

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

	paths := testutil.MustMapField(t, spec, "paths")
	crossRepoPath := testutil.MustMapField(t, paths, "/api/v0/code/dead-code/cross-repo")
	post := testutil.MustMapField(t, crossRepoPath, "post")
	description, ok := post["description"].(string)
	if !ok {
		t.Fatalf("description type = %T, want string", post["description"])
	}
	for _, want := range []string{"live_by_consumer", "unknown_needs_evidence", "stale generations"} {
		if !strings.Contains(description, want) {
			t.Fatalf("description = %q, want %q", description, want)
		}
	}
	requestProperties := testutil.MustMapField(
		t,
		testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, post, "requestBody"), "content"), "application/json"),
		"schema",
	)
	requestFields := testutil.MustMapField(t, requestProperties, "properties")
	for _, field := range []string{"repo_id", "consumer_repo_ids", "language", "limit"} {
		if _, ok := requestFields[field]; !ok {
			t.Fatalf("cross-repo dead-code request schema missing %s", field)
		}
	}
	responses := testutil.MustMapField(t, post, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	properties := testutil.MustMapField(
		t,
		testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json"), "schema"),
		"properties",
	)
	for _, field := range []string{"query_shape", "candidate_buckets", "bucket_counts"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("cross-repo dead-code response schema missing %s", field)
		}
	}
}
