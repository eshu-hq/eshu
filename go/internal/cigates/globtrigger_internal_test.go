// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestMain scrubs the git environment for the whole test binary before any
// case runs. Every fixture in this package is built with `git -C <root> …`,
// and -C is NOT authoritative: GIT_DIR, GIT_INDEX_FILE, GIT_WORK_TREE and
// their siblings override it, so an ambient one silently retargets the command
// at whatever repository the variable names. A git pre-commit hook exports
// GIT_INDEX_FILE, and running this package under one turned this suite red AND
// left the other repository's index holding this package's fixture files, its
// own two tracked entries gone — a test suite that rewrites an unrelated
// working tree. (The red count is not recorded here: it depends on the victim
// tree and on whether subtests are counted, so it would read as a fact about
// the defect when it is a fact about one machine's run.)
//
// The scrub is process-wide rather than per-command because the subject under
// test reads the process environment too: loadTrackedPaths shells out to git
// with the inherited environment on purpose (see its doc comment, and keep it
// that way — under a hook that environment describes the very tree being
// committed). Scrubbing only the fixture commands would leave every Validate
// call resolving globs against the ambient tree while the fixture wrote to the
// right one, which is the same wrong answer arrived at more quietly.
//
// Dropping every GIT_* name is deliberate, and an allowlist of the ones known
// to retarget a command is not the same fix: `git help environment` lists more
// of them than anyone recalls, it grows between releases, and a list that
// misses one leaves exactly the defect this closes. GIT_CONFIG_* is then set
// back so the developer's own global config — a core.excludesFile, an
// init.templateDir, a hook — cannot decide what a fixture tracks either.
//
// The scrub then checks itself, because a harness that silently stops
// scrubbing looks exactly like a clean machine: narrow the sweep to one name
// and every case here still passes in a shell that happens to export nothing.
// A canary planted before the sweep makes the check bite in ANY environment
// rather than only under a hook, and the survivor sweep afterwards catches an
// ambient name the loop somehow walked past.
func TestMain(m *testing.M) {
	const canary = "GIT_CIGATES_SCRUB_CANARY"
	if err := os.Setenv(canary, "planted"); err != nil {
		panic("cigates test: could not plant the scrub canary: " + err.Error())
	}
	for _, entry := range os.Environ() {
		if name, _, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(name, "GIT_") {
			if err := os.Unsetenv(name); err != nil {
				panic("cigates test: could not scrub " + name + ": " + err.Error())
			}
		}
	}
	pinned := map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
	}
	for name, value := range pinned {
		if err := os.Setenv(name, value); err != nil {
			panic("cigates test: could not pin " + name + ": " + err.Error())
		}
	}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, "GIT_") {
			continue
		}
		if _, isPinned := pinned[name]; isPinned {
			continue
		}
		panic("cigates test: " + name + " survived the scrub. Every GIT_* name but the pinned config set must be gone before any fixture runs, or `git -C <root>` is silently retargeted at whatever repository the survivor names — and the suite then rewrites an unrelated working tree instead of failing")
	}
	os.Exit(m.Run())
}

// universeOf builds a path universe from an explicit path list, so a case can
// state exactly what the tree contains — including the directory entries
// loadTrackedPaths derives — without needing a git fixture.
func universeOf(paths ...string) *trackedPaths {
	tp := &trackedPaths{byHead: make(map[string][][]string)}
	seen := make(map[string]struct{})
	for _, p := range paths {
		tp.add(p, seen)
	}
	return tp
}

