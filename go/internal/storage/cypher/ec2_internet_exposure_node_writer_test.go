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
	writer := NewEC2InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

	if err := writer.WriteEC2InternetExposureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2InternetExposureNodeWriterMergesConfirmedExistingCloudResourceAndSetsProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

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
	// Issue #5652: a bare-MATCH-anchored UNWIND SET silently drops its write
	// on the pinned production NornicDB image, so the shipped statement
	// anchors with MERGE. Never-create is enforced in Go instead: a row only
	// reaches this statement once filterRowsToExistingCloudResourceUIDs has
	// confirmed its uid already exists (see
	// TestEC2InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource
	// below), so MERGE always matches and never creates.
	if !strings.Contains(cypher, "MERGE (resource:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must MERGE-anchor on the CloudResource uid:\n%s", cypher)
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

// TestEC2InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource proves
// the never-create contract survives the MATCH->MERGE fix (issue #5652): a
// row whose uid the reader does not confirm as existing is dropped before it
// ever reaches the MERGE-anchored write statement, so the executor never sees
// it and cannot fabricate a node for it.
func TestEC2InternetExposureNodeWriterNeverCreatesUnconfirmedCloudResource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"cloud-resource-ec2-exists": true}}
	writer := NewEC2InternetExposureNodeWriter(executor, reader, 0)

	rows := []map[string]any{
		{"uid": "cloud-resource-ec2-exists", "state": "exposed", "internet_exposed": true, "reason": "sg", "source_fact_id": "fact-1"},
		{"uid": "cloud-resource-ec2-missing", "state": "exposed", "internet_exposed": true, "reason": "sg", "source_fact_id": "fact-2"},
	}
	if err := writer.WriteEC2InternetExposureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	writtenRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if len(writtenRows) != 1 {
		t.Fatalf("len(writtenRows) = %d, want 1 (only the confirmed-existing uid)", len(writtenRows))
	}
	if got := writtenRows[0]["uid"]; got != "cloud-resource-ec2-exists" {
		t.Fatalf("writtenRows[0][uid] = %v, want cloud-resource-ec2-exists", got)
	}
}

// TestEC2InternetExposureNodeWriterAllRowsUnconfirmedSkipsWriteEntirely proves
// the writer issues no statement at all when the existence read confirms
// nothing, rather than sending an empty-but-present batch.
func TestEC2InternetExposureNodeWriterAllRowsUnconfirmedSkipsWriteEntirely(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{}}
	writer := NewEC2InternetExposureNodeWriter(executor, reader, 0)

	if err := writer.WriteEC2InternetExposureNodes(context.Background(), ec2InternetExposureRows(), "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when no candidate uid is confirmed", len(executor.calls))
	}
}

// TestEC2InternetExposureNodeWriterRequiresReader proves the writer fails
// fast instead of silently defaulting to MATCH-only (bare-MATCH) semantics
// when no PostureExistenceReader is wired.
func TestEC2InternetExposureNodeWriterRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, nil, 0)

	if err := writer.WriteEC2InternetExposureNodes(context.Background(), ec2InternetExposureRows(), "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err == nil {
		t.Fatal("WriteEC2InternetExposureNodes() error = nil, want error for nil reader")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when reader is nil", len(executor.calls))
	}
}

func TestEC2InternetExposureNodeWriterRetractRemovesOnlyReducerOwnedProperties(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InternetExposureNodeWriter(executor, &echoingPostureExistenceReader{}, 0)

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
