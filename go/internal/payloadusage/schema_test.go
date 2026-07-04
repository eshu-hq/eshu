// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAllMappedSchemas writes a minimal valid schema file for every fact
// kind in factKindSchemaFile under dir, so LoadDeclaredFieldsFromSchemas
// (which fails closed on a missing mapped file) has every file it requires.
// The named field is added to the target kind's properties so a caller can
// assert a specific field is loaded.
func writeAllMappedSchemas(t *testing.T, dir, targetKind, targetField string) {
	t.Helper()
	for factKindConst, fileName := range factKindSchemaFile {
		props := `"account_id": {"type": "string"}`
		if factKindConst == targetKind && targetField != "" {
			props += `, "` + targetField + `": {"type": "string"}`
		}
		schemaJSON := `{"properties": {` + props + `}, "required": ["account_id"]}`
		if err := os.WriteFile(filepath.Join(dir, fileName), []byte(schemaJSON), 0o600); err != nil {
			t.Fatalf("write fixture schema %s: %v", fileName, err)
		}
	}
}

func TestLoadDeclaredFieldsFromSchemas(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeAllMappedSchemas(t, dir, "FactKindAWSResource", "resource_id")

	declared, err := LoadDeclaredFieldsFromSchemas(dir)
	if err != nil {
		t.Fatalf("LoadDeclaredFieldsFromSchemas() error = %v", err)
	}

	fields, ok := declared["FactKindAWSResource"]
	if !ok {
		t.Fatalf("FactKindAWSResource not found in declared fields: %+v", declared)
	}
	for _, want := range []string{"account_id", "resource_id"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("declared fields missing %q: %+v", want, fields)
		}
	}

	// Every mapped kind must be present now that the loader fails closed on a
	// missing mapped file: all 8 schema files were written.
	if len(declared) != len(factKindSchemaFile) {
		t.Errorf("len(declared) = %d, want %d (every mapped kind present)", len(declared), len(factKindSchemaFile))
	}
}

func TestLoadDeclaredFieldsFromSchemasFailsClosedOnMissingMappedFile(t *testing.T) {
	t.Parallel()

	// A mapped schema file that is missing must be a fail-closed ERROR, not a
	// skip: otherwise CheckManifest falls back to the manifest's own
	// DeclaredFields for that kind and can never report a violation, so
	// deleting a schema silently disables the gate for its kind.
	dir := t.TempDir()
	writeAllMappedSchemas(t, dir, "", "")
	// Remove exactly one mapped schema file.
	removed := factKindSchemaFile["FactKindAWSResource"]
	if err := os.Remove(filepath.Join(dir, removed)); err != nil {
		t.Fatalf("remove fixture schema: %v", err)
	}

	_, err := LoadDeclaredFieldsFromSchemas(dir)
	if err == nil {
		t.Fatal("LoadDeclaredFieldsFromSchemas() error = nil, want a fail-closed error for a missing mapped schema file")
	}
	if !strings.Contains(err.Error(), "FactKindAWSResource") || !strings.Contains(err.Error(), removed) {
		t.Errorf("error = %q, want it to name the fact kind and the missing file", err.Error())
	}
}

func TestLoadDeclaredFieldsFromSchemasMissingDirFailsClosed(t *testing.T) {
	t.Parallel()

	// A schema dir that does not exist at all fails closed too: the first
	// mapped file read errors, and that is louder-is-better than silently
	// producing an empty declared set that disables every kind's gate.
	_, err := LoadDeclaredFieldsFromSchemas(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("LoadDeclaredFieldsFromSchemas() error = nil, want a fail-closed error for a missing schema dir")
	}
}

func TestLoadDeclaredFieldsFromSchemasRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aws_resource.v1.schema.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture schema: %v", err)
	}
	_, err := LoadDeclaredFieldsFromSchemas(dir)
	if err == nil {
		t.Fatal("LoadDeclaredFieldsFromSchemas() error = nil, want an error for malformed JSON")
	}
}

func TestUnmappedSeamFactKinds(t *testing.T) {
	t.Parallel()

	seams := []DecodeSeam{
		{FuncName: "decodeAWSResource", FactKindConst: "FactKindAWSResource"},
		{FuncName: "decodeSomethingNew", FactKindConst: "FactKindSomethingNew"},
	}
	missing := UnmappedSeamFactKinds(seams)
	if len(missing) != 1 || missing[0] != "FactKindSomethingNew" {
		t.Fatalf("UnmappedSeamFactKinds() = %v, want [FactKindSomethingNew]", missing)
	}
}
