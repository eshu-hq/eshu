// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAWSRelationshipMaterializationQuarantinesMissingAccountID is the flagship
// regression test for issue #4568 (Contract System v1 §3.2). It proves the
// accuracy guarantee the typed-decode migration exists to protect AND the
// per-fact isolation contract the migration ships (P1-A): an aws_resource fact
// missing its required account_id key is QUARANTINED as a visible input_invalid
// dead-letter — never silently indexed under a uid computed with an empty-string
// account segment — while every VALID fact in the same batch still projects and
// the handler Acks so one malformed fact never stalls the scope generation.
//
// Before the migration this behavior was impossible: buildCloudResourceJoinIndex
// read account_id with payloadString, which returns "" for the absent key, and
// cloudResourceUID("", region, type, id) yielded a wrong-but-plausible identity;
// the relationship then resolved against that empty-account node and the handler
// wrote an edge to it — the silent wrong graph truth the Life Motto ranks as the
// worst failure.
//
// After the migration the join index decodes each aws_resource fact through
// factschema.DecodeAWSResource; the malformed fact yields a classified
// *factschema.DecodeError that partitionDecodeFailures routes to a per-fact
// quarantine. The handler records it (metric + structured log + the
// input_invalid_facts SubSignal) and continues, so the batch's valid
// source→target edge still materializes and no edge references an empty-account
// uid.
func TestAWSRelationshipMaterializationQuarantinesMissingAccountID(t *testing.T) {
	t.Parallel()

	// A source resource fact whose required account_id key is ABSENT (not merely
	// empty): the exact malformed input the AC names. Everything else is present
	// so the ONLY reason to quarantine the fact is the missing required field.
	malformedSource := awsResourceEnvelope(map[string]any{
		// "account_id" intentionally absent.
		"region":        "us-east-1",
		"resource_type": "aws_lambda_function",
		"resource_id":   "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"arn":           "arn:aws:lambda:us-east-1:111122223333:function:fn",
	})
	// A fully valid, independent source→target relationship that must still
	// project despite the malformed fact sharing the batch. This is the isolation
	// half of the contract: valid facts are unaffected by a poisoned sibling.
	validSource := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:good", "arn:aws:lambda:us-east-1:111122223333:function:good")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:good",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:good",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{malformedSource, validSource, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), awsRelationshipIntent())
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed aws_resource fact must be quarantined per-fact, not fail the whole intent", err)
	}

	// The malformed fact must be counted as an input_invalid quarantine in the
	// Result SubSignals so the operator sees it on the per-intent signal (each
	// quarantined fact is also on the eshu_dp_reducer_input_invalid_facts_total
	// counter and a structured error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-account_id fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID relationship must still materialize its edge: isolation
	// means a poisoned sibling never suppresses valid graph truth.
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1; the valid source→target relationship must still project despite the quarantined fact", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("writtenRows = %d, want 1; exactly the one valid edge must be written", len(writer.writtenRows))
	}

	// No edge may reference a uid computed with an empty-string account segment —
	// the accuracy guarantee. The malformed fact's empty-account uid must never
	// appear as a source or target in any written edge.
	emptyAccountUID := cloudResourceUID("", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn")
	for _, row := range writer.writtenRows {
		if row["source_uid"] == emptyAccountUID || row["target_uid"] == emptyAccountUID {
			t.Fatalf("written edge references the empty-account uid %q; a quarantined fact must never produce graph identity", emptyAccountUID)
		}
	}
}
