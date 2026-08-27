// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantictools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveSemanticRegistrationContract(t *testing.T) {
	t.Parallel()

	evidenceTools := EvidenceTools()
	searchTools := SearchTools()
	if got, want := len(evidenceTools), 2; got != want {
		t.Fatalf("semantic evidence tool count = %d, want %d", got, want)
	}
	if got, want := len(searchTools), 1; got != want {
		t.Fatalf("semantic search tool count = %d, want %d", got, want)
	}
	tools := append(evidenceTools, searchTools...)
	wantNames := []string{
		"list_semantic_documentation_observations",
		"list_semantic_code_hints",
		"search_semantic_context",
	}
	if got, want := len(tools), len(wantNames); got != want {
		t.Fatalf("semantic tool count = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := tools[i].Name; got != want {
			t.Fatalf("semantic tool %d name = %q, want %q", i, got, want)
		}
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal semantic tools: %v", err)
	}
	if got, want := len(encoded), 4188; got != want {
		t.Fatalf("serialized semantic tool definitions length = %d, want %d", got, want)
	}
	const wantDefinitionsHash = "4f58551bed9b8e61e7595b12b68f05f2a140ad9c53b11e95f60a3f7b8999021d"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("semantic tool definitions hash = %s, want %s", got, wantDefinitionsHash)
	}

	requireSemanticSchemaTypes(t, evidenceTools, searchTools)
}

func TestEvidenceToolsPreserveShallowSiblingCloneTopology(t *testing.T) {
	t.Parallel()

	tools := EvidenceTools()
	documentationProperties := semanticProperties(t, tools[0].InputSchema)
	codeHintProperties := semanticProperties(t, tools[1].InputSchema)

	documentationProperties["__outer_mutation__"] = true
	if _, ok := codeHintProperties["__outer_mutation__"]; ok {
		t.Fatal("semantic evidence tools share outer properties map storage")
	}

	documentationLimit, ok := documentationProperties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("documentation limit schema type = %T, want map[string]any", documentationProperties["limit"])
	}
	codeHintLimit, ok := codeHintProperties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("code-hint limit schema type = %T, want map[string]any", codeHintProperties["limit"])
	}
	documentationLimit["maximum"] = 201
	if got, want := codeHintLimit["maximum"], 201; got != want {
		t.Fatalf("shared evidence limit maximum = %#v, want %#v", got, want)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := append(EvidenceTools(), SearchTools()...)
	second := append(EvidenceTools(), SearchTools()...)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("semantic tool counts = %d and %d, want 3 and 3", len(first), len(second))
	}
	encodedSecond, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second semantic tool set: %v", err)
	}

	first[0].Name = "mutated"
	if second[0].Name == "mutated" {
		t.Fatal("semantic tool constructors share slice storage")
	}
	for i := range first {
		mutateSemanticSchema(first[i].InputSchema)
	}

	encodedSecondAfterMutation, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second semantic tool set after mutation: %v", err)
	}
	if !bytes.Equal(encodedSecondAfterMutation, encodedSecond) {
		t.Fatal("semantic tool constructors share nested schema storage")
	}
}

func mutateSemanticSchema(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			mutateSemanticSchema(child)
		}
		typed["__mutation__"] = true
	case []any:
		for i := range typed {
			mutateSemanticSchema(typed[i])
		}
		if len(typed) > 0 {
			typed[0] = "mutated"
		}
	case []string:
		if len(typed) > 0 {
			typed[0] = "mutated"
		}
	}
}

func requireSemanticSchemaTypes(
	t *testing.T,
	evidenceTools []toolcontract.ToolDefinition,
	searchTools []toolcontract.ToolDefinition,
) {
	t.Helper()

	for i, tool := range evidenceTools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("semantic evidence tool %d schema type = %T, want map[string]any", i, tool.InputSchema)
		}
		if _, ok := schema["required"].([]string); !ok {
			t.Fatalf("semantic evidence tool %d required type = %T, want []string", i, schema["required"])
		}
	}
	searchSchema, ok := searchTools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("semantic search schema type = %T, want map[string]any", searchTools[0].InputSchema)
	}
	if _, ok := searchSchema["required"].([]any); !ok {
		t.Fatalf("semantic search required type = %T, want []any", searchSchema["required"])
	}
	properties := semanticProperties(t, searchSchema)
	mode, ok := properties["mode"].(map[string]any)
	if !ok {
		t.Fatalf("semantic search mode schema type = %T, want map[string]any", properties["mode"])
	}
	if _, ok := mode["enum"].([]any); !ok {
		t.Fatalf("semantic search mode enum type = %T, want []any", mode["enum"])
	}
}

func semanticProperties(t *testing.T, schema any) map[string]any {
	t.Helper()

	root, ok := schema.(map[string]any)
	if !ok {
		t.Fatalf("semantic schema type = %T, want map[string]any", schema)
	}
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("semantic properties type = %T, want map[string]any", root["properties"])
	}
	return properties
}
