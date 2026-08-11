// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every digest FIELD in every recorded cassette must hold a well-formed
// digest, for the same reason TestSnapshotDigestsAreWellFormedSHA256 checks
// the B-12 snapshot: nothing downstream re-validates the value, and the
// snapshot's own digest values are copies of these. #6011's own commit
// message reports 76 malformed occurrences across 25 files at that point in
// the fix (cassettes, snapshot, fixtures, evidence docs, and query-string
// assertions) -- the squashed PR went further; the cassettes are where that
// class of fixture bug originates, and until this test existed nothing
// guarded them at all.
//
// Reuses walkJSONStrings, isDigestField, isVersionValue, and
// digestFieldValueIsWellFormed from snapshot_digest_format_test.go verbatim
// — the predicates are the contract, not something this test should
// redefine.
func TestCassetteDigestsAreWellFormedSHA256(t *testing.T) {
	t.Parallel()

	paths, err := collectCassetteFiles("../../../testdata/cassettes")
	if err != nil {
		t.Fatalf("walk cassettes: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no cassette files found under testdata/cassettes; walk root or working directory is wrong")
	}

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
					"or reference@digest for a field in refDigestAllowedFields, "+
					"or bare lowercase hex at the algorithm's width for a hashes-map key)", path, location, value)
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

// collectCassetteFiles returns every ".json" file under root, discovered by
// a recursive directory walk rather than a fixed-depth glob. P2-1: every
// cassette happens to sit at exactly <collector>/<file>.json today, so a
// depth-fixed "root/*/*.json" glob is complete right now -- but it silently
// drops a cassette added at any other depth from the digest walk, with no
// signal beyond "the checked count went down", which the checked==0 guard in
// TestCassetteDigestsAreWellFormedSHA256 cannot see (some other cassette
// keeps the count positive). Walking recursively makes the file discovery
// itself depth-agnostic; TestCassetteDigestWalkCoversManifestFiles is the
// permanent regression that a coverage regression -- a directory excluded
// from the walk, a root moved, and so on -- still fails loudly.
func collectCassetteFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// manifestCassetteRefPattern matches a replay-coverage-manifest.v1.yaml
// scenario ref that points at a cassette file, whether the YAML value is
// quoted (`ref: "testdata/cassettes/..."`) or bare
// (`ref: testdata/cassettes/...`) -- the manifest uses both forms.
var manifestCassetteRefPattern = regexp.MustCompile(`ref:\s*"?(testdata/cassettes/[A-Za-z0-9_./-]+\.json)"?`)

// manifestCassetteRefs extracts every distinct testdata/cassettes/*.json
// reference the replay-coverage manifest enumerates. It is a targeted text
// scan, not a YAML parse -- this package has no schema for
// replay-coverage-manifest.v1.yaml, and a regex match over the one field
// shape it cares about is enough to name every cassette ref the manifest
// commits to.
func manifestCassetteRefs(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	matches := manifestCassetteRefPattern.FindAllStringSubmatch(string(raw), -1)
	seen := make(map[string]bool, len(matches))
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		ref := match[1]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

// TestCassetteDigestWalkCoversManifestFiles is the P2-1 regression:
// collectCassetteFiles must find every cassette file the replay-coverage
// manifest commits to, at whatever depth it lives. checked==0 in
// TestCassetteDigestsAreWellFormedSHA256 only proves the walk found SOME
// cassette file, not that it found all of them -- a cassette silently
// dropped from the walk (a depth change, a renamed directory, a root moved)
// would still satisfy that guard as long as one other cassette remained
// reachable. Cross-checking the walk's output against the manifest's own ref
// list gives the walk a floor that a single dropped file trips, instead of
// relying on nothing stronger than "found more than zero".
func TestCassetteDigestWalkCoversManifestFiles(t *testing.T) {
	t.Parallel()

	const manifestPath = "../../../specs/replay-coverage-manifest.v1.yaml"
	refs, err := manifestCassetteRefs(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	if len(refs) == 0 {
		t.Fatal("no testdata/cassettes refs found in the replay-coverage manifest; regex pattern or manifest path is wrong")
	}

	walked, err := collectCassetteFiles("../../../testdata/cassettes")
	if err != nil {
		t.Fatalf("walk cassettes: %v", err)
	}
	present := make(map[string]bool, len(walked))
	for _, path := range walked {
		// Walked paths carry the "../../../" prefix collectCassetteFiles was
		// called with; strip it so they compare against the manifest's
		// repo-relative refs.
		present[strings.TrimPrefix(path, "../../../")] = true
	}

	var missing []string
	for _, ref := range refs {
		if !present[ref] {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d manifest-referenced cassette file(s) not found by the recursive walk: %v", len(missing), missing)
	}
}
