// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSchemaBytesReturnsCanonicalSchema proves SchemaBytes loads the
// checked-in JSON Schema for a known fact kind straight from this package's
// own embed, byte-identical to the canonical artifact under schema/. This is
// the accessor a runtime conformance test in the core `go` module uses to
// validate a real emitter payload against its committed schema without
// duplicating the schema tree (unlike sdk/go/factschema/fixturepack, whose
// embedded copy exists only because go:embed cannot reach a sibling
// directory).
func TestSchemaBytesReturnsCanonicalSchema(t *testing.T) {
	t.Parallel()

	got, ok := SchemaBytes("aws_security_group_rule")
	if !ok {
		t.Fatalf("SchemaBytes(%q) ok = false, want true", "aws_security_group_rule")
	}
	want, err := os.ReadFile(filepath.Join("schema", "aws_security_group_rule.v1.schema.json"))
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("SchemaBytes(%q) returned bytes that differ from the canonical schema/ artifact", "aws_security_group_rule")
	}
}

// TestSchemaBytesUnknownKind proves an unknown fact kind reports ok=false
// rather than panicking or returning a zero-value schema that would silently
// pass validation against nothing.
func TestSchemaBytesUnknownKind(t *testing.T) {
	t.Parallel()

	if _, ok := SchemaBytes("does_not_exist"); ok {
		t.Fatalf("SchemaBytes(%q) ok = true, want false", "does_not_exist")
	}
}
