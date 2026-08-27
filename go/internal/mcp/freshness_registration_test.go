// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	freshnesstools "github.com/eshu-hq/eshu/go/internal/mcp/freshness"
)

func TestReadOnlyToolsKeepsFreshnessRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantFreshness := freshnesstools.Tools()
	if got := freshnessTools(); !reflect.DeepEqual(got, wantFreshness) {
		t.Fatal("root freshnessTools wrapper drifted from freshness.Tools")
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "derive_visualization_packet" {
			continue
		}
		freshnessStart := start + 1
		contextStart := freshnessStart + len(wantFreshness)
		if contextStart >= len(tools) {
			break
		}
		if got := tools[contextStart].Name; got != "resolve_entity" {
			break
		}
		if got := tools[freshnessStart:contextStart]; !reflect.DeepEqual(got, wantFreshness) {
			t.Fatal("ReadOnlyTools freshness definitions drifted from freshness.Tools")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered visualization/freshness/context boundary")
}
