// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Every digest FIELD in every recorded cassette must hold a well-formed
// digest, for the same reason TestSnapshotDigestsAreWellFormedSHA256 checks
// the B-12 snapshot: nothing downstream re-validates the value, and the
// snapshot's own digest values are copies of these. #6011 fixed 76 malformed
// occurrences by walking the snapshot alone; the cassettes are where that
// class of fixture bug originates, and until this test existed nothing
// guarded them at all.
//
// Reuses walkJSONStrings, isDigestField, isVersionValue, digestPattern, and
// digestFieldValueIsWellFormed from snapshot_digest_format_test.go verbatim
// — the predicates are the contract, not something this test should
// redefine.
func TestCassetteDigestsAreWellFormedSHA256(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("../../../testdata/cassettes/*/*.json")
	if err != nil {
		t.Fatalf("glob cassettes: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no cassette files matched testdata/cassettes/*/*.json; glob pattern or working directory is wrong")
	}
	sort.Strings(paths)

	checked, violations := 0, 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}

		walkJSONStrings(document, "", func(location, value string) {
			if !isDigestField(location) {
				return
			}
			if isVersionValue(location, value) {
				return
			}
			checked++
			if !digestFieldValueIsWellFormed(location, value) {
				violations++
				t.Errorf("%s: %s = %q is not a well-formed digest "+
					"(want sha256: plus 64 lowercase hex, or sha512: plus 128, "+
					"or reference@digest for a field in refDigestAllowedFields)", path, location, value)
			}
		})
	}
	t.Logf("%d digest values checked, %d violations", checked, violations)

	// A cassette set that stopped carrying digest fields would satisfy the
	// loop above without proving anything.
	if checked == 0 {
		t.Fatal("no digest values checked across cassettes; this check passed vacuously")
	}
}

// The "reference@digest" carve-out itself — its shape, its field gating via
// refDigestAllowedFields, and its malformed-input handling — is defined and
// tested once, in snapshot_digest_format_test.go
// (digestFieldValueIsWellFormed, TestDigestFieldValueIsWellFormedHandlesRefDigestShape).
// This file only proves that walker applies it to every cassette.
