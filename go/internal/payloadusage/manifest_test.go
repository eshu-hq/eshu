// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"strings"
	"testing"
)

func fixtureSeam() DecodeSeam {
	return DecodeSeam{
		FuncName:      "decodeAWSResource",
		FactKindConst: "FactKindAWSResource",
		StructPackage: "awsv1",
		StructName:    "Resource",
	}
}

func fixtureShape() map[string]StructShape {
	return map[string]StructShape{
		"awsv1.Resource": {
			Qualified: "awsv1.Resource",
			Fields: []StructField{
				{GoName: "AccountID", JSONName: "account_id", Required: true},
				{GoName: "ResourceID", JSONName: "resource_id", Required: true},
				{GoName: "Region", JSONName: "region", Required: true},
				{GoName: "ResourceType", JSONName: "resource_type", Required: true},
				{GoName: "ARN", JSONName: "arn", Required: false},
				{GoName: "Name", JSONName: "name", Required: false},
			},
		},
	}
}

// TestBuildManifestForwardSafe proves issue #4573's forward-safe acceptance
// criterion: a handler that decodes only fields the schema declares produces
// a manifest whose UsedFields is a subset of DeclaredFields, and CheckManifest
// against the same declared set reports zero violations.
func TestBuildManifestForwardSafe(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{fixtureSeam()}
	shapes := fixtureShape()
	usage := map[string][]FieldUsage{
		"decodeAWSResource": {
			{File: "aws_resource_materialization.go", GoFieldName: "AccountID"},
			{File: "aws_resource_materialization.go", GoFieldName: "ResourceType"},
			{File: "aws_relationship_join.go", GoFieldName: "ARN"},
		},
	}

	manifest := BuildManifest(seams, shapes, usage)
	if len(manifest.Kinds) != 1 {
		t.Fatalf("len(manifest.Kinds) = %d, want 1", len(manifest.Kinds))
	}
	kind := manifest.Kinds[0]
	if kind.FactKind != "FactKindAWSResource" {
		t.Fatalf("FactKind = %q, want FactKindAWSResource", kind.FactKind)
	}
	if len(kind.DeclaredFields) != 6 {
		t.Fatalf("len(DeclaredFields) = %d, want 6", len(kind.DeclaredFields))
	}
	if len(kind.UsedFields) != 3 {
		t.Fatalf("len(UsedFields) = %d, want 3 (AccountID, ARN, ResourceType)", len(kind.UsedFields))
	}

	declared := map[string]map[string]struct{}{
		"FactKindAWSResource": {
			"account_id": {}, "resource_id": {}, "region": {}, "resource_type": {}, "arn": {}, "name": {},
		},
	}
	violations := CheckManifest(manifest, declared)
	if len(violations) != 0 {
		t.Fatalf("CheckManifest() = %+v, want no violations for fields the schema declares", violations)
	}
}

// TestBuildManifestFailsOnUndeclaredField proves issue #4573's failing-first
// acceptance criterion: a handler that reads (or requires) a field absent
// from the declared schema field set produces exactly one violation naming
// the handler file, fact kind, and field.
func TestBuildManifestFailsOnUndeclaredField(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{fixtureSeam()}
	shapes := fixtureShape()
	usage := map[string][]FieldUsage{
		"decodeAWSResource": {
			{File: "aws_resource_materialization.go", GoFieldName: "AccountID"},
			{File: "some_new_handler.go", GoFieldName: "ResourceType"},
		},
	}

	manifest := BuildManifest(seams, shapes, usage)

	// Declared set for FactKindAWSResource is missing "resource_type" — a
	// schema that regressed (or a struct that gained a field with no
	// matching declared schema property), the exact drift this gate exists
	// to catch.
	declared := map[string]map[string]struct{}{
		"FactKindAWSResource": {"account_id": {}, "resource_id": {}, "region": {}, "arn": {}, "name": {}},
	}
	violations := CheckManifest(manifest, declared)
	if len(violations) != 1 {
		t.Fatalf("CheckManifest() = %+v, want exactly 1 violation for the undeclared resource_type read", violations)
	}
	v := violations[0]
	if v.FactKind != "FactKindAWSResource" {
		t.Errorf("Violation.FactKind = %q, want FactKindAWSResource", v.FactKind)
	}
	if v.File != "some_new_handler.go" {
		t.Errorf("Violation.File = %q, want some_new_handler.go", v.File)
	}
	if v.GoFieldName != "ResourceType" {
		t.Errorf("Violation.GoFieldName = %q, want ResourceType", v.GoFieldName)
	}
	msg := v.String()
	for _, want := range []string{"some_new_handler.go", "ResourceType", "FactKindAWSResource"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Violation.String() = %q, want it to name %q", msg, want)
		}
	}
}

