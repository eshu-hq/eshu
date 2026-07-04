// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// TestFixturePackSchemasMatchCanonical locks the fixture pack's embedded
// schemas to the canonical generated artifacts under schema/. The pack ships an
// embedded copy because go:embed cannot reach a sibling directory, so this test
// is the guard that the copy never drifts from the source of truth: if a schema
// is regenerated (a payload struct changed) without refreshing the pack, or the
// pack copy is hand-edited, the two diverge and this test fails, the same
// drift-as-build-failure discipline TestSchemasHaveNoDrift applies to the
// generated schemas versus the structs.
func TestFixturePackSchemasMatchCanonical(t *testing.T) {
	t.Parallel()

	packFiles, err := fixturepack.SchemaFiles()
	if err != nil {
		t.Fatalf("fixturepack.SchemaFiles() error = %v, want nil", err)
	}

	canonicalEntries, err := os.ReadDir("schema")
	if err != nil {
		t.Fatalf("os.ReadDir(schema) error = %v, want nil", err)
	}
	canonical := make(map[string]struct{}, len(canonicalEntries))
	for _, entry := range canonicalEntries {
		canonical[entry.Name()] = struct{}{}
	}

	// Every canonical schema must be present in the pack: a new fact kind that
	// lands a schema/ artifact but not a pack copy is a gap this catches.
	packSet := make(map[string]struct{}, len(packFiles))
	for _, name := range packFiles {
		packSet[name] = struct{}{}
	}
	for name := range canonical {
		if _, ok := packSet[name]; !ok {
			t.Errorf("canonical schema %q is missing from the fixture pack; add it under sdk/go/factschema/fixturepack/schema/", name)
		}
	}

	for _, name := range packFiles {
		if _, ok := canonical[name]; !ok {
			t.Errorf("fixture pack schema %q has no canonical source under schema/; remove it or add the source", name)
			continue
		}
		want, err := os.ReadFile(filepath.Join("schema", name))
		if err != nil {
			t.Fatalf("os.ReadFile(schema/%s) error = %v, want nil", name, err)
		}
		got, err := fixturepack.RawSchemaFile(name)
		if err != nil {
			t.Fatalf("fixturepack.RawSchemaFile(%s) error = %v, want nil", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("fixture pack schema %q drifted from canonical schema/%s; refresh the embedded copy (cp schema/%s fixturepack/schema/%s)", name, name, name, name)
		}
	}
}

// TestFixturePackPayloadsDecodeThroughSeam proves every curated valid payload
// the pack ships decodes cleanly through the typed contracts seam, and every
// curated invalid payload is rejected with a classified *DecodeError naming a
// field. This keeps the pack's fixtures honest against the same decode path the
// reducer uses, so a fixture cannot silently describe a shape the reducer would
// actually dead-letter (or accept).
func TestFixturePackPayloadsDecodeThroughSeam(t *testing.T) {
	t.Parallel()

	for _, kind := range fixturepack.Kinds() {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			valid, ok := fixturepack.ValidPayload(kind)
			if !ok {
				t.Fatalf("fixturepack ships no valid payload for %q", kind)
			}
			if err := decodeByKind(t, kind, valid); err != nil {
				t.Fatalf("valid payload for %q failed decode: %v", kind, err)
			}

			invalid, ok := fixturepack.InvalidPayload(kind)
			if !ok {
				t.Fatalf("fixturepack ships no invalid payload for %q", kind)
			}
			err := decodeByKind(t, kind, invalid)
			if err == nil {
				t.Fatalf("invalid payload for %q decoded without error, want a classified rejection", kind)
			}
			var decodeErr *DecodeError
			if !errors.As(err, &decodeErr) || decodeErr.Field == "" {
				t.Fatalf("invalid payload for %q error = %v, want a *DecodeError naming a field", kind, err)
			}
		})
	}
}
