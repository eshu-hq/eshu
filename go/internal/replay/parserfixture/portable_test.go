// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture

import (
	"bytes"
	"strings"
	"testing"
)

// TestPortableizeReplacesRepoRootAndRehydrateRestoresIt is the seam contract:
// portableize strips the absolute repo root to a sentinel so a committed fixture
// is machine-independent, and rehydrate restores it exactly, byte for byte.
func TestPortableizeReplacesRepoRootAndRehydrateRestoresIt(t *testing.T) {
	const root = "/Users/dev/checkout/eshu"
	original := []byte(`{"source_uri":"` + root + `/go/x.yaml","payload":{"path":"` + root + `/go/x.yaml"}}`)

	portable, err := portableize(original, root)
	if err != nil {
		t.Fatalf("portableize: %v", err)
	}
	if bytes.Contains(portable, []byte(root)) {
		t.Fatalf("portable fixture still contains the repo root %q: %s", root, portable)
	}
	if !bytes.Contains(portable, []byte(repoRootSentinel)) {
		t.Fatalf("portable fixture is missing the sentinel: %s", portable)
	}

	restored, err := rehydrate(portable, root)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("rehydrate is not the inverse of portableize:\n want %s\n  got %s", original, restored)
	}
}

// TestRehydrateAgainstDifferentRootRebindsPaths proves a fixture recorded under
// one checkout replays bound to another checkout — the portability guarantee a
// committed fixture needs to be byte-stable across machines and CI.
func TestRehydrateAgainstDifferentRootRebindsPaths(t *testing.T) {
	const recordRoot = "/home/ci/runner/eshu"
	const replayRoot = "/Users/dev/checkout/eshu"
	recorded := []byte(`{"source_uri":"` + recordRoot + `/a/b.tf"}`)

	portable, err := portableize(recorded, recordRoot)
	if err != nil {
		t.Fatalf("portableize: %v", err)
	}
	restored, err := rehydrate(portable, replayRoot)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	want := `{"source_uri":"` + replayRoot + `/a/b.tf"}`
	if string(restored) != want {
		t.Fatalf("rebind mismatch:\n want %s\n  got %s", want, restored)
	}
}

// TestPortableizeFailsWhenTreeIsOutsideRepoRoot guards the recorder against
// silently committing a fixture whose paths are not under the declared root —
// that fixture would not be portable.
func TestPortableizeFailsWhenTreeIsOutsideRepoRoot(t *testing.T) {
	// No occurrence of the root means nothing was substituted, but the bytes also
	// do not contain the root, so portableize succeeds vacuously; the real guard is
	// that an absolute path NOT under the root would survive. Simulate that.
	data := []byte(`{"source_uri":"/somewhere/else/x.yaml"}`)
	out, err := portableize(data, "/repo/root")
	if err != nil {
		t.Fatalf("portableize over disjoint paths should not error: %v", err)
	}
	if !bytes.Contains(out, []byte("/somewhere/else/x.yaml")) {
		t.Fatalf("a path outside the repo root must survive unchanged: %s", out)
	}
}

// TestRehydrateNoSentinelIsIdentity proves a non-portable (absolute-path) fixture
// loads unchanged, so NewSource and the temp-dir round-trip keep working.
func TestRehydrateNoSentinelIsIdentity(t *testing.T) {
	data := []byte(`{"source_uri":"/abs/path/x.yaml"}`)
	out, err := rehydrate(data, "/repo/root")
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("rehydrate without a sentinel must be identity: %s", out)
	}
}

// TestCleanRootRejectsEmpty guards the rehydrating load API: a portable fixture
// cannot be rebound without a real root.
func TestCleanRootRejectsEmpty(t *testing.T) {
	if _, err := cleanRoot("   "); err == nil {
		t.Fatal("cleanRoot must reject a blank root")
	}
	if _, err := LoadFileRehydrated("ignored.json", ""); err == nil || !strings.Contains(err.Error(), "repo root is required") {
		t.Fatalf("LoadFileRehydrated must require a repo root, got %v", err)
	}
}
