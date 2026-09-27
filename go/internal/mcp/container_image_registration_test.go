// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	containerimagetools "github.com/eshu-hq/eshu/go/internal/mcp/container/image"
)

func TestReadOnlyToolsKeepsContainerImageRegistrationPositions(t *testing.T) {
	t.Parallel()

	wantIdentity := containerimagetools.Tools()
	if got := containerImageTools(); !reflect.DeepEqual(got, wantIdentity) {
		t.Fatal("root containerImageTools wrapper drifted from container/image.Tools")
	}
	wantAggregates := containerimagetools.AggregateTools()
	if got := containerImageAggregateTools(); !reflect.DeepEqual(got, wantAggregates) {
		t.Fatal("root containerImageAggregateTools wrapper drifted from container/image.AggregateTools")
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "get_vulnerability_scanner_read_contract" {
			continue
		}
		identityStart := start + 1
		impactStart := identityStart + len(wantIdentity)
		if impactStart >= len(tools) {
			break
		}
		if got := tools[impactStart].Name; got != "list_supply_chain_impact_findings" {
			break
		}
		if got := tools[identityStart:impactStart]; !reflect.DeepEqual(got, wantIdentity) {
			t.Fatal("ReadOnlyTools container-image identity definitions drifted from container/image.Tools")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered scanner/image/impact boundary")
}

func TestReadOnlyToolsKeepsContainerImageAggregateRegistrationPositions(t *testing.T) {
	t.Parallel()

	wantAggregates := containerimagetools.AggregateTools()

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "get_security_alert_reconciliation_inventory" {
			continue
		}
		aggregateStart := start + 1
		sbomStart := aggregateStart + len(wantAggregates)
		if sbomStart >= len(tools) {
			break
		}
		if got := tools[sbomStart].Name; got != "count_sbom_attestation_attachments" {
			break
		}
		if got := tools[aggregateStart:sbomStart]; !reflect.DeepEqual(got, wantAggregates) {
			t.Fatal("ReadOnlyTools container-image aggregate definitions drifted from container/image.AggregateTools")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered alerts/image-aggregates/sbom boundary")
}
