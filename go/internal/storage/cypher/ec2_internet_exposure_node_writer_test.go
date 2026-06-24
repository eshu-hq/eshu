// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func ec2InternetExposureRows() []map[string]any {
	return []map[string]any{{
		"uid":              "cloud-resource-ec2",
		"state":            "unknown",
		"internet_exposed": nil,
		"reason":           "eni_attachment_unresolved",
		"source_fact_id":   "fact-posture-1",
	}}
}

func TestEC2InternetExposureNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, 0)

	if err := writer.WriteEC2InternetExposureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2InternetExposureNodeWriterMatchesExistingCloudResourceAndSetsProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, 0)

	if err := writer.WriteEC2InternetExposureNodes(context.Background(), ec2InternetExposureRows(), "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (resource:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MATCH the existing CloudResource by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE") {
		t.Fatalf("ec2 internet exposure must never fabricate nodes with MERGE:\n%s", cypher)
	}
	for _, want := range []string{
		"resource.ec2_internet_exposure_state = row.state",
		"resource.ec2_internet_exposed = row.internet_exposed",
		"resource.ec2_internet_exposure_reason = row.reason",
		"resource.ec2_internet_exposure_scope_id = row.scope_id",
		"resource.ec2_internet_exposure_generation_id = row.generation_id",
		"resource.ec2_internet_exposure_evidence_source = row.evidence_source",
		"resource.ec2_internet_exposure_source_fact_id = row.source_fact_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got := rows[0]["internet_exposed"]; got != nil {
		t.Fatalf("internet_exposed row value = %v, want nil so unknown removes the bool property", got)
	}
	if _, leaked := rows[0]["public_ip_address"]; leaked {
		t.Fatalf("row leaked raw public IP: %v", rows[0])
	}
}

func TestEC2InternetExposureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, 0)

	if err := writer.RetractEC2InternetExposureNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("RetractEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (resource:CloudResource)") {
		t.Fatalf("retract must match CloudResource nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.ec2_internet_exposure_scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by reducer-owned scope property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.ec2_internet_exposure_evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE resource.ec2_internet_exposure_state") {
		t.Fatalf("retract must remove reducer-owned properties:\n%s", cypher)
	}
	if strings.Contains(cypher, "DELETE resource") {
		t.Fatalf("retract must never delete CloudResource nodes:\n%s", cypher)
	}
}
