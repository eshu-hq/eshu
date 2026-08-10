// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// Reuses walkJSONStrings, isDigestField, isVersionValue, and digestPattern
// from snapshot_digest_format_test.go verbatim — the predicates are the
// contract, not something this test should redefine.
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

	checked := 0
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
			if !cassetteDigestValueIsWellFormed(value) {
				t.Errorf("%s: %s = %q is not a well-formed digest "+
					"(want sha256: plus 64 lowercase hex, or sha512: plus 128, "+
					"optionally prefixed with a non-empty \"reference@\")", path, location, value)
			}
		})
	}

	// A cassette set that stopped carrying digest fields would satisfy the
	// loop above without proving anything.
	if checked == 0 {
		t.Fatal("no digest values checked across cassettes; this check passed vacuously")
	}
}

// cassetteDigestValueIsWellFormed validates a digest-field value, allowing
// for the "reference@digest" shape a digest field is documented to carry
// (kuberneteslive's resolved_image_digest is CRI-normalized to the bare
// "repo@sha256:<digest>" form -- go/internal/collector/kuberneteslive/
// envelope.go and doc.go, #5432). That shape is a real, committed field
// contract, not a fixture bug, so the fix is to validate the digest inside
// it rather than exempt the field: exempting it would stop checking the
// digest entirely, and a resolved_image_digest carrying a malformed digest
// after the "@" is exactly what this gate exists to catch.
//
// The split is on the LAST "@" because a registry/repository path can itself
// contain "@" (rare, but the digest is always the final segment, never an
// earlier one). The reference portion before "@" must be non-empty, or the
// value is not really a reference@digest pair -- a bare "@sha256:<hex>" is
// held to the same rejection as any other malformed digest field.
func cassetteDigestValueIsWellFormed(value string) bool {
	if index := strings.LastIndex(value, "@"); index >= 0 {
		reference, digest := value[:index], value[index+1:]
		if reference == "" {
			return false
		}
		return digestPattern.MatchString(digest)
	}
	return digestPattern.MatchString(value)
}

// TestCassetteDigestValueIsWellFormedHandlesRefDigestShape proves the "@"
// carve-out validates rather than exempts: a well-formed digest after the
// last "@" is accepted, but a malformed one still fails, both with and
// without a reference prefix. It also proves the carve-out did not loosen
// the pre-existing bare-digest behavior.
func TestCassetteDigestValueIsWellFormedHandlesRefDigestShape(t *testing.T) {
	t.Parallel()

	accept := []string{
		"sha256:" + strings.Repeat("a", 64),
		"ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 64),
		"sha512:" + strings.Repeat("f", 128),
	}
	for _, value := range accept {
		if !cassetteDigestValueIsWellFormed(value) {
			t.Errorf("cassetteDigestValueIsWellFormed(%q) = false, want true", value)
		}
	}

	reject := []string{
		"sha256:" + strings.Repeat("a", 63),                                   // bare, too short
		"ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 63), // ref@digest, digest too short
		"ghcr.io/eshu-hq/supply-chain-demo@sha256:" + strings.Repeat("a", 65), // ref@digest, digest too long
		"@sha256:" + strings.Repeat("a", 64),                                  // empty reference before "@"
		"ghcr.io/eshu-hq/supply-chain-demo@not-a-digest",
	}
	for _, value := range reject {
		if cassetteDigestValueIsWellFormed(value) {
			t.Errorf("cassetteDigestValueIsWellFormed(%q) = true, want false", value)
		}
	}
}
