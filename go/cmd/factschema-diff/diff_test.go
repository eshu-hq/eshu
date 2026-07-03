// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

const baselineSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://eshu.dev/schemas/factschema/aws/v1/resource.schema.json",
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

// TestCompareSchemasDetectsRemovedRequiredField proves the gate fails when a
// required field present in the baseline is missing from the current schema
// and the schema's version marker (embedded in "title") did not take a major
// bump. This is fixture (a) from issue #4569's acceptance criteria.
func TestCompareSchemasDetectsRemovedRequiredField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}
	if len(violations) == 0 {
		t.Fatalf("compareSchemas() found no violations, want a removed-required-field violation")
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationRemovedRequiredField && v.Field == "resource_id" {
			found = true
			if !strings.Contains(v.String(), "resource_id") || !strings.Contains(v.String(), "removed") {
				t.Fatalf("violation message = %q, want field name and violation type", v.String())
			}
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a removed required field violation naming resource_id", violations)
	}
}

// TestCompareSchemasDetectsRenamedField proves the gate fails when a required
// field is renamed (the old name disappears from both properties and
// required, a differently-named field appears) without a major bump. This is
// fixture (b) from issue #4569's acceptance criteria.
func TestCompareSchemasDetectsRenamedField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_identifier": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_identifier", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationRemovedRequiredField && v.Field == "resource_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want the renamed field to surface as a removed required field (resource_id)", violations)
	}
}

// TestCompareSchemasDetectsNarrowedType proves the gate fails when a field's
// type is narrowed (string -> enum subset) without a major bump. This is
// fixture (c) from issue #4569's acceptance criteria.
func TestCompareSchemasDetectsNarrowedType(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string", "enum": ["us-east-1", "us-west-2"]},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
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
		if v.Kind == ViolationNarrowedType && v.Field == "region" {
			found = true
			if !strings.Contains(v.String(), "region") || !strings.Contains(v.String(), "narrow") {
				t.Fatalf("violation message = %q, want field name and violation type", v.String())
			}
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a narrowed type violation naming region", violations)
	}
}

// TestCompareSchemasPassesOnAdditiveOptionalField proves the gate passes
// (zero violations) when a new optional field is added and no existing
// required field, type, or name changes. This is fixture (d), the
// minor-compatible pass case.
func TestCompareSchemasPassesOnAdditiveOptionalField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"},
    "availability_zone": {"type": "string"}
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
	if len(violations) != 0 {
		t.Fatalf("compareSchemas() violations = %+v, want none for an additive optional field", violations)
	}
}

// TestCompareSchemasAllowsBreakingChangeWithMajorBump proves a schema that
// would otherwise be flagged as breaking (removed required field) passes
// when the title's version marker took a major bump, per Contract System v1
// §5's semver compatibility rule.
func TestCompareSchemasAllowsBreakingChangeWithMajorBump(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "region", "resource_type"],
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

// TestCompareSchemasDetectsWidenedRequired proves widening `required` (a
// previously-optional field becomes required) is itself a breaking change
// for existing collectors that never emitted it, and is flagged without a
// major bump.
func TestCompareSchemasDetectsWidenedRequired(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type", "name"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationWidenedRequired && v.Field == "name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a widened-required violation naming name", violations)
	}
}

// TestCompareSchemasDetectsRemovedOptionalField proves the gate fails when
// an OPTIONAL field is removed. Under additionalProperties:false a collector
// still emitting the dropped field now produces a schema-invalid payload, so
// removing any field — required or not — is a break without a major bump.
func TestCompareSchemasDetectsRemovedOptionalField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
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
		if v.Kind == ViolationRemovedField && v.Field == "name" {
			found = true
			if !strings.Contains(v.String(), "name") || !strings.Contains(v.String(), "removed") {
				t.Fatalf("violation message = %q, want field name and violation type", v.String())
			}
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want a removed-field violation naming the optional field name", violations)
	}
}

// TestCompareSchemasDetectsRenamedOptionalField proves renaming an optional
// field (name -> display_name) surfaces as a removed-field break on the old
// name, since under additionalProperties:false the old name is now rejected.
func TestCompareSchemasDetectsRenamedOptionalField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "display_name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
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
		if v.Kind == ViolationRemovedField && v.Field == "name" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want the renamed optional field to surface as a removed field (name)", violations)
	}
}

// TestCompareSchemasDetectsAddedRequiredField proves the gate fails when a
// brand-new REQUIRED field (absent from the baseline properties entirely) is
// added. Existing collectors that never emitted it now fail validation, so
// this is a break without a major bump.
func TestCompareSchemasDetectsAddedRequiredField(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "partition": {"type": "string"},
    "name": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type", "partition"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("aws_resource.v1.schema.json", []byte(baselineSchema), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}

	found := false
	for _, v := range violations {
		if v.Kind == ViolationAddedRequiredField && v.Field == "partition" {
			found = true
			if !strings.Contains(v.String(), "partition") {
				t.Fatalf("violation message = %q, want field name", v.String())
			}
		}
	}
	if !found {
		t.Fatalf("compareSchemas() violations = %+v, want an added-required-field violation naming partition", violations)
	}
}

// TestCompareSchemasAllowsOptionalRemovalWithMajorBump proves the major-bump
// escape hatch suppresses the new optional-removal violation class too.
func TestCompareSchemasAllowsOptionalRemovalWithMajorBump(t *testing.T) {
	t.Parallel()

	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "tags": {"additionalProperties": {"type": "string"}, "type": "object"}
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

// TestCompareSchemasAllowsOptionalRemovalWhenAdditionalPropertiesOpen proves
// the precision guard: when the baseline schema does NOT set
// additionalProperties:false, removing an optional field is not a break (a
// collector may still emit the field and the open schema accepts it).
// Required-field removal and added-required stay breaks regardless.
func TestCompareSchemasAllowsOptionalRemovalWhenAdditionalPropertiesOpen(t *testing.T) {
	t.Parallel()

	openBaseline := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "name": {"type": "string"}
  },
  "type": "object",
  "required": ["account_id", "resource_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`
	current := `{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"}
  },
  "type": "object",
  "required": ["account_id", "resource_id"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}`

	violations, err := compareSchemas("open.schema.json", []byte(openBaseline), []byte(current))
	if err != nil {
		t.Fatalf("compareSchemas() error = %v, want nil", err)
	}
	if len(violations) != 0 {
		t.Fatalf("compareSchemas() violations = %+v, want none when baseline additionalProperties is open", violations)
	}
}

// TestSchemaVersionMajorParsesTitleMarker proves the version marker parser
// extracts the major version number from a schema's "title" field, which is
// where sdk/go/factschema/internal/schemagen embeds "(schema version N)".
func TestSchemaVersionMajorParsesTitleMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  int
		ok    bool
	}{
		{"Eshu aws.resource Payload (schema version 1)", 1, true},
		{"Eshu aws.resource Payload (schema version 42)", 42, true},
		{"Eshu aws.resource Payload", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		got, ok := schemaVersionMajor(tt.title)
		if ok != tt.ok || got != tt.want {
			t.Errorf("schemaVersionMajor(%q) = (%d, %v), want (%d, %v)", tt.title, got, ok, tt.want, tt.ok)
		}
	}
}
