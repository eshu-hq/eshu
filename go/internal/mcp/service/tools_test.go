// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveServiceRegistrationContract(t *testing.T) {
	t.Parallel()

	catalogTools := CatalogTools()
	contextTools := ContextTools()
	intelligenceTools := IntelligenceTools()
	requireServiceToolNames(t, "catalog", catalogTools, []string{
		"list_service_catalog_correlations",
	})
	requireServiceToolNames(t, "context", contextTools, []string{
		"get_service_context",
		"get_service_story",
		"investigate_service",
	})
	requireServiceToolNames(t, "intelligence", intelligenceTools, []string{
		"get_service_intelligence_report",
	})

	tools := combinedServiceTools()
	encoded := marshalServiceDefinitions(t, tools)
	if got, want := len(encoded), 5219; got != want {
		t.Fatalf("serialized service definitions length = %d, want %d", got, want)
	}
	const wantDefinitionsHash = "49c243812a07ca8e5a32112878b1d030af123899b57d30de23bebbcb6b8954e5"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("service definitions hash = %s, want %s", got, wantDefinitionsHash)
	}

	requireServiceSchemaTypes(t, catalogTools, contextTools, intelligenceTools)
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := combinedServiceTools()
	second := combinedServiceTools()
	encodedSecond := marshalServiceDefinitions(t, second)

	for i := range first {
		first[i].Name = "mutated"
		mutateServiceSchema(first[i].InputSchema)
	}
	if got := marshalServiceDefinitions(t, second); !bytes.Equal(got, encodedSecond) {
		t.Fatal("service constructors share slice or nested schema storage across calls")
	}
}

func TestToolsKeepSiblingDefinitionsIndependent(t *testing.T) {
	t.Parallel()

	assertServiceSiblingsIndependent(t, "context", ContextTools)
	assertServiceSiblingsIndependent(t, "combined", combinedServiceTools)
}

func requireServiceToolNames(
	t *testing.T,
	group string,
	tools []toolcontract.ToolDefinition,
	wantNames []string,
) {
	t.Helper()

	if got, want := len(tools), len(wantNames); got != want {
		t.Fatalf("%s tool count = %d, want %d", group, got, want)
	}
	for i, want := range wantNames {
		if got := tools[i].Name; got != want {
			t.Fatalf("%s tool %d name = %q, want %q", group, i, got, want)
		}
	}
}

func requireServiceSchemaTypes(
	t *testing.T,
	catalogTools []toolcontract.ToolDefinition,
	contextTools []toolcontract.ToolDefinition,
	intelligenceTools []toolcontract.ToolDefinition,
) {
	t.Helper()

	groups := [][]toolcontract.ToolDefinition{catalogTools, contextTools, intelligenceTools}
	for _, tools := range groups {
		for i, tool := range tools {
			schema := requireServiceSchemaMap(t, fmt.Sprintf("tool %d", i), tool.InputSchema)
			properties := requireServiceSchemaMap(t, fmt.Sprintf("tool %d properties", i), schema["properties"])
			for name, value := range properties {
				requireServiceSchemaMap(t, fmt.Sprintf("tool %d property %s", i, name), value)
			}
		}
	}

	catalogSchema := requireServiceSchemaMap(t, "catalog", catalogTools[0].InputSchema)
	catalogProperties := requireServiceSchemaMap(t, "catalog properties", catalogSchema["properties"])
	outcome := requireServiceSchemaMap(t, "catalog outcome", catalogProperties["outcome"])
	if _, ok := outcome["enum"].([]string); !ok {
		t.Fatalf("catalog outcome enum type = %T, want []string", outcome["enum"])
	}
	limit := requireServiceSchemaMap(t, "catalog limit", catalogProperties["limit"])
	for field, want := range map[string]int{"default": 50, "minimum": 1, "maximum": 200} {
		got, ok := limit[field].(int)
		if !ok {
			t.Fatalf("catalog limit %s type = %T, want int", field, limit[field])
		}
		if got != want {
			t.Fatalf("catalog limit %s = %d, want %d", field, got, want)
		}
	}

	for _, index := range []int{0, 2} {
		schema := requireServiceSchemaMap(t, fmt.Sprintf("context tool %d", index), contextTools[index].InputSchema)
		if _, ok := schema["required"].([]string); !ok {
			t.Fatalf("context tool %d required type = %T, want []string", index, schema["required"])
		}
	}
}

func combinedServiceTools() []toolcontract.ToolDefinition {
	catalogTools := CatalogTools()
	contextTools := ContextTools()
	intelligenceTools := IntelligenceTools()
	tools := make([]toolcontract.ToolDefinition, 0, len(catalogTools)+len(contextTools)+len(intelligenceTools))
	tools = append(tools, catalogTools...)
	tools = append(tools, contextTools...)
	return append(tools, intelligenceTools...)
}

func assertServiceSiblingsIndependent(
	t *testing.T,
	group string,
	constructor func() []toolcontract.ToolDefinition,
) {
	t.Helper()

	count := len(constructor())
	for mutatedIndex := 0; mutatedIndex < count; mutatedIndex++ {
		tools := constructor()
		before := make([][]byte, len(tools))
		for i := range tools {
			before[i] = marshalServiceDefinition(t, tools[i])
		}

		tools[mutatedIndex].Name = "mutated"
		mutateServiceSchema(tools[mutatedIndex].InputSchema)
		for siblingIndex := range tools {
			if siblingIndex == mutatedIndex {
				continue
			}
			if got := marshalServiceDefinition(t, tools[siblingIndex]); !bytes.Equal(got, before[siblingIndex]) {
				t.Fatalf("%s tool %d mutation changed sibling %d", group, mutatedIndex, siblingIndex)
			}
		}
	}
}

func mutateServiceSchema(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			mutateServiceSchema(child)
		}
		typed["__mutation__"] = true
	case []any:
		for i := range typed {
			mutateServiceSchema(typed[i])
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

func requireServiceSchemaMap(t *testing.T, name string, value any) map[string]any {
	t.Helper()

	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s schema type = %T, want map[string]any", name, value)
	}
	return schema
}

func marshalServiceDefinitions(t *testing.T, tools []toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal service definitions: %v", err)
	}
	return encoded
}

func marshalServiceDefinition(t *testing.T, tool toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal service definition: %v", err)
	}
	return encoded
}
