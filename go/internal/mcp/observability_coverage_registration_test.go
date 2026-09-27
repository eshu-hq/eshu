// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	observabilitycoveragetools "github.com/eshu-hq/eshu/go/internal/mcp/observability/coverage"
)

func TestReadOnlyToolsKeepsObservabilityCoverageRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantCoverage := observabilitycoveragetools.Tools()
	gotWrapper := observabilityCoverageTools()
	if !reflect.DeepEqual(gotWrapper, wantCoverage) {
		t.Fatalf("root observabilityCoverageTools wrapper drifted from observability/coverage.Tools: got %+v, want %+v", gotWrapper, wantCoverage)
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "count_secrets_iam_posture" {
			continue
		}
		coverageStart := start + 1
		supplyStart := coverageStart + len(wantCoverage)
		if supplyStart >= len(tools) {
			t.Fatalf("ReadOnlyTools missing ordered secrets/coverage/supply boundary: window [%d:%d] leaves no trailing supply anchor in %d tools", coverageStart, supplyStart, len(tools))
		}
		if got := tools[supplyStart].Name; got != "get_vulnerability_scanner_read_contract" {
			t.Fatalf("ReadOnlyTools missing ordered secrets/coverage/supply boundary: tool[%d] = %q after the secrets anchor, want %q", supplyStart, got, "get_vulnerability_scanner_read_contract")
		}
		if got := tools[coverageStart:supplyStart]; !reflect.DeepEqual(got, wantCoverage) {
			t.Fatalf("ReadOnlyTools observability coverage definitions drifted from observability/coverage.Tools: got %+v, want %+v", got, wantCoverage)
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered secrets/coverage/supply boundary: secrets anchor count_secrets_iam_posture not found")
}
