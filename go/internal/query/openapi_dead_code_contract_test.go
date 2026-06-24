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
