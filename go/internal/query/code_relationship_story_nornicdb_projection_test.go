// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestNornicDBRelationshipStoryCypherReturnsDirectProperties(t *testing.T) {
	t.Parallel()

	for _, direction := range []string{"incoming", "outgoing"} {
		sourceVariable := "source"
		targetVariable := "anchor"
		if direction == "outgoing" {
			sourceVariable = "anchor"
			targetVariable = "target"
		}
		cypher, _ := nornicDBRelationshipStoryGraphCypher(
			relationshipStoryRequest{RelationshipType: "CALLS", Limit: 50},
			"function-target",
			"Function",
			"uid",
			direction,
			repositoryAccessFilter{allScopes: true},
		)
		if strings.Contains(cypher, "coalesce(") {
			t.Fatalf("%s story projection contains unsupported coalesce():\n%s", direction, cypher)
		}
		for _, projection := range []string{
			sourceVariable + ".id as source_legacy_id",
			sourceVariable + ".uid as source_uid",
			targetVariable + ".id as target_legacy_id",
			targetVariable + ".uid as target_uid",
			sourceVariable + ".repo_id as source_node_repo_id",
			"sourceRepo.id as source_repo_fallback_id",
			targetVariable + ".repo_id as target_node_repo_id",
			"targetRepo.id as target_repo_fallback_id",
		} {
			if !strings.Contains(cypher, projection) {
				t.Fatalf("%s story projection missing %q:\n%s", direction, projection, cypher)
			}
		}
	}
}

func TestNormalizeNornicDBRelationshipStoryRowsCoalescesRawProperties(t *testing.T) {
	t.Parallel()

	rows := normalizeNornicDBRelationshipStoryRows([]map[string]any{
		{
			"source_legacy_id":        "source.id",
			"source_uid":              "source-uid",
			"source_node_repo_id":     "source.repo_id",
			"source_repo_fallback_id": "source-repo",
			"source_language_value":   "source.language",
			"source_lang_value":       "java",
			"source_file_language":    "sourceFile.language",
			"target_legacy_id":        "legacy-target",
			"target_uid":              "target-uid",
			"target_node_repo_id":     "target-repo",
			"target_repo_fallback_id": "fallback-target-repo",
			"target_language_value":   "kotlin",
			"target_lang_value":       "target.lang",
			"target_file_language":    "targetFile.language",
			"method_legacy_id":        "method.id",
			"method_uid":              "method-uid",
		},
		{
			"source_legacy_id":        "anchor.id",
			"source_uid":              "anchor.uid",
			"source_node_repo_id":     "anchor.repo_id",
			"source_repo_fallback_id": "source-repo-fallback",
			"source_language_value":   "anchor.language",
			"source_lang_value":       "anchor.lang",
			"source_file_language":    "go",
			"target_legacy_id":        "anchor.id",
			"target_uid":              "anchor.uid",
			"target_node_repo_id":     "anchor.repo_id",
			"target_repo_fallback_id": "target-repo-fallback",
			"target_language_value":   "anchor.language",
			"target_lang_value":       "anchor.lang",
			"target_file_language":    "rust",
		},
	})
	if got, want := StringVal(rows[0], "source_id"), "source-uid"; got != want {
		t.Fatalf("source_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "source_repo_id"), "source-repo"; got != want {
		t.Fatalf("source_repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "source_language"), "java"; got != want {
		t.Fatalf("source_language = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "target_id"), "legacy-target"; got != want {
		t.Fatalf("target_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "target_repo_id"), "target-repo"; got != want {
		t.Fatalf("target_repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "target_language"), "kotlin"; got != want {
		t.Fatalf("target_language = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[0], "method_id"), "method-uid"; got != want {
		t.Fatalf("method_id = %q, want %q", got, want)
	}
	if _, present := rows[1]["source_id"]; present {
		t.Fatalf("anchor source identity placeholders leaked as source_id: %#v", rows[1]["source_id"])
	}
	if _, present := rows[1]["target_id"]; present {
		t.Fatalf("anchor target identity placeholders leaked as target_id: %#v", rows[1]["target_id"])
	}
	if got, want := StringVal(rows[1], "source_repo_id"), "source-repo-fallback"; got != want {
		t.Fatalf("anchor-placeholder source_repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[1], "source_language"), "go"; got != want {
		t.Fatalf("anchor-placeholder source_language = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[1], "target_repo_id"), "target-repo-fallback"; got != want {
		t.Fatalf("anchor-placeholder target_repo_id = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[1], "target_language"), "rust"; got != want {
		t.Fatalf("anchor-placeholder target_language = %q, want %q", got, want)
	}
	for _, rawKey := range []string{
		"source_legacy_id", "source_uid", "source_node_repo_id", "source_repo_fallback_id",
		"source_language_value", "source_lang_value", "source_file_language",
		"target_legacy_id", "target_uid", "target_node_repo_id", "target_repo_fallback_id",
		"target_language_value", "target_lang_value", "target_file_language",
		"method_legacy_id", "method_uid",
	} {
		for index, row := range rows {
			if _, present := row[rawKey]; present {
				t.Fatalf("raw projection key %q leaked into normalized row %d", rawKey, index)
			}
		}
	}
}

func TestNornicDBRelationshipStoryHelperCypherReturnsDirectProperties(t *testing.T) {
	t.Parallel()

	classCypher, _ := nornicDBRelationshipStoryClassMethodsCypher(
		relationshipStoryRequest{Limit: 50},
		"class-target",
		"uid",
	)
	if strings.Contains(classCypher, "coalesce(") ||
		!strings.Contains(classCypher, "method.id as method_legacy_id") ||
		!strings.Contains(classCypher, "method.uid as method_uid") {
		t.Fatalf("class method projection is not direct-property compatible:\n%s", classCypher)
	}

	for _, direction := range []string{"incoming", "outgoing"} {
		sourceVariable := "source"
		targetVariable := "anchor"
		if direction == "outgoing" {
			sourceVariable = "anchor"
			targetVariable = "target"
		}
		inheritanceCypher, _ := nornicDBRelationshipStoryInheritanceDepthCypher(
			relationshipStoryRequest{Limit: 50, MaxDepth: 3},
			"class-target",
			direction,
			"uid",
		)
		if strings.Contains(inheritanceCypher, "coalesce(") ||
			!strings.Contains(inheritanceCypher, sourceVariable+".id as source_legacy_id") ||
			!strings.Contains(inheritanceCypher, targetVariable+".uid as target_uid") {
			t.Fatalf("%s inheritance projection is not direct-property compatible:\n%s", direction, inheritanceCypher)
		}
	}
}
