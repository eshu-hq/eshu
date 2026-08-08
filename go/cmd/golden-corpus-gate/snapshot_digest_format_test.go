// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// A sha256 digest is the algorithm, a colon, and exactly 64 lowercase hex
// characters. Anything else is not a digest, whatever it looks like.
var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Every digest-shaped value in the golden snapshot must be a well-formed
// sha256, because nothing downstream checks.
//
// The CVE-2026-00010 demo finding carried a 66-character subject_digest — a
// valid-looking string with two extra hex characters on the end. It survived
// because every consumer compares digests with string equality, so a value that
// is not a digest at all round-trips cleanly through the cassettes, the B-12
// snapshot, the MCP query shape and the HTTP assertions. The first thing to
// break would have been a future digest-format validation, and it would have
// broken against a fixture rather than against real data (#5788).
//
// This asserts the shape rather than pinning specific values, so it keeps
// holding as fixtures are added.
func TestSnapshotDigestsAreWellFormedSHA256(t *testing.T) {
	t.Parallel()

	const path = "../../../testdata/golden/e2e-20repo-snapshot.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	checked := 0
	walkJSONStrings(document, "", func(location, value string) {
		// Scoped to fields that ARE digests, by name. Two other snapshot values
		// carry a "sha256:" prefix without being one, deliberately: a combined
		// "sha256:...|sha512:..." hash pair, and a synthetic
		// kv_path_fingerprint that encodes a path rather than a hash. Checking
		// every sha256-prefixed string would fail on both and force the check
		// to be weakened until it caught nothing.
		if !isDigestField(location) || !strings.HasPrefix(value, "sha256:") {
			return
		}
		checked++
		if !sha256DigestPattern.MatchString(value) {
			hex := strings.TrimPrefix(value, "sha256:")
			t.Errorf("%s = %q is not a sha256 digest: %d hex characters, want 64",
				location, value, len(hex))
		}
	})

	// A snapshot that stopped carrying digests would satisfy the loop above
	// without proving anything.
	if checked == 0 {
		t.Fatal("no sha256-prefixed values found in the snapshot; this check passed vacuously")
	}
}

// isDigestField reports whether a dotted JSON location names a digest field.
// The trailing segment is what matters: "digest", "subject_digest",
// "artifact_digest", and "digest_or_version" are digests; "hashes" and
// "kv_path_fingerprint" are not.
func isDigestField(location string) bool {
	segment := location
	if index := strings.LastIndex(segment, "."); index >= 0 {
		segment = segment[index+1:]
	}
	segment = strings.TrimSuffix(segment, "[]")
	if index := strings.Index(segment, "["); index >= 0 {
		segment = segment[:index]
	}
	return segment == "digest" || strings.HasSuffix(segment, "_digest") ||
		segment == "digest_or_version"
}

// walkJSONStrings visits every string in a decoded JSON document, passing a
// dotted path so a failure names the field rather than just the value.
func walkJSONStrings(node any, location string, visit func(location, value string)) {
	switch typed := node.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if location != "" {
				next = location + "." + key
			}
			walkJSONStrings(child, next, visit)
		}
	case []any:
		for index, child := range typed {
			next := location + "[" + itoa(index) + "]"
			walkJSONStrings(child, next, visit)
		}
	case string:
		visit(location, typed)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
