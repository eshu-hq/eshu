// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	investigationtools "github.com/eshu-hq/eshu/go/internal/mcp/investigation"
)

func TestReadOnlyToolsKeepsInvestigationRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantWorkflows := investigationtools.WorkflowTools()
	if got, want := len(wantWorkflows), 2; got != want {
		t.Fatalf("investigation.WorkflowTools length = %d, want %d", got, want)
	}
	if got := investigationWorkflowTools(); !reflect.DeepEqual(got, wantWorkflows) {
		t.Fatal("root investigationWorkflowTools wrapper drifted from investigation.WorkflowTools")
	}
	wantPackets := investigationtools.PacketTools()
	if got, want := len(wantPackets), 3; got != want {
		t.Fatalf("investigation.PacketTools length = %d, want %d", got, want)
	}
	if got := investigationPacketTools(); !reflect.DeepEqual(got, wantPackets) {
		t.Fatal("root investigationPacketTools wrapper drifted from investigation.PacketTools")
	}
	wantInvestigation := make([]ToolDefinition, 0, len(wantWorkflows)+len(wantPackets))
	wantInvestigation = append(wantInvestigation, wantWorkflows...)
	wantInvestigation = append(wantInvestigation, wantPackets...)

	wantNames := []string{
		"resolve_query_playbook",
		"list_investigation_workflows",
		"resolve_investigation_workflow",
		"export_supply_chain_impact_packet",
		"export_deployable_unit_packet",
		"export_cloud_runtime_drift_packet",
		"list_semantic_documentation_observations",
	}
	tools := ReadOnlyTools()
	for start := range tools {
		end := start + len(wantNames)
		if end > len(tools) || tools[start].Name != wantNames[0] {
			continue
		}
		for i, want := range wantNames {
			if got := tools[start+i].Name; got != want {
				t.Fatalf("ReadOnlyTools investigation boundary name[%d] = %q, want %q", i, got, want)
			}
		}
		if got := tools[start+1 : end-1]; !reflect.DeepEqual(got, wantInvestigation) {
			t.Fatal("ReadOnlyTools investigation definitions drifted from investigation constructors")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered playbook/investigation/semantic boundary")
}
