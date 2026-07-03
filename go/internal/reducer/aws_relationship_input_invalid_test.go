// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// classifiedReducerFailure is the reducer-observable contract a self-classifying
// terminal failure implements: FailureClass() returns the durable failure_class
// string the Postgres dead-letter path writes verbatim (queueFailureMetadata
// reads it via errors.As). The reducer package cannot import go/internal/projector
// to reference its triage classes (that package imports reducer — an import
// cycle), so the test asserts on this reducer-side interface and the literal
// "input_invalid" value, which is byte-equal to projector.TriageClassInputInvalid
// and factschema.ClassificationInputInvalid by the same by-value contract the
// design mandates.
type classifiedReducerFailure interface {
	FailureClass() string
}

// TestAWSRelationshipMaterializationDeadLettersMissingAccountID is the flagship
// regression test for issue #4568 (Contract System v1 §3.2). It proves the
// accuracy guarantee the typed-decode migration exists to protect: an
// aws_resource fact missing its required account_id key must dead-letter as
// input_invalid, NEVER be silently indexed under a uid computed with an
// empty-string account segment.
//
// Before the migration this test FAILS: buildCloudResourceJoinIndex reads
// account_id with payloadString, which returns "" for the absent key, and
// cloudResourceUID("", region, type, id) yields a wrong-but-plausible identity;
// the relationship then resolves against that empty-account node and the handler
// writes an edge and returns success. That is the silent wrong graph truth the
// Life Motto ranks as the worst failure.
//
// After the migration the handler decodes the aws_resource fact through
// factschema.DecodeAWSResource at the join-index boundary; the missing
// account_id yields a *factschema.DecodeError the handler surfaces as a
// non-retryable, input_invalid-classified reducer error, so Handle returns an
// error that dead-letters (failure_class=input_invalid) and writes no edge.
func TestAWSRelationshipMaterializationDeadLettersMissingAccountID(t *testing.T) {
	t.Parallel()

	// A source resource fact whose required account_id key is ABSENT (not merely
	// empty): the exact malformed input the AC names. Everything else is present
	// so the ONLY reason to reject the fact is the missing required field.
	source := awsResourceEnvelope(map[string]any{
		// "account_id" intentionally absent.
		"region":        "us-east-1",
		"resource_type": "aws_lambda_function",
		"resource_id":   "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"arn":           "arn:aws:lambda:us-east-1:111122223333:function:fn",
	})
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	_, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err == nil {
		t.Fatal("Handle returned nil error for an aws_resource fact missing required account_id; the missing required field must dead-letter, not silently produce an empty-account graph identity")
	}

	// The surfaced error must self-classify as input_invalid so the durable
	// dead-letter row carries failure_class=input_invalid — the non-retryable
	// terminal bucket. Replaying the malformed fact unchanged can never succeed.
	var classified classifiedReducerFailure
	if !errors.As(err, &classified) {
		t.Fatalf("error %v (%T) does not implement FailureClass(); a decode failure must surface as a self-classifying reducer error so the dead-letter row is labeled input_invalid", err, err)
	}
	if got := classified.FailureClass(); got != "input_invalid" {
		t.Fatalf("FailureClass() = %q, want %q; the decode failure must map to the input_invalid triage class by value", got, "input_invalid")
	}

	// The error must NOT be retryable: a missing required field is terminal.
	if IsRetryable(err) {
		t.Fatal("IsRetryable(err) = true for a missing-required-field decode failure; input_invalid is terminal and must not retry")
	}

	// No edge may be written against a zero-value/empty-account identity.
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0; a malformed source fact must produce no edge, not an edge to an empty-account node", writer.writeCalls)
	}
}
