// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func s3InternetExposureRows() []map[string]any {
	return []map[string]any{{
		"uid":              "cloud-resource-1",
		"state":            "unknown",
		"internet_exposed": nil,
		"reason":           "policy_public_grant_unknown",
		"source_fact_id":   "fact-posture-1",
	}}
}

func TestS3InternetExposureNodeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, 0)

	if err := writer.WriteS3InternetExposureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestS3InternetExposureNodeWriterMatchesExistingCloudResourceAndSetsProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, 0)

	if err := writer.WriteS3InternetExposureNodes(context.Background(), s3InternetExposureRows(), "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes returned error: %v", err)
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
		t.Fatalf("s3 internet exposure must never fabricate nodes with MERGE:\n%s", cypher)
	}
	for _, want := range []string{
		"resource.s3_internet_exposure_state = row.state",
		"resource.s3_internet_exposed = row.internet_exposed",
		"resource.s3_internet_exposure_reason = row.reason",
		"resource.s3_internet_exposure_scope_id = row.scope_id",
		"resource.s3_internet_exposure_generation_id = row.generation_id",
		"resource.s3_internet_exposure_evidence_source = row.evidence_source",
		"resource.s3_internet_exposure_source_fact_id = row.source_fact_id",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got := rows[0]["internet_exposed"]; got != nil {
		t.Fatalf("internet_exposed row value = %v, want nil so unknown removes the bool property", got)
	}
	if got, want := rows[0]["scope_id"], "scope-1"; got != want {
		t.Fatalf("scope_id = %v, want %v", got, want)
	}
}

func TestS3InternetExposureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, 0)

	if err := writer.RetractS3InternetExposureNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("RetractS3InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (resource:CloudResource)") {
		t.Fatalf("retract must match CloudResource nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.s3_internet_exposure_scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by reducer-owned scope property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "resource.s3_internet_exposure_evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "REMOVE resource.s3_internet_exposure_state") {
		t.Fatalf("retract must remove reducer-owned properties:\n%s", cypher)
	}
	if strings.Contains(cypher, "DELETE resource") {
		t.Fatalf("retract must never delete CloudResource nodes:\n%s", cypher)
	}
}
