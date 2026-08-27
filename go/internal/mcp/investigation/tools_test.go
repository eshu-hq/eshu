// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigationtools

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestToolsPreserveInvestigationRegistrationContract(t *testing.T) {
	t.Parallel()

	workflowTools := WorkflowTools()
	packetTools := PacketTools()
	requireToolNames(t, "workflow", workflowTools, []string{
		"list_investigation_workflows",
		"resolve_investigation_workflow",
	})
	requireToolNames(t, "packet", packetTools, []string{
		"export_supply_chain_impact_packet",
		"export_deployable_unit_packet",
		"export_cloud_runtime_drift_packet",
	})

	tools := make([]toolcontract.ToolDefinition, 0, len(workflowTools)+len(packetTools))
	tools = append(tools, workflowTools...)
	tools = append(tools, packetTools...)
	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal investigation tools: %v", err)
	}
	if got, want := len(encoded), 4824; got != want {
		t.Fatalf("serialized investigation definitions length = %d, want %d", got, want)
	}
	const wantDefinitionsHash = "393e7901eda034e7a18a8a043895e2cde337dc0b103f994126bcc7ae972b8a82"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("investigation definitions hash = %s, want %s", got, wantDefinitionsHash)
	}

	requireInvestigationSchemaTypes(t, workflowTools, packetTools)
}

func TestToolsReturnIndependentDefinitions(t *testing.T) {
	t.Parallel()

	first := combinedInvestigationTools()
	second := combinedInvestigationTools()
	encodedSecond := marshalDefinitions(t, second)

	for i := range first {
		first[i].Name = "mutated"
		mutateInvestigationSchema(first[i].InputSchema)
	}
	if got := marshalDefinitions(t, second); !bytes.Equal(got, encodedSecond) {
		t.Fatal("investigation constructors share slice or nested schema storage across calls")
	}
}

func TestToolsKeepSiblingDefinitionsIndependent(t *testing.T) {
	t.Parallel()

	assertSiblingDefinitionsIndependent(t, "workflow", WorkflowTools)
	assertSiblingDefinitionsIndependent(t, "packet", PacketTools)
}

func requireToolNames(
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

func requireInvestigationSchemaTypes(
	t *testing.T,
	workflowTools []toolcontract.ToolDefinition,
	packetTools []toolcontract.ToolDefinition,
) {
	t.Helper()

	for i, tool := range workflowTools {
		schema := requireSchemaMap(t, fmt.Sprintf("workflow tool %d", i), tool.InputSchema)
		requireSchemaMap(t, fmt.Sprintf("workflow tool %d properties", i), schema["properties"])
	}
	resolveSchema := requireSchemaMap(t, "workflow resolve", workflowTools[1].InputSchema)
	if _, ok := resolveSchema["required"].([]string); !ok {
		t.Fatalf("workflow resolve required type = %T, want []string", resolveSchema["required"])
	}
	resolveProperties := requireSchemaMap(t, "workflow resolve properties", resolveSchema["properties"])
	inputs := requireSchemaMap(t, "workflow inputs", resolveProperties["inputs"])
	requireSchemaMap(t, "workflow inputs additionalProperties", inputs["additionalProperties"])
	missingEvidence := requireSchemaMap(t, "workflow missing_evidence", resolveProperties["missing_evidence"])
	requireSchemaMap(t, "workflow missing_evidence items", missingEvidence["items"])

	for i, tool := range packetTools {
		schema := requireSchemaMap(t, fmt.Sprintf("packet tool %d", i), tool.InputSchema)
		properties := requireSchemaMap(t, fmt.Sprintf("packet tool %d properties", i), schema["properties"])
		requireSchemaMap(t, fmt.Sprintf("packet tool %d max_source_facts", i), properties["max_source_facts"])
	}
	deployableSchema := requireSchemaMap(t, "deployable packet", packetTools[1].InputSchema)
	if _, ok := deployableSchema["required"].([]string); !ok {
		t.Fatalf("deployable packet required type = %T, want []string", deployableSchema["required"])
	}
	driftSchema := requireSchemaMap(t, "drift packet", packetTools[2].InputSchema)
	driftProperties := requireSchemaMap(t, "drift packet properties", driftSchema["properties"])
	provider := requireSchemaMap(t, "drift packet provider", driftProperties["provider"])
	if _, ok := provider["enum"].([]string); !ok {
		t.Fatalf("drift packet provider enum type = %T, want []string", provider["enum"])
	}
}

func requireSchemaMap(t *testing.T, name string, value any) map[string]any {
	t.Helper()

	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s schema type = %T, want map[string]any", name, value)
	}
	return schema
}

func combinedInvestigationTools() []toolcontract.ToolDefinition {
	workflowTools := WorkflowTools()
	packetTools := PacketTools()
	tools := make([]toolcontract.ToolDefinition, 0, len(workflowTools)+len(packetTools))
	tools = append(tools, workflowTools...)
	return append(tools, packetTools...)
}

func assertSiblingDefinitionsIndependent(
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
			before[i] = marshalDefinition(t, tools[i])
		}

		tools[mutatedIndex].Name = "mutated"
		mutateInvestigationSchema(tools[mutatedIndex].InputSchema)
		for siblingIndex := range tools {
			if siblingIndex == mutatedIndex {
				continue
			}
			if got := marshalDefinition(t, tools[siblingIndex]); !bytes.Equal(got, before[siblingIndex]) {
				t.Fatalf(
					"%s tool %d mutation changed sibling %d",
					group,
					mutatedIndex,
					siblingIndex,
				)
			}
		}
	}
}

func mutateInvestigationSchema(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			mutateInvestigationSchema(child)
		}
		typed["__mutation__"] = true
	case []any:
		for i := range typed {
			mutateInvestigationSchema(typed[i])
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

func marshalDefinitions(t *testing.T, tools []toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal investigation definitions: %v", err)
	}
	return encoded
}

func marshalDefinition(t *testing.T, tool toolcontract.ToolDefinition) []byte {
	t.Helper()

	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal investigation definition: %v", err)
	}
	return encoded
}
