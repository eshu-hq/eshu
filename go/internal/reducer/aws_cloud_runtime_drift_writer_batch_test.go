// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestPostgresAWSCloudRuntimeDriftWriterPersistsBatchedFacts proves
// WriteAWSCloudRuntimeDriftFindings upserts candidates through the shared
// reducerBatchInsertVersionedFacts bulk-insert path (issue #5317) rather than
// one ExecContext per candidate, and that the decoded rows carry
// byte-identical content — including the governed schema_version — to what
// the retired per-row canonicalVersionedReducerFactInsertQuery loop produced:
// the row-building helpers (awsCloudRuntimeDriftFactID/StableFactKey/
// TypedPayload) are unchanged, only the ExecContext call site moved.
func TestPostgresAWSCloudRuntimeDriftWriterPersistsBatchedFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresAWSCloudRuntimeDriftWriter{DB: db}

	write := AWSCloudRuntimeDriftWrite{
		IntentID:     "intent-aws-drift-batch",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws-drift-batch",
		SourceSystem: "aws",
		Cause:        "aws runtime facts observed",
		Candidates: []model.Candidate{
			{
				ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:orphan:orphaned_cloud_resource",
				Kind:           rules.AWSCloudRuntimeDriftPackName,
				CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:orphan",
				Confidence:     1,
				State:          model.CandidateStateAdmitted,
				Evidence: []model.EvidenceAtom{
					{EvidenceType: cloudruntime.EvidenceTypeFindingKind, Key: "finding_kind", Value: string(cloudruntime.FindingKindOrphanedCloudResource)},
				},
			},
			{
				ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:unmanaged:unmanaged_cloud_resource",
				Kind:           rules.AWSCloudRuntimeDriftPackName,
				CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
				Confidence:     1,
				State:          model.CandidateStateAdmitted,
				Evidence: []model.EvidenceAtom{
					{EvidenceType: cloudruntime.EvidenceTypeFindingKind, Key: "finding_kind", Value: string(cloudruntime.FindingKindUnmanagedCloudResource)},
				},
			},
		},
	}

	result, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	// One ExecContext call for two candidates: proves the batched path
	// replaced the retired per-candidate loop.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert)", got, want)
	}
	rows := decodeBatchedVersionedFactCalls(t, db.execs)
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}

	for i, candidate := range write.Candidates {
		row := rows[i]
		canonicalID := canonicalAWSCloudRuntimeDriftID(write, candidate)
		if got, want := row.FactID, awsCloudRuntimeDriftFactID(write, candidate); got != want {
			t.Fatalf("row %d FactID = %q, want %q (byte-identical to per-row loop)", i, got, want)
		}
		if got, want := row.StableFactKey, awsCloudRuntimeDriftStableFactKey(write, candidate); got != want {
			t.Fatalf("row %d StableFactKey = %q, want %q", i, got, want)
		}
		if got, want := row.FactKind, awsCloudRuntimeDriftFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		if got, want := row.SchemaVersion, facts.ReducerDerivedSchemaVersionV1; got != want {
			t.Fatalf("row %d SchemaVersion = %q, want %q (governed reducer-derived fact must keep its schema version)", i, got, want)
		}
		wantTypedPayload, err := factschema.EncodeReducerAWSCloudRuntimeDriftFinding(
			awsCloudRuntimeDriftTypedPayload(write, candidate, canonicalID),
		)
		if err != nil {
			t.Fatalf("encode expected payload: %v", err)
		}
		wantPayload, err := json.Marshal(wantTypedPayload)
		if err != nil {
			t.Fatalf("marshal expected payload: %v", err)
		}
		if got, want := string(row.Payload), string(wantPayload); got != want {
			t.Fatalf("row %d Payload = %s, want %s (byte-identical to per-row loop)", i, got, want)
		}
	}
}

// TestWriteAWSCloudRuntimeDriftFindingsBoundedExecCount guards issue #5317: N
// candidates must be persisted in O(N/batchSize) bulk inserts rather than one
// ExecContext per candidate.
func TestWriteAWSCloudRuntimeDriftFindingsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const candidateCount = 1500
	candidates := make([]model.Candidate, candidateCount)
	for i := range candidates {
		arn := fmt.Sprintf("arn:aws:lambda:us-east-1:123456789012:function:orphan-%d", i)
		candidates[i] = model.Candidate{
			ID:             fmt.Sprintf("aws_cloud_runtime_drift:%s:%s", arn, cloudruntime.FindingKindOrphanedCloudResource),
			Kind:           rules.AWSCloudRuntimeDriftPackName,
			CorrelationKey: arn,
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{EvidenceType: cloudruntime.EvidenceTypeFindingKind, Key: "finding_kind", Value: string(cloudruntime.FindingKindOrphanedCloudResource)},
			},
		}
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresAWSCloudRuntimeDriftWriter{DB: db}

	result, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), AWSCloudRuntimeDriftWrite{
		IntentID:     "intent-aws-drift-batch",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-batch",
		SourceSystem: "aws",
		Candidates:   candidates,
	})
	if err != nil {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() error = %v", err)
	}
	if got, want := result.CanonicalWrites, candidateCount; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	wantExecs := expectedBatchedExecCount(candidateCount)
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d candidates, want %d (bounded batched inserts)", got, candidateCount, wantExecs)
	}
	if rows := decodeBatchedVersionedFactCalls(t, db.execs); len(rows) != candidateCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), candidateCount)
	}
}
