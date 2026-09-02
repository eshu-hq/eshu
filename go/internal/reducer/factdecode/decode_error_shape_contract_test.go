// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factdecode

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// contractShapeError is a non-AWS attribute-shape failure behind the same
// AttributeShapeField contract *awsv1.AttributeShapeError satisfies. These
// tests lock the mechanism tier's family-agnostic boundary (issue #6358): the
// adapters must extract the field from ANY error behind the contract, never
// from one family's concrete type.
type contractShapeError struct{ field string }

func (e *contractShapeError) Error() string               { return "contract shape: field " + e.field }
func (e *contractShapeError) AttributeShapeField() string { return e.field }

func TestQuarantinedAttributeShapeFactExtractsFieldFromContract(t *testing.T) {
	env := facts.Envelope{FactID: "fact-1", FactKind: "aws_resource"}
	q := QuarantinedAttributeShapeFact(env, &contractShapeError{field: "attributes.encrypted"})
	if q.Field != "attributes.encrypted" {
		t.Fatalf("Field = %q, want %q (fell back to Error() text instead of the shape contract)", q.Field, "attributes.encrypted")
	}
	if q.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want input_invalid", q.Classification)
	}
}

func TestQuarantinedAttributeShapeFactStillExtractsAWSField(t *testing.T) {
	_, attrErr := awsv1.DecodeResourceEC2VolumeAttributes(awsv1.Resource{
		Attributes: map[string]any{"attributes": map[string]any{"encrypted": "yes"}},
	})
	if attrErr == nil {
		t.Fatal("DecodeResourceEC2VolumeAttributes accepted the malformed value; the fixture is wrong and must be fixed, not skipped")
	}
	env := facts.Envelope{FactID: "fact-1", FactKind: "aws_resource"}
	q := QuarantinedAttributeShapeFact(env, attrErr)
	if q.Field != "attributes.encrypted" {
		t.Fatalf("Field = %q, want %q", q.Field, "attributes.encrypted")
	}
}

func TestAttributeShapeAsFactDecodeErrorQuarantinesThroughSharedPath(t *testing.T) {
	env := facts.Envelope{FactID: "fact-1", FactKind: "aws_resource"}
	wrapped := AttributeShapeAsFactDecodeError("aws_resource", &contractShapeError{field: "attributes.encrypted"})
	q, ok, err := PartitionDecodeFailures(env, wrapped)
	if err != nil {
		t.Fatalf("PartitionDecodeFailures returned fatal err: %v", err)
	}
	if !ok {
		t.Fatal("PartitionDecodeFailures did not quarantine the adapted shape error")
	}
	if q.Field != "attributes.encrypted" {
		t.Fatalf("Field = %q, want %q", q.Field, "attributes.encrypted")
	}
	var decodeErr *FactDecodeError
	if !errors.As(wrapped, &decodeErr) {
		t.Fatal("adapted error is not a *FactDecodeError")
	}
}

func TestAttributeShapeFieldFallsBackToErrText(t *testing.T) {
	plain := errors.New("boom")
	if got := attributeShapeField(plain); got != "boom" {
		t.Fatalf("fallback = %q, want %q", got, "boom")
	}
	wrapped := fmt.Errorf("ctx: %w", &contractShapeError{field: "attributes.tags"})
	if got := attributeShapeField(wrapped); got != "attributes.tags" {
		t.Fatalf("wrapped contract field = %q, want %q", got, "attributes.tags")
	}
}
