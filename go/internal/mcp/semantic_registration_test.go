// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	semantictools "github.com/eshu-hq/eshu/go/internal/mcp/semantic"
)

func TestReadOnlyToolsKeepsSemanticRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantEvidence := semantictools.EvidenceTools()
	if got := semanticEvidenceTools(); !reflect.DeepEqual(got, wantEvidence) {
		t.Fatal("root semanticEvidenceTools wrapper drifted from semantic.EvidenceTools")
	}
	wantSearch := semantictools.SearchTools()
	if got := semanticSearchTools(); !reflect.DeepEqual(got, wantSearch) {
		t.Fatal("root semanticSearchTools wrapper drifted from semantic.SearchTools")
	}
	wantSemantic := append(wantEvidence, wantSearch...)

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "export_cloud_runtime_drift_packet" {
			continue
		}
		semanticStart := start + 1
		semanticEnd := semanticStart + len(wantSemantic)
		if semanticEnd >= len(tools) {
			break
		}
		if got := tools[semanticEnd].Name; got != "count_documentation_findings" {
			break
		}
		if got := tools[semanticStart:semanticEnd]; !reflect.DeepEqual(got, wantSemantic) {
			t.Fatal("ReadOnlyTools semantic definitions drifted from semantic constructors")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered investigation-packet/semantic/documentation-finding boundary")
}
