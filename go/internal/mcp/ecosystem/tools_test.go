// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecosystemtools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveEcosystemRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := Tools()
	wantNames := []string{
		"get_ecosystem_overview",
		"get_graph_summary_packet",
		"investigate_contract_impact",
		"trace_deployment_chain",
		"investigate_deployment_config",
		"find_blast_radius",
		"find_infra_resources",
		"investigate_resource",
		"analyze_infra_relationships",
		"get_repo_summary",
		"get_repo_context",
		"get_relationship_evidence",
		"analyze_pre_change_impact",
		"plan_developer_change",
		"list_package_registry_packages",
		"list_package_registry_versions",
		"get_repo_story",
		"get_repository_coverage",
		"trace_resource_to_code",
		"explain_dependency_path",
		"find_change_surface",
		"investigate_change_surface",
		"compare_environments",
	}
	requireEcosystemToolNames(t, tools, wantNames)
	for i, tool := range tools {
		requireEcosystemSchemaTypes(t, fmt.Sprintf("tool %d (%s)", i, tool.Name), tool.InputSchema)
	}

	encoded := marshalEcosystemDefinitions(t, tools)
	if got, want := len(encoded), 20585; got != want {
		t.Fatalf("serialized ecosystem definitions length = %d, want %d", got, want)
	}
	const wantDefinitionsHash = "8dcb60e87971b24d53f1be68ccbc7657faa03a1378f34d92990833db0ab0284f"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("ecosystem definitions hash = %s, want %s", got, wantDefinitionsHash)
	}
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := Tools()
	second := Tools()
	encodedSecond := marshalEcosystemDefinitions(t, second)

	for i := range first {
		first[i].Name = "mutated"
		mutateEcosystemSchema(first[i].InputSchema)
	}
	if got := marshalEcosystemDefinitions(t, second); !bytes.Equal(got, encodedSecond) {
		t.Fatal("ecosystem constructors share slice or nested schema storage across calls")
	}
}

func TestToolsKeepSiblingDefinitionsIndependent(t *testing.T) {
	t.Parallel()

	count := len(Tools())
	for mutatedIndex := 0; mutatedIndex < count; mutatedIndex++ {
		tools := Tools()
		before := make([][]byte, len(tools))
		for i := range tools {
			before[i] = marshalEcosystemDefinition(t, tools[i])
		}

		tools[mutatedIndex].Name = "mutated"
		mutateEcosystemSchema(tools[mutatedIndex].InputSchema)
		for siblingIndex := range tools {
			if siblingIndex == mutatedIndex {
				continue
			}
			if got := marshalEcosystemDefinition(t, tools[siblingIndex]); !bytes.Equal(got, before[siblingIndex]) {
				t.Fatalf("ecosystem tool %d mutation changed sibling %d", mutatedIndex, siblingIndex)
			}
		}
	}
}

func requireEcosystemToolNames(
	t *testing.T,
	tools []toolcontract.ToolDefinition,
	wantNames []string,
) {
	t.Helper()

	if got, want := len(tools), len(wantNames); got != want {
		t.Fatalf("ecosystem tool count = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		if got := tools[i].Name; got != want {
			t.Fatalf("ecosystem tool %d name = %q, want %q", i, got, want)
		}
	}
}

func requireEcosystemSchemaTypes(t *testing.T, path string, value any) {
	t.Helper()

	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s schema type = %T, want map[string]any", path, value)
	}
	requireEcosystemSchemaValueTypes(t, path, schema)

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties type = %T, want map[string]any", path, schema["properties"])
	}
	for name, property := range properties {
		if _, ok := property.(map[string]any); !ok {
			t.Fatalf("%s property %q type = %T, want map[string]any", path, name, property)
		}
	}
	if required, present := schema["required"]; present {
		if _, ok := required.([]string); !ok {
			t.Fatalf("%s required type = %T, want []string", path, required)
		}
	}
}

func requireEcosystemSchemaValueTypes(t *testing.T, path string, value any) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			requireEcosystemSchemaValueTypes(t, path+"."+key, child)
		}
	case []string, string, bool, int:
		return
	default:
		t.Fatalf("%s value type = %T, want MCP schema map, []string, string, bool, or int", path, value)
	}
}

func mutateEcosystemSchema(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			mutateEcosystemSchema(child)
		}
		typed["__mutation__"] = true
	case []any:
		for i := range typed {
			mutateEcosystemSchema(typed[i])
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

func marshalEcosystemDefinitions(t *testing.T, tools []toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal ecosystem definitions: %v", err)
	}
	return encoded
}

func marshalEcosystemDefinition(t *testing.T, tool toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal ecosystem definition: %v", err)
	}
	return encoded
}
