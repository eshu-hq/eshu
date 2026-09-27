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
	if got := observabilityCoverageTools(); !reflect.DeepEqual(got, wantCoverage) {
		t.Fatal("root observabilityCoverageTools wrapper drifted from observability/coverage.Tools")
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "count_secrets_iam_posture" {
			continue
		}
		coverageStart := start + 1
		supplyStart := coverageStart + len(wantCoverage)
		if supplyStart >= len(tools) {
			break
		}
		if got := tools[supplyStart].Name; got != "get_vulnerability_scanner_read_contract" {
			break
		}
		if got := tools[coverageStart:supplyStart]; !reflect.DeepEqual(got, wantCoverage) {
			t.Fatal("ReadOnlyTools observability coverage definitions drifted from observability/coverage.Tools")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered secrets/coverage/supply boundary")
}
