// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dsSeedFile is one file in a scratch git repo: written to disk always,
// staged and committed only when committed is set. Untracked entries mirror
// ignored kit materializations and scratch copies in a dirty checkout.
// Named fields (never map keys) carry the docs names: a map key naming a
// docs file would resolve through R4 in this reading file and pin findings
// the test never meant to name.
type dsSeedFile struct {
	rel       string
	content   string
	committed bool
}

// dsWriteTempGitRepo builds a scratch git repository under t.TempDir from
// files. Tests that need git skip when any git step fails (git binary or
// identity unavailable).
func dsWriteTempGitRepo(t *testing.T, files []dsSeedFile) (root, goRoot string) {
	t.Helper()
	root = t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		// Identity travels on the command line so the test never depends on
		// the ambient git config; --no-verify keeps repo hooks out.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=lockstep-test",
			"GIT_AUTHOR_EMAIL=lockstep-test@example.com",
			"GIT_COMMITTER_NAME=lockstep-test",
			"GIT_COMMITTER_EMAIL=lockstep-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable for lockstep universe test (git %s): %v: %s",
				strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	git("init", "-q")
	for _, f := range files {
		if f.committed {
			git("add", filepath.FromSlash(f.rel))
		}
	}
	git("commit", "-q", "--no-verify", "-m", "seed")
	return root, filepath.Join(root, "go")
}

// dsBarePinSource is a test-shaped Go file whose path-named constant names
// the bare basename raw and references it from another line, so R3 pins raw
// exactly when the basename resolves uniquely.
func dsBarePinSource(raw string) string {
	return "package fixture\n\nvar _ = docPage\nconst docPage = \"" + raw + "\"\n"
}

// TestDSGitUniverseExcludesUntrackedDuplicates is the regression test for
// #11634: an untracked scratch copy of a tracked doc basename must not break
// the sole tracked pin, and an untracked Go file must not contribute reads.
// The old whole-root walk indexed both, so the basename went ambiguous and
// the untracked probe file added a phantom read.
func TestDSGitUniverseExcludesUntrackedDuplicates(t *testing.T) {
	t.Parallel()
	root, goRoot := dsWriteTempGitRepo(t, []dsSeedFile{
		{rel: "keep.md", content: "canon", committed: true},
		{rel: "go/seed_test.go", content: dsBarePinSource("keep.md"), committed: true},
		{rel: "go/other_test.go", content: "package fixture\n", committed: true},
		{rel: "scratch-dup/keep.md", content: "scratch copy"},
		// Single-arg full-path read: the old walk resolved this on disk and
		// reported a phantom test input; the index universe never scans it.
		{rel: "go/zz_untracked_probe_test.go", content: "package fixture\n\nvar _ = os.ReadFile(\"scratch-dup/keep.md\")\n"},
	})
	read, _, unresolved := dsScan(t, root, goRoot)
	if !read["keep.md"] {
		t.Errorf("tracked keep.md lost its reader; read=%v unresolved=%v", read, unresolved)
	}
	if read["scratch-dup/keep.md"] {
		t.Errorf("untracked probe file contributed a read; read=%v", read)
	}
	if len(unresolved) != 0 {
		t.Errorf("unexpected unresolved demands: %v", unresolved)
	}
}

// TestDSFallbackSkipsNestedCheckoutRoots pins the non-git fallback: a
// directory carrying its own .git entry is a separate checkout, not part of
// this tree, so its same-basename doc must not break uniqueness.
func TestDSFallbackSkipsNestedCheckoutRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, f := range []dsSeedFile{
		{rel: "keep.md", content: "canon"},
		{rel: "go/seed_test.go", content: dsBarePinSource("keep.md")},
		{rel: "nested/.git", content: "gitdir: /elsewhere"},
		{rel: "nested/keep.md", content: "nested checkout copy"},
		{rel: "nested/go/x_test.go", content: "package fixture\n"},
	} {
		full := filepath.Join(root, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read, _, unresolved := dsScan(t, root, filepath.Join(root, "go"))
	if !read["keep.md"] {
		t.Errorf("tracked keep.md lost its reader across a nested checkout; read=%v unresolved=%v", read, unresolved)
	}
}

// TestDSGitUniverseSkipsAllowlistData pins the walk parity the #11634 fix
// must preserve: the scanner's own allowlist data file names every entry by
// construction, so scanning it would report the allowlist itself as findings
// (its map keys resolve as reads through R4). The filesystem walk skips it;
// the git universe must too.
func TestDSGitUniverseSkipsAllowlistData(t *testing.T) {
	t.Parallel()
	root, goRoot := dsWriteTempGitRepo(t, []dsSeedFile{
		{rel: "keep.md", content: "canon", committed: true},
		{rel: "go/seed_test.go", content: dsBarePinSource("keep.md"), committed: true},
		{rel: "go/lockstep_paths_test.go", content: "package fixture\n", committed: true},
		{rel: "go/nested_lockstep_paths_test.go", content: "package fixture\n", committed: true},
	})
	_, goFiles, _, gitRoot, err := dsGitUniverse(root, goRoot, func(path string) bool {
		return strings.HasSuffix(path, "lockstep_paths_test.go")
	})
	if err != nil {
		t.Fatalf("dsGitUniverse: %v", err)
	}
	if !gitRoot {
		t.Fatal("dsGitUniverse did not recognize the scratch git repo")
	}
	for _, gf := range goFiles {
		if strings.HasSuffix(gf, "lockstep_paths_test.go") {
			t.Errorf("allowlist data file scanned: %v", goFiles)
		}
	}
}

// TestDSStagedAmbiguityStaysUnresolved guards the other half of #11634:
// ambiguity among TRACKED files must stay fail-closed. Two committed
// same-basename docs leave the bare pin unresolved by construction.
func TestDSStagedAmbiguityStaysUnresolved(t *testing.T) {
	t.Parallel()
	root, goRoot := dsWriteTempGitRepo(t, []dsSeedFile{
		{rel: "docs/a/keep.md", content: "first", committed: true},
		{rel: "docs/b/keep.md", content: "second", committed: true},
		{rel: "go/seed_test.go", content: dsBarePinSource("keep.md"), committed: true},
	})
	read, _, _ := dsScan(t, root, goRoot)
	if read["docs/a/keep.md"] || read["docs/b/keep.md"] {
		t.Errorf("ambiguous tracked basename resolved; read=%v", read)
	}
}

// TestDSGitFailureFailsClosed pins the fail-closed contract: a root that
// carries a .git entry git itself cannot use must error instead of silently
// scanning an unscoped tree.
func TestDSGitFailureFailsClosed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "go"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := dsScanErr(root, filepath.Join(root, "go")); err == nil {
		t.Error("dsScanErr with an unusable .git entry succeeded; want a fail-closed error")
	}
}
