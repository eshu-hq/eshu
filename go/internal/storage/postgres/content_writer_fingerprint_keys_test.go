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
