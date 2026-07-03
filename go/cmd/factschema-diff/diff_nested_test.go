// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// TestCompareSchemasDetectsNarrowedNestedAdditionalProperties proves the gate
// fails when a map-valued field's nested value type narrows — for example
// tags going from map[string]string (additionalProperties.type "string") to
// map[string]integer. Both sides unmarshal as a top-level object, so only a
// recursive comparison of the nested additionalProperties schema catches it.
func TestCompareSchemasDetectsNarrowedNestedAdditionalProperties(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "integer"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationNarrowedType && v.Field == "tags" {
			found = true
			if !strings.Contains(v.String(), "tags") || !strings.Contains(v.String(), "narrow") {
				t.Fatalf("violation message = %q, want field name and violation type", v.String())
			}
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a narrowed-type violation naming tags (nested additionalProperties)", violations)
	}
}

// TestCompareSchemasDetectsNarrowedArrayItems proves the gate fails when an
// array field's item type narrows (items.type string -> integer), which the
// top-level Type/Enum comparison alone cannot see.
func TestCompareSchemasDetectsNarrowedArrayItems(t *testing.T) {
	t.Parallel()

	base := `{
  "properties": {
    "account_id": {"type": "string"},
    "zones": {"type": "array", "items": {"type": "string"}}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "zones": {"type": "array", "items": {"type": "integer"}}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("arr.schema.json", []byte(base), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationNarrowedType && v.Field == "zones" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a narrowed-type violation naming zones (array items)", violations)
	}
}

// TestCompareSchemasAllowsNestedNarrowingWithMajorBump proves the major-bump
// escape hatch still suppresses a nested-narrowing violation.
func TestCompareSchemasAllowsNestedNarrowingWithMajorBump(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "integer"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 2)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}
	if len(violations) != 0 {
		t.Fatalf("compareSchemas() violations = %+v, want none when the version marker takes a major bump", violations)
	}
}

// TestCompareSchemasIgnoresNestedAdditionalPropertiesBoolForm proves parsing
// tolerates a property whose additionalProperties is the bool form (false)
// rather than a schema object — the two shapes JSON Schema allows must both
// parse without error, and an unchanged bool-form field is not a violation.
func TestCompareSchemasIgnoresNestedAdditionalPropertiesBoolForm(t *testing.T) {
	t.Parallel()

	schema := `{
  "properties": {
    "account_id": {"type": "string"},
    "meta": {"type": "object", "additionalProperties": false}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("bool.schema.json", []byte(schema), []byte(schema))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil (bool-form additionalProperties must parse)", err)
	}
	if len(violations) != 0 {
		t.Fatalf("compareSchemas() violations = %+v, want none for an unchanged schema", violations)
	}
}