// TestTrackedPaths_MatchesAnyAgreesWithMatchGlob is the equivalence guard the
// first-segment index needs. matchesAny skips whole buckets of the universe
// and reuses matchSegments directly instead of calling MatchGlob per path, so
// it can drift from MatchGlob in exactly the cases the index reasons about: a
// pattern whose first segment is a wildcard, and a leading "**". Select uses
// MatchGlob to pick gates at run time; if these two disagree, this check
// either passes a trigger that can never select or fails one that does.
//
// What this test does NOT pin is matchesAny's leading-"/" / trailing-"/"
// guard, and the two patterns below that exercise it are vacuous cases rather
// than teeth. Delete that guard and every case here stays green: a leading "/"
// makes the pattern's first segment empty, so the index sends it to the
// byHead[""] bucket, and a trailing "/" makes its last segment empty, which no
// path segment can equal. A universe built by loadTrackedPaths can never hold
// an empty segment — git ls-files emits paths with neither a leading nor a
// trailing "/" — so the guard is unreachable by construction, and the only
// case that could go red without it would have to hand matchesAny a universe
// the enumerator cannot produce. Saying otherwise here would be the same
// claimed-but-absent coverage #6159 exists to close.
//
// The guard stays, and the two patterns stay in the table, because the
// contract of matchesAny is "the same semantics as MatchGlob" over any
// universe it is handed. A reader comparing the two should not have to re-derive
// that one of MatchGlob's guards happens to be redundant here, and a future
// change to how candidates are chosen would make it load-bearing again.
func TestTrackedPaths_MatchesAnyAgreesWithMatchGlob(t *testing.T) {
	t.Parallel()

	universe := []string{
		"go", "go/internal", "go/internal/cigates", "go/internal/cigates/glob.go",
		"go/internal/query", "go/internal/query/openapi_types.go",
		"scripts", "scripts/run-remote-e2e-x.sh", "scripts/lib", "scripts/lib/live-gate-lock.sh",
		".github", ".github/workflows", ".github/workflows/test.yml",
		"README.md", "Makefile",
	}
	tp := universeOf(universe...)

	patterns := []string{
		"go/**",                          // literal head, matches
		"go/internal/*",                  // literal head, only a directory matches
		"go/internal/cigates/*.go",       // literal head, file match
		"go/internal/nothing/**",         // literal head, no match
		"scripts/**/run-remote-e2e-*.sh", // ** at its zero-segment expansion
		"scripts/**/nothing-*.sh",        // ** expansion, no match
		"*.md",                           // wildcard head — must not use the index
		"*/internal/**",                  // wildcard head, matches deeper
		"*/nothing/**",                   // wildcard head, no match
		"**/openapi*.go",                 // ** head — must not use the index
		"**/nothing*.go",                 // ** head, no match
		".github/workflows/*.yml",        // dotted head
		"../etc/*.conf",                  // escapes the tree: matches nothing
		"/go/**",                         // anchored: agreement is vacuous, see above
		"go/internal/",                   // directory-style: agreement is vacuous, see above
		"go",                             // degenerate single-segment literal
		"Makefile",                       // degenerate single-segment literal
		"go/internal/cigates/glob.go/**", // past a file: no match
		"go/**/cigates/**/glob.go",       // two **, one expanding to zero
	}

	for _, pattern := range patterns {
		want := false
		for _, p := range universe {
			if MatchGlob(pattern, p) {
				want = true
				break
			}
		}
		if got := tp.matchesAny(pattern); got != want {
			t.Errorf("matchesAny(%q) = %v; a MatchGlob scan of the same universe says %v", pattern, got, want)
		}
	}
}

// TestTrackedPaths_MatchesAnyConsultsTheFirstSegmentIndex pins the design the
// measured cost in matchesAny's own comment rests on: for a pattern whose
// first segment holds no "*", the candidate set is byHead[that segment], never
// the whole universe. Reverting the body to the per-path MatchGlob scan it is
// measured against leaves every other case in this package green, because the
// two forms return identical verdicts by construction — which is precisely
// what MatchesAnyAgreesWithMatchGlob asserts — so without this the order of
// magnitude the design exists for could be given back with nothing failing.
//
// The universe here is deliberately desynchronised to make the difference
// observable: one path sits in `all` but not in `byHead`. `add` cannot produce
// that state and no production code can reach it, and it is the only
// deterministic way to tell "consulted the index" from "scanned everything".
// The alternative — asserting a wall-clock ratio — would trade a real guard
// for one that fails on a loaded machine.
func TestTrackedPaths_MatchesAnyConsultsTheFirstSegmentIndex(t *testing.T) {
	t.Parallel()

	tp := universeOf("go", "go/internal", "go/internal/cigates", "go/internal/cigates/glob.go")
	tp.all = append(tp.all, strings.Split("go/unindexed/hidden.go", "/"))

	// Positive control: without this, a matchesAny stubbed to return false
	// would satisfy the assertion below for the wrong reason.
	if !tp.matchesAny("go/internal/cigates/*.go") {
		t.Fatal("matchesAny() = false for a pattern the indexed bucket does hold; the case below cannot distinguish an unindexed scan unless the indexed answer is still right")
	}
	if tp.matchesAny("go/unindexed/*.go") {
		t.Fatal("matchesAny() = true for a path reachable only by scanning the whole universe: a literal first segment must resolve through byHead, or the ~11M-comparison scan this index exists to avoid is back")
	}
}

