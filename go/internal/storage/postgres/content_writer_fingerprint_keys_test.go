// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
)

// TestFingerprintRowReadsEveryMetadataKey ties the query layer's strip list to
// the writer: querycontract/entity.FingerprintMetadataKeys is what the API removes from response
// metadata, so each of those keys must be one the writer actually consumes.
// Dropping any one key from a complete payload must change the row the writer
// builds; a key the writer ignores would mean the strip list names something
// that is not a store-internal fingerprint column.
func TestFingerprintRowReadsEveryMetadataKey(t *testing.T) {
	t.Parallel()

	full := map[string]any{
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyRenamed:    "def",
		fingerprint.KeySketch:     "00ff",
		fingerprint.KeyShingles:   fingerprint.EncodeShingles([]uint64{7, 42}),
		fingerprint.KeyTokenCount: 77,
	}
	want := fingerprintRowFromMetadata("e1", "r1", full)
	if !want.hasFingerprint {
		t.Fatal("complete metadata must yield a fingerprint row")
	}
	keys := entitycontract.FingerprintMetadataKeys()
	if len(keys) != len(full) {
		t.Fatalf("FingerprintMetadataKeys() = %v, fixture covers %d keys; update the fixture with the new key", keys, len(full))
	}
	for _, key := range keys {
		if _, ok := full[key]; !ok {
			t.Fatalf("FingerprintMetadataKeys() names %q, which the fixture does not populate", key)
		}
		without := make(map[string]any, len(full))
		for k, v := range full {
			if k != key {
				without[k] = v
			}
		}
		if got := fingerprintRowFromMetadata("e1", "r1", without); reflect.DeepEqual(got, want) {
			t.Errorf("dropping %q left the writer row unchanged; the writer does not read it", key)
		}
	}
}

// TestPersistedEntityMetadataStripsFingerprintKeys pins the #7172 write
// contract: the metadata map persisted to content_entities carries no parser
// fingerprint keys (side tables own that truth), while every other entry
// survives and the caller's map is never mutated (the same map still feeds
// the side-table fan-out).
func TestPersistedEntityMetadataStripsFingerprintKeys(t *testing.T) {
	t.Parallel()

	full := map[string]any{
		"docstring":               "Handles the request.",
		fingerprint.KeyExact:      "abc",
		fingerprint.KeyRenamed:    "def",
		fingerprint.KeySketch:     "00ff",
		fingerprint.KeyShingles:   fingerprint.EncodeShingles([]uint64{7, 42}),
		fingerprint.KeyTokenCount: 77,
	}
	got := persistedEntityMetadata(full)
	for _, key := range entitycontract.FingerprintMetadataKeys() {
		if _, present := got[key]; present {
			t.Errorf("persisted metadata kept store-internal key %q", key)
		}
	}
	if got["docstring"] != "Handles the request." {
		t.Errorf("persisted metadata = %#v, want non-fingerprint entries kept", got)
	}
	if len(full) != 6 {
		t.Errorf("persistedEntityMetadata mutated its input: %#v", full)
	}
	if out := persistedEntityMetadata(nil); out != nil {
		t.Errorf("persistedEntityMetadata(nil) = %#v, want nil", out)
	}
	if out := persistedEntityMetadata(map[string]any{}); len(out) != 0 {
		t.Errorf("persistedEntityMetadata(empty) = %#v, want empty", out)
	}
	plain := map[string]any{"docstring": "Handles the request.", "lang": "go"}
	if out := persistedEntityMetadata(plain); !reflect.DeepEqual(out, plain) {
		t.Errorf("persistedEntityMetadata(plain) = %#v, want entries kept", out)
	}
}
