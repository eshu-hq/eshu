// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestCompareSchemasPassesWhenTypeGainsNull proves that widening a field's type
// to accept null (["string"] -> ["string","null"], the nullability the
// factschema generator adds so an optional field accepts the explicit null the
// collectors emit) is NOT a breaking change: the accepted value space grew, it
// did not shrink.
func TestCompareSchemasPassesWhenTypeGainsNull(t *testing.T) {
	t.Parallel()

	base := `{
  "properties": {
    "account_id": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": true,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "name": {"type": ["string", "null"]}
  },
  "additionalProperties": true,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(base), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}
	if len(violations) != 0 {
		t.Fatalf("compareSchemas() violations = %+v, want none; adding null widens the type and is non-breaking", violations)
	}
}

// TestCompareSchemasDetectsTypeDroppedFromUnion proves the inverse: dropping a
// type the baseline accepted (["string","null"] -> ["string"]) narrows the
// value space and IS a breaking change without a major bump.
func TestCompareSchemasDetectsTypeDroppedFromUnion(t *testing.T) {
	t.Parallel()

	base := `{
  "properties": {
    "account_id": {"type": "string"},
    "name": {"type": ["string", "null"]}
  },
  "additionalProperties": true,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": true,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(base), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}
	found := false
	for _, v := range violations {
		if v.Kind == ViolationNarrowedType && v.Field == "name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a narrowed-type violation naming name (dropped null)", violations)
	}
}