// TestLoadTrackedPaths_DerivesEveryAncestorDirectory pins the universe's shape
// against the real enumerator. git tracks files only, so the directory
// entries a directory-shaped trigger resolves against exist only because this
// derives them — and it stops walking at the first ancestor it has already
// seen, which is sound only while every recorded path brought its own
// ancestors with it. A regression there drops directories silently and turns
// working registry triggers into reported-stale ones.
func TestLoadTrackedPaths_DerivesEveryAncestorDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Two files sharing a prefix: the second reaches an ancestor the first
	// already recorded, which is the case the short-circuit walks into.
	for _, rel := range []string{"go/internal/cigates/glob.go", "go/internal/cigates/validate.go", "README.md"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"add", "-A", "--force"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	tp, err := loadTrackedPaths(root)
	if err != nil {
		t.Fatalf("loadTrackedPaths() error = %v", err)
	}

	var got []string
	for _, segments := range tp.all {
		got = append(got, strings.Join(segments, "/"))
	}
	sort.Strings(got)
	want := []string{
		"README.md",
		"go",
		"go/internal",
		"go/internal/cigates",
		"go/internal/cigates/glob.go",
		"go/internal/cigates/validate.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("universe =\n%s\nwant (every file plus every implied directory, each once) =\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLoadTrackedPaths_IgnoresAnAmbientGitDir pins gitTreeEnv on the production
// call. "git -C repoRoot" does NOT pin which repository git reads: an ambient
// GIT_DIR overrides it, and the wrong answer is silent rather than loud.
// Pointed at another checkout of the same repository — the normal state of a
// machine that develops in worktrees — the enumeration succeeds and the gate
// reports PASS while every trigger was resolved against a tree that is not the
// one under test. Measured on this repository before the fix: git read 20,194
// paths from the other checkout instead of 20,197 from the worktree named by
// --repo-root, and the gate still exited 0. A trigger whose last file was just
// deleted here then passes on the strength of a copy that still has it, which
// is the #6159 defect arriving through the environment rather than the
// registry.
//
// GIT_DIR is the one name pinned here because it is the one measured to change
// what `git ls-files` returns. gitTreeEnv drops several siblings as well; those
// are belt-and-braces against git commands this package does not run today, not
// vectors demonstrated against this one, and this case deliberately does not
// claim otherwise. GIT_INDEX_FILE is the other measured vector and is KEPT on
// purpose — see gitTreeEnv for the hook rationale and the residual it leaves.
//
// Not parallel: t.Setenv forbids it, and the ambient variable is the whole
// subject. Go runs this while every t.Parallel case is still paused, so no
// sibling observes the mutation.
func TestLoadTrackedPaths_IgnoresAnAmbientGitDir(t *testing.T) {
	under := gitRepoWithFiles(t, "under-test.go")
	elsewhere := gitRepoWithFiles(t, "elsewhere.go")
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))

	tp, err := loadTrackedPaths(under)
	if err != nil {
		t.Fatalf("loadTrackedPaths() error = %v", err)
	}
	var got []string
	for _, segments := range tp.all {
		got = append(got, strings.Join(segments, "/"))
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "under-test.go" {
		t.Fatalf("universe = %v with GIT_DIR naming another checkout; want only the tree at repoRoot. An ambient repository pointer must not decide which tree a trigger is verified against", got)
	}
}

// TestLoadTrackedPaths_HonoursThePendingIndexAHookNames pins the one variable
// gitTreeEnv deliberately KEEPS, and it exists because the keep is the half of
// that function no other test can see. Its sibling above proves GIT_DIR is
// dropped; delete GIT_DIR from the retargeting map and that case goes red. ADD
// GIT_INDEX_FILE to the same map — the obvious one-line "finish the scrub"
// edit — and every test in this package and in cmd/ci-gates stayed green
// before this case existed, while the gate silently stopped doing its job.
//
// Why the asymmetry is correct rather than an oversight: a pre-commit hook
// exports GIT_INDEX_FILE naming the index that describes the tree being
// COMMITTED, which under `git commit -a` or `--only` is a pending lock file
// and not the index on disk. That pending tree is precisely what a pre-commit
// gate must validate its triggers against. Scrub the variable and the gate
// reads the on-disk index instead, so a commit deleting the last file a glob
// trigger names passes the very hook #6159 added it to fail. GIT_DIR carries
// no such meaning for us — dropping it makes git rediscover the same gitdir
// from repoRoot — which is why one is dropped and the other kept.
//
// Not parallel: t.Setenv forbids it, and the ambient variable is the subject.
func TestLoadTrackedPaths_HonoursThePendingIndexAHookNames(t *testing.T) {
	root := gitRepoWithFiles(t, "on-disk.go")
	pending := filepath.Join(t.TempDir(), "pending-index")
	if err := os.WriteFile(filepath.Join(root, "pending-only.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stage into the pending index ONLY, leaving the on-disk index holding
	// just on-disk.go. The two indexes now disagree, so the universe names
	// which one loadTrackedPaths actually read.
	cmd := exec.Command("git", "-C", root, "add", "--force", "--", "pending-only.go")
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+pending)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add into the pending index: %v\n%s", err, out)
	}
	t.Setenv("GIT_INDEX_FILE", pending)

	tp, err := loadTrackedPaths(root)
	if err != nil {
		t.Fatalf("loadTrackedPaths() error = %v", err)
	}
	var got []string
	for _, segments := range tp.all {
		got = append(got, strings.Join(segments, "/"))
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "pending-only.go" {
		t.Fatalf("universe = %v; want only the pending index's entry. A pre-commit hook names the index describing the tree being committed; scrub GIT_INDEX_FILE and the gate validates the tree on disk instead, so a commit deleting the last file a glob names passes the hook it should fail", got)
	}
}

// gitRepoWithFiles builds a git work tree holding exactly rels, staged.
func gitRepoWithFiles(t *testing.T, rels ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"add", "-A", "--force"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return root
}

// TestLoadTrackedPaths_FailsOnATreeGitDoesNotTrack pins the fail-closed rule
// at its source. An empty or unreadable tracked set means no glob trigger was
// verified, and the caller must be told that rather than handed an empty
// universe every trigger would then "fail" against for the wrong reason, or —
// worse — a silent skip.
func TestLoadTrackedPaths_FailsOnATreeGitDoesNotTrack(t *testing.T) {
	t.Parallel()

	tp, err := loadTrackedPaths(t.TempDir())
	if err == nil {
		t.Fatalf("loadTrackedPaths() on a non-work-tree returned %+v and no error; an unverifiable trigger set must not read as enumerable", tp)
	}
	if !strings.Contains(err.Error(), "tracked paths") {
		t.Fatalf("loadTrackedPaths() error = %v, want one naming the tracked path set it could not read", err)
	}
}

// TestLoadTrackedPaths_FailsOnAWorkTreeGitTracksNothingIn covers the other
// half of the fail-closed rule, and the half git reports as SUCCESS: in a
// freshly initialised repository `git ls-files` exits 0 and prints nothing, so
// the non-zero-exit case its sibling above covers never fires. Returning that
// empty universe would turn every glob trigger into a confident "matches no
// tracked path" error naming the trigger, when the true cause is that nothing
// was enumerated at all — 499 wrong errors instead of one right one, and an
// operator sent to rewrite a registry that is fine.
//
// README.md, doc.go and loadTrackedPaths' own comment each promise this case,
// which is why it is asserted rather than left to the reader.
func TestLoadTrackedPaths_FailsOnAWorkTreeGitTracksNothingIn(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cmd := exec.Command("git", "-C", root, "-c", "init.defaultBranch=main", "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	tp, err := loadTrackedPaths(root)
	if err == nil {
		t.Fatalf("loadTrackedPaths() on a work tree with nothing tracked in it returned %+v and no error; git exits 0 here, so only an explicit empty-set check reports it", tp)
	}
	if !strings.Contains(err.Error(), "git reported no tracked files") {
		t.Fatalf("loadTrackedPaths() error = %v, want one naming the empty tracked set as the reason no glob trigger could be verified", err)
	}
}
