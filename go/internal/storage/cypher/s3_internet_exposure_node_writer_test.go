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
	writer := NewS3InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteS3InternetExposureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestS3InternetExposureNodeWriterMergesConfirmedExistingCloudResourceAndSetsProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

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
	// Issue #5652: a bare-MATCH-anchored UNWIND SET silently drops its write
	// on the pinned production NornicDB image, so the shipped statement
	// anchors with MERGE. Never-create is enforced in Go instead: see
	// TestS3InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource.
	if !strings.Contains(cypher, "MERGE (resource:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE-anchor on the CloudResource uid:\n%s", cypher)
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

// TestS3InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource proves
// the never-create contract survives the MATCH->MERGE fix (issue #5652).
func TestS3InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"cloud-resource-s3-exists": true}}
	writer := NewS3InternetExposureNodeWriter(executor, reader, 0)

	rows := []map[string]any{
		{"uid": "cloud-resource-s3-exists", "state": "exposed", "internet_exposed": true, "reason": "policy", "source_fact_id": "fact-1"},
		{"uid": "cloud-resource-s3-missing", "state": "exposed", "internet_exposed": true, "reason": "policy", "source_fact_id": "fact-2"},
	}
	if err := writer.WriteS3InternetExposureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	writtenRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if len(writtenRows) != 1 {
		t.Fatalf("len(writtenRows) = %d, want 1 (only the confirmed-existing uid)", len(writtenRows))
	}
	if got := writtenRows[0]["uid"]; got != "cloud-resource-s3-exists" {
		t.Fatalf("writtenRows[0][uid] = %v, want cloud-resource-s3-exists", got)
	}
}

// TestS3InternetExposureNodeWriterRequiresReader proves the writer fails fast
// instead of silently defaulting to bare-MATCH semantics without a reader.
func TestS3InternetExposureNodeWriterRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, nil, 0)

	if err := writer.WriteS3InternetExposureNodes(context.Background(), s3InternetExposureRows(), "scope-1", "gen-1", "reducer/s3-internet-exposure"); err == nil {
		t.Fatal("WriteS3InternetExposureNodes() error = nil, want error for nil reader")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when reader is nil", len(executor.calls))
	}
}

func TestS3InternetExposureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

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
