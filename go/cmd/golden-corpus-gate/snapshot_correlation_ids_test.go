// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestGoldenSnapshotRequiredCorrelationIDsAreUnique(t *testing.T) {
	t.Parallel()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	seen := make(map[string]struct{}, len(snap.Graph.RequiredCorrelations))
	for _, correlation := range snap.Graph.RequiredCorrelations {
		if _, duplicate := seen[correlation.ID]; duplicate {
			t.Errorf("required_correlations contains duplicate id %q", correlation.ID)
		}
		seen[correlation.ID] = struct{}{}
	}
}

// TestGoldenSnapshotHasNoDuplicateJSONKeys walks the raw snapshot file and
// fails on any object that repeats a key. encoding/json silently keeps the
// LAST value for a duplicated key, so a merge mistake that folds one
// required-correlation entry's fields into a sibling's object does not
// surface as a parse error or as a duplicate-id (the earlier entry simply
// VANISHES from the decoded snapshot, and its assertion is silently no longer
// enforced — a false green). This is exactly what happened on the #5810
// branch: rc-170's fields were appended inside rc-169's object, so the gate
// asserted rc-170 while rc-169's HAS_REGISTRY_EVENT floor (#5458) was
// destroyed without any test or gate output noticing.
func TestGoldenSnapshotHasNoDuplicateJSONKeys(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkJSONForDuplicateKeys(decoder, "$"); err != nil {
		t.Fatal(err)
	}
}

// walkJSONForDuplicateKeys consumes exactly one JSON value from decoder,
// recursing into objects and arrays, and returns an error naming the path of
// the first object that declares the same key twice.
func walkJSONForDuplicateKeys(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("token at %s: %w", path, err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil // scalar
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("object key at %s: %w", path, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("non-string object key %v at %s", keyToken, path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf(
					"duplicate JSON key %q at %s — encoding/json keeps only the last value, so the earlier entry's assertion is silently destroyed",
					key, path,
				)
			}
			seen[key] = struct{}{}
			if err := walkJSONForDuplicateKeys(decoder, path+"."+key); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil { // consume '}'
			return fmt.Errorf("object close at %s: %w", path, err)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := walkJSONForDuplicateKeys(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		if _, err := decoder.Token(); err != nil { // consume ']'
			return fmt.Errorf("array close at %s: %w", path, err)
		}
	}
	return nil
}
