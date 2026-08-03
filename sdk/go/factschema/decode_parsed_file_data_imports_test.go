// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"strings"
	"testing"
)

// TestDecodeParsedFileDataImports_PerLanguageShapes proves the typed accessor
// reads the two name layouts the language parsers actually write — Go's
// module-in-name with no source, and the module-in-source/symbol-in-name shape
// Python and JavaScript/TypeScript use — without either producer losing a field.
func TestDecodeParsedFileDataImports_PerLanguageShapes(t *testing.T) {
	t.Parallel()

	parsed := map[string]any{
		"imports": []any{
			map[string]any{"name": "fmt", "line_number": 4, "lang": "go"},
			map[string]any{"name": "Session", "source": "requests", "alias": "req", "line_number": 3, "import_type": "from"},
		},
	}

	entries, err := DecodeParsedFileDataImports(parsed)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataImports() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if entries[0].Name != "fmt" || entries[0].Source != "" || entries[0].LineNumber != 4 {
		t.Errorf("go entry = %+v", entries[0])
	}
	if got := entries[0].Attributes["lang"]; got != "go" {
		t.Errorf("go entry Attributes[lang] = %v, want go", got)
	}

	if entries[1].Name != "Session" || entries[1].Source != "requests" || entries[1].Alias != "req" {
		t.Errorf("python entry = %+v", entries[1])
	}
	if got := entries[1].Attributes["import_type"]; got != "from" {
		t.Errorf("python entry Attributes[import_type] = %v, want from", got)
	}
	for _, named := range []string{"name", "source", "alias", "line_number"} {
		if _, leaked := entries[1].Attributes[named]; leaked {
			t.Errorf("named field %q leaked into Attributes", named)
		}
	}
}

// TestDecodeParsedFileDataImports_AbsentKeyIsNotAnError keeps a file with no
// imports reading as "imports nothing" rather than as a malformed bucket.
func TestDecodeParsedFileDataImports_AbsentKeyIsNotAnError(t *testing.T) {
	t.Parallel()

	for name, parsed := range map[string]map[string]any{
		"absent": {},
		"null":   {"imports": nil},
		"empty":  {"imports": []any{}},
	} {
		entries, err := DecodeParsedFileDataImports(parsed)
		if err != nil {
			t.Errorf("%s: error = %v, want nil", name, err)
		}
		if len(entries) != 0 {
			t.Errorf("%s: len(entries) = %d, want 0", name, len(entries))
		}
	}
}

// TestDecodeParsedFileDataImports_MalformedBucketSurfaces proves a bucket that
// is not a slice of objects is reported instead of being read as an empty
// import set — the silent-empty failure mode issue #5691 was itself an instance
// of.
func TestDecodeParsedFileDataImports_MalformedBucketSurfaces(t *testing.T) {
	t.Parallel()

	if _, err := DecodeParsedFileDataImports(map[string]any{"imports": "fmt"}); err == nil {
		t.Fatal("error = nil for a string imports bucket, want non-nil")
	} else if !strings.Contains(err.Error(), "imports") {
		t.Errorf("error = %q, want it to name the imports key", err.Error())
	}

	_, err := DecodeParsedFileDataImports(map[string]any{
		"imports": []any{map[string]any{"name": "fmt", "line_number": "not-a-number"}},
	})
	if err == nil {
		t.Fatal("error = nil for an uncoercible line_number, want non-nil")
	}
	if !strings.Contains(err.Error(), "imports[0]") {
		t.Errorf("error = %q, want it to name the failing element index", err.Error())
	}
}

// TestDecodeParsedFileDataImports_WithoutAttributesRemainder proves the hot-path
// option leaves the named fields untouched and only skips the remainder rebuild,
// which the projector's per-generation extractor relies on.
func TestDecodeParsedFileDataImports_WithoutAttributesRemainder(t *testing.T) {
	t.Parallel()

	parsed := map[string]any{
		"imports": []any{
			map[string]any{"name": "Router", "source": "express", "line_number": 2, "lang": "typescript"},
		},
	}

	entries, err := DecodeParsedFileDataImports(parsed, WithoutAttributesRemainder())
	if err != nil {
		t.Fatalf("DecodeParsedFileDataImports() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Name != "Router" || entries[0].Source != "express" || entries[0].LineNumber != 2 {
		t.Errorf("entry = %+v, want the named fields decoded", entries[0])
	}
	if entries[0].Attributes != nil {
		t.Errorf("Attributes = %v, want nil under WithoutAttributesRemainder", entries[0].Attributes)
	}
}

// TestDecodeParsedFileDataImports_CoercesPostgresRoundTripNumbers proves a
// line_number that came back from a JSONB round trip as float64 still decodes,
// which is how every import reaches the projector in the deployed runtime.
func TestDecodeParsedFileDataImports_CoercesPostgresRoundTripNumbers(t *testing.T) {
	t.Parallel()

	entries, err := DecodeParsedFileDataImports(map[string]any{
		"imports": []any{map[string]any{"name": "fmt", "line_number": float64(7)}},
	})
	if err != nil {
		t.Fatalf("DecodeParsedFileDataImports() error = %v", err)
	}
	if len(entries) != 1 || entries[0].LineNumber != 7 {
		t.Fatalf("entries = %+v, want line_number 7", entries)
	}
}