// TestBuildManifestSkipsSeamWithoutShape proves a seam whose struct type has
// no parsed shape (a wiring gap the caller must fix) is silently excluded
// from the manifest rather than panicking or fabricating an empty entry, and
// that main.go's own verifyEverySeamProduced is what surfaces this as a hard
// error — this test only covers BuildManifest's own defensive behavior.
func TestBuildManifestSkipsSeamWithoutShape(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{fixtureSeam()}
	manifest := BuildManifest(seams, map[string]StructShape{}, nil)
	if len(manifest.Kinds) != 0 {
		t.Fatalf("len(manifest.Kinds) = %d, want 0 when no struct shape matches the seam", len(manifest.Kinds))
	}
}

// TestCheckManifestFallsBackToOwnDeclaredFields proves that a nil
// declaredOverride (or an entry missing for a kind) falls back to the
// manifest's own DeclaredFields, which can never itself produce a violation
// — used fields are always drawn from the same DeclaredFields set by
// construction (BuildManifest only records UsedFields whose GoName exists in
// shape.Fields).
func TestCheckManifestFallsBackToOwnDeclaredFields(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{fixtureSeam()}
	shapes := fixtureShape()
	usage := map[string][]FieldUsage{
		"decodeAWSResource": {{File: "x.go", GoFieldName: "AccountID"}},
	}
	manifest := BuildManifest(seams, shapes, usage)

	violations := CheckManifest(manifest, nil)
	if len(violations) != 0 {
		t.Fatalf("CheckManifest(nil) = %+v, want no violations (self-consistent fallback)", violations)
	}
}

// TestMergeRegistryPayloadSchemaFieldsIsAdditive proves the registry-ref
// merge widens rather than narrows the declared set, per #4573's "treat
// registry refs as an ADDITIVE optional input" scope boundary (issue #4570
// may not have landed payload_schema refs yet).
func TestMergeRegistryPayloadSchemaFieldsIsAdditive(t *testing.T) {
	t.Parallel()

	base := map[string]map[string]struct{}{
		"FactKindAWSResource": {"account_id": {}},
	}
	registry := map[string]map[string]struct{}{
		"FactKindAWSResource": {"resource_id": {}},
		"FactKindNewKind":     {"some_field": {}},
	}

	merged := MergeRegistryPayloadSchemaFields(base, registry)
	if _, ok := merged["FactKindAWSResource"]["account_id"]; !ok {
		t.Error("merged declared set lost the base account_id field; merge must be additive, not a replace")
	}
	if _, ok := merged["FactKindAWSResource"]["resource_id"]; !ok {
		t.Error("merged declared set did not gain resource_id from the registry input")
	}
	if _, ok := merged["FactKindNewKind"]["some_field"]; !ok {
		t.Error("merged declared set did not gain a brand-new kind from the registry input")
	}
}

func TestMergeRegistryPayloadSchemaFieldsNilIsNoOp(t *testing.T) {
	t.Parallel()

	base := map[string]map[string]struct{}{"FactKindAWSResource": {"account_id": {}}}
	merged := MergeRegistryPayloadSchemaFields(base, nil)
	if len(merged["FactKindAWSResource"]) != 1 {
		t.Fatalf("nil registry input must be a no-op, got %+v", merged)
	}
}
