// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSourcedScripts_PathEscapeRejected pins the #5934 review finding:
// sourcedScripts joined repoRoot with an untrusted script path derived from a
// regex match on a shell `source`/`.` line, and the regex's character class
// allows "." and "/" after the literal "scripts/" prefix -- so a source line
// such as "source scripts/../../../etc/passwd" produces a script argument
// that filepath.Join cleans to a path OUTSIDE repoRoot, and sourcedScripts
// read it anyway.
//
// This fixture proves the escape is rejected: repoRoot is a "repo"
// subdirectory of a temp dir, and the escaping path
// "scripts/../../escape-marker.sh" resolves (Join+Clean) to a sibling file
// one level ABOVE repoRoot. That sibling file's own content sources a
// distinctive marker script; if sourcedScripts ever reads outside repoRoot
// again, the marker surfaces in its return value.
//
// package cigates (not cigates_test) so this test can call the unexported
// sourcedScripts directly, the same internal-test-package pattern
// trivyskipdirs_pipefail_test.go already uses in this directory.
func TestSourcedScripts_PathEscapeRejected(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The escape target sits OUTSIDE repoRoot, one level up -- exactly where
	// "scripts/../../escape-marker.sh" resolves relative to repoRoot.
	escapeTarget := filepath.Join(tmp, "escape-marker.sh")
	if err := os.WriteFile(escapeTarget, []byte("source scripts/should-not-be-discovered.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	escapingScript := "scripts/../../escape-marker.sh"
	got := sourcedScripts(repoRoot, escapingScript)
	for _, p := range got {
		if p == "scripts/should-not-be-discovered.sh" {
			t.Fatalf("sourcedScripts read outside repoRoot: escaping path %q resolved to a"+
				" sibling file and its content leaked into the result: %v", escapingScript, got)
		}
	}
	if len(got) != 0 {
		t.Errorf("expected no sourced scripts from a repoRoot-escaping path, got %v", got)
	}
}

// TestSourcedScripts_NonEscapingPathStillWorks proves the repoRoot guard
// added for the escape fix above does not also reject a legitimate,
// non-escaping "scripts/../" reference that still resolves INSIDE repoRoot
// (e.g. a script one directory deep referencing a sibling via "../").
func TestSourcedScripts_NonEscapingPathStillWorks(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "scripts", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "scripts", "sibling.sh"), []byte("true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "scripts/lib/../sibling.sh" cleans to "scripts/sibling.sh", which is
	// still inside repoRoot -- the guard must not reject this.
	entry := "scripts/lib/../sibling.sh"
	if err := os.WriteFile(filepath.Join(repoRoot, "scripts", "lib", "entry.sh"),
		[]byte("#!/usr/bin/env bash\nsource "+entry+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := sourcedScripts(repoRoot, "scripts/lib/entry.sh")
	if len(got) != 1 || got[0] != entry {
		t.Errorf("expected sourcedScripts to resolve the in-repo relative reference %q, got %v", entry, got)
	}
}
