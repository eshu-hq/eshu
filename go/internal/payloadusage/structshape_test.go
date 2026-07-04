// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureResourceStruct = `package v1

import "encoding/json"

type Resource struct {
	AccountID string ` + "`json:\"account_id\"`" + `
	ResourceID string ` + "`json:\"resource_id\"`" + `
	ARN *string ` + "`json:\"arn,omitempty\"`" + `
	CorrelationAnchors []string ` + "`json:\"correlation_anchors,omitempty\"`" + `
	Tags *map[string]string ` + "`json:\"tags,omitempty\"`" + `
	Explicit string ` + "`json:\"explicit_no_omitempty\"`" + `
	Attributes map[string]any ` + "`json:\"-\"`" + `
	unexported string
}

// resourceAlias mirrors the real custom-marshaler helper structs (resourceAlias,
// relationshipAlias). It is unexported, so ParseStructShapes must skip it — a
// decode seam's returned type is always an exported struct.
type resourceAlias struct {
	AccountID string ` + "`json:\"account_id\"`" + `
}

func (r *Resource) UnmarshalJSON(data []byte) error { return nil }
`

func writeFixtureDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return dir
}

func TestParseStructShapes(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"resource.go": fixtureResourceStruct,
		"resource_test.go": `package v1

type TestOnlyStruct struct {
	Field string ` + "`json:\"field\"`" + `
}
`,
	})

	shapes, err := ParseStructShapes(dir, "awsv1")
	if err != nil {
		t.Fatalf("ParseStructShapes() error = %v", err)
	}

	shape, ok := shapes["awsv1.Resource"]
	if !ok {
		t.Fatalf("awsv1.Resource not found in parsed shapes: %+v", shapes)
	}

	if _, ok := shapes["awsv1.TestOnlyStruct"]; ok {
		t.Error("a struct declared only in a _test.go file leaked into the parsed shapes")
	}

	if _, ok := shapes["awsv1.resourceAlias"]; ok {
		t.Error("an unexported struct (resourceAlias) leaked into the parsed shapes; only exported structs are seam-eligible")
	}

	byJSON := map[string]StructField{}
	for _, f := range shape.Fields {
		byJSON[f.JSONName] = f
	}

	if _, ok := byJSON["-"]; ok {
		t.Error("a field tagged json:\"-\" (the Attributes pass-through) must be excluded from Fields")
	}
	if len(shape.Fields) != 6 {
		t.Fatalf("len(Fields) = %d, want 6 (account_id, resource_id, arn, correlation_anchors, tags, explicit_no_omitempty); got %+v", len(shape.Fields), shape.Fields)
	}

	cases := []struct {
		jsonName string
		wantReq  bool
	}{
		{"account_id", true},
		{"resource_id", true},
		{"arn", false},                 // pointer -> optional even without omitempty semantics mattering
		{"correlation_anchors", false}, // slice -> optional
		{"tags", false},                // pointer-to-map -> optional
		{"explicit_no_omitempty", true},
	}
	for _, tc := range cases {
		field, ok := byJSON[tc.jsonName]
		if !ok {
			t.Errorf("field %q not found in parsed shape", tc.jsonName)
			continue
		}
		if field.Required != tc.wantReq {
			t.Errorf("field %q Required = %v, want %v", tc.jsonName, field.Required, tc.wantReq)
		}
	}

	if _, ok := byJSON["unexported"]; ok {
		t.Error("an unexported field with no json tag must not appear in Fields")
	}
}

func TestParseStructShapesEmptyDirIsNotError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	shapes, err := ParseStructShapes(dir, "awsv1")
	if err != nil {
		t.Fatalf("ParseStructShapes() error = %v, want nil for an empty dir", err)
	}
	if len(shapes) != 0 {
		t.Fatalf("len(shapes) = %d, want 0 for an empty dir", len(shapes))
	}
}

func TestParseJSONTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag           string
		wantName      string
		wantOmitempty bool
		wantHasTag    bool
	}{
		{"`json:\"account_id\"`", "account_id", false, true},
		{"`json:\"arn,omitempty\"`", "arn", true, true},
		{"`json:\"-\"`", "-", false, true},
		{"`yaml:\"foo\"`", "", false, false},
	}
	for _, tc := range cases {
		name, omitempty, hasTag := parseJSONTag(tc.tag)
		if name != tc.wantName || omitempty != tc.wantOmitempty || hasTag != tc.wantHasTag {
			t.Errorf("parseJSONTag(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tc.tag, name, omitempty, hasTag, tc.wantName, tc.wantOmitempty, tc.wantHasTag)
		}
	}
}
