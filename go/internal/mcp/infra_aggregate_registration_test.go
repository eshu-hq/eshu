// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	infrainventorytools "github.com/eshu-hq/eshu/go/internal/mcp/infra/inventory"
)

func TestReadOnlyToolsKeepsInfraResourceAggregateRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantAggregates := infrainventorytools.Tools()
	gotWrapper := infraResourceAggregateTools()
	if !reflect.DeepEqual(gotWrapper, wantAggregates) {
		t.Fatalf("root infraResourceAggregateTools wrapper drifted from infra/inventory.Tools: got %+v, want %+v", gotWrapper, wantAggregates)
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "compare_environments" {
			continue
		}
		aggregateStart := start + 1
		cloudStart := aggregateStart + len(wantAggregates)
		if cloudStart >= len(tools) {
			t.Fatalf("ReadOnlyTools missing ordered ecosystem/infra/cloud boundary: window [%d:%d] leaves no trailing cloud anchor in %d tools", aggregateStart, cloudStart, len(tools))
		}
		if got := tools[cloudStart].Name; got != "list_cloud_resource_inventory" {
			t.Fatalf("ReadOnlyTools missing ordered ecosystem/infra/cloud boundary: tool[%d] = %q after the ecosystem anchor, want %q", cloudStart, got, "list_cloud_resource_inventory")
		}
		if got := tools[aggregateStart:cloudStart]; !reflect.DeepEqual(got, wantAggregates) {
			t.Fatalf("ReadOnlyTools infra-inventory aggregate definitions drifted from infra/inventory.Tools: got %+v, want %+v", got, wantAggregates)
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered ecosystem/infra/cloud boundary: ecosystem anchor compare_environments not found")
}
