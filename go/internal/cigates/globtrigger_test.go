// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// gitInitAndTrack turns a hermetic fixture tree into a git work tree and
// stages everything currently in it. Glob-trigger resolution (#6159) reads the
// tracked path set, so a fixture that is not a git work tree, or whose files
// are never staged, has an EMPTY path universe — the fixtures must therefore
// carry the same tracked truth the real repository does, or every glob case
// would pass or fail for the wrong reason.
//
// --force stages files regardless of the developer's global excludes: the
// fixture's tracked set must be a property of the test, never of the machine
// running it.
func gitInitAndTrack(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"add", "-A", "--force"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// writeTracked writes repo-relative files under root with placeholder content
// and stages them, so a glob trigger under test resolves against a tracked
// path rather than an incidental on-disk one.
func writeTracked(t *testing.T, root string, rels ...string) {
	t.Helper()
	writeUntracked(t, root, rels...)
	cmd := exec.Command("git", append([]string{"-C", root, "add", "--force", "--"}, rels...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

// writeUntracked writes repo-relative files under root without staging them.
// It exists to prove the negative half of the tracked-truth contract: a file
// that only exists on disk must NOT satisfy a glob trigger.
func writeUntracked(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("placeholder\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// globGate builds a one-gate registry whose only trigger is the glob under
// test, so a case asserts on glob resolution and not on some other check.
func globGate(trigger string) *cigates.Registry {
	return buildRegistry([]cigates.Gate{
		{
			ID:       "openapi-surface",
			Name:     "Verify OpenAPI Surface",
			Category: cigates.CategoryExactness,
			Tier:     cigates.TierPrePR,
			Blocking: true,
			Triggers: []string{trigger},
			Local:    &cigates.Local{Command: "bash scripts/verify-openapi.sh"},
			CI:       cigates.CI{Workflow: "verify-openapi.yml", Job: "Verify OpenAPI gate"},
		},
	})
}

// hermeticGlobRepo builds the standard fixture tree (one script, one workflow,
// both tracked) that every glob case starts from.
func hermeticGlobRepo(t *testing.T) string {
	t.Helper()
	return buildHermeticRepo(
		t,
		[]string{"scripts/verify-openapi.sh"},
		[]string{"verify-openapi.yml"},
	)
}

// errorNaming returns the first error mentioning every fragment, or "".
func errorNaming(errs []error, fragments ...string) string {
	for _, err := range errs {
		msg := err.Error()
		all := true
		for _, f := range fragments {
			if !strings.Contains(msg, f) {
				all = false
				break
			}
		}
		if all {
			return msg
		}
	}
	return ""
}

// TestValidate_GlobTriggerMatchingNothingFails is the #6159 teeth. A glob
// trigger that resolves to zero paths can never select its gate, so the gate
// reads as wired for a surface it can no longer guard — indistinguishable,
// from the registry's own output, from a trigger that works. #6055 closed this
// for literal triggers only; a glob was deliberately exempt, and two stale
// glob-shaped entries survived a full review round in #6142 with the registry
// gate green throughout.
func TestValidate_GlobTriggerMatchingNothingFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	reg := globGate("go/internal/collector/nothing/matches/this/**")

	errs := reg.Validate(root)
	if got := errorNaming(errs, "openapi-surface", "go/internal/collector/nothing/matches/this/**"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the gate and the glob trigger that matches nothing; without it a dead trigger reads as wired", errs)
	}
}

// TestValidate_GlobTriggerMatchingTrackedFilePasses pins the other side: a
// glob that resolves to a real tracked file is exactly what the registry is
// supposed to carry, and must never be flagged.
func TestValidate_GlobTriggerMatchingTrackedFilePasses(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/query/openapi_types.go")
	reg := globGate("go/internal/query/openapi*.go")

	if errs := reg.Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v, want none for a glob matching a tracked file", errs)
	}
}

// TestValidate_GlobTriggerMatchingOnlyADirectoryPasses proves the path
// universe carries directories, not only files. A trigger naming a directory
// is legitimate — the literal half of this check accepts one via os.Stat — so
// a glob whose only hits are directory prefixes must pass too. Enumerating
// `git ls-files` output alone, without the implied ancestor directories, would
// fail this case and would make the check reject working registry entries.
func TestValidate_GlobTriggerMatchingOnlyADirectoryPasses(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/cigates/glob.go")
	// "go/internal/*" is three segments; the tracked file is four, so the only
	// possible match is the directory "go/internal/cigates".
	reg := globGate("go/internal/*")

	if errs := reg.Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v, want none for a glob whose only match is a directory", errs)
	}
}

// TestValidate_GlobTriggerDoubleStarZeroSegmentPasses pins the case a naive
// matcher gets wrong. In this registry's dialect `**` matches ZERO or more
// segments, so "scripts/**/run-remote-e2e-*.sh" matches
// "scripts/run-remote-e2e-x.sh" with no intervening directory. path.Match and
// fnmatch both answer "no match" here, and wiring either of those in would
// report five live registry triggers as dead.
func TestValidate_GlobTriggerDoubleStarZeroSegmentPasses(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "scripts/run-remote-e2e-x.sh")
	reg := globGate("scripts/**/run-remote-e2e-*.sh")

	if errs := reg.Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v, want none: ** matches zero segments, so this trigger does select scripts/run-remote-e2e-x.sh", errs)
	}
}

// TestValidate_GlobTriggerMatchingOnlyAnUntrackedFileFails pins the choice of
// path universe. CI selects gates from the paths in a pull request diff, which
// are tracked paths; a build artifact or a scratch file satisfying a trigger
// would make the check pass locally and leave the gate dead in CI, and would
// make the verdict depend on whatever junk the developer's tree happens to
// hold. Reading the tree with a filesystem walk instead of the tracked set
// fails exactly here.
func TestValidate_GlobTriggerMatchingOnlyAnUntrackedFileFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeUntracked(t, root, "go/internal/query/openapi_types.go")
	reg := globGate("go/internal/query/openapi*.go")

	errs := reg.Validate(root)
	if got := errorNaming(errs, "openapi-surface", "go/internal/query/openapi*.go"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the trigger: an untracked file must not satisfy a trigger CI can never select on", errs)
	}
}

// TestValidate_GlobTriggerFailsWhenTrackedPathsCannotBeEnumerated is the
// fail-closed rule. If the tracked path set cannot be read, every glob trigger
// is unverifiable — and an unverifiable trigger must not read as present, the
// same discipline the literal check applies to a stat it could not complete.
func TestValidate_GlobTriggerFailsWhenTrackedPathsCannotBeEnumerated(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/query/openapi_types.go")
	// Remove the repository metadata: the tree still holds the file the
	// trigger names, so only a fail-closed check reports anything here.
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	reg := globGate("go/internal/query/openapi*.go")

	errs := reg.Validate(root)
	if got := errorNaming(errs, "tracked paths"); got == "" {
		t.Fatalf("Validate() errors = %v, want one reporting that the tracked path set could not be enumerated; an unverifiable glob trigger must not pass silently", errs)
	}
	// The enumeration failure must be reported ONCE, not once per glob
	// trigger. Skipping the per-trigger check when the universe is unknown is
	// what keeps it to one: without that skip every glob derives its own
	// "matches no tracked path" error from a set that was never loaded, so the
	// committed registry's 499 glob triggers would emit 500 errors instead of
	// 1 -- each confidently naming a trigger that is in fact fine, sending an
	// operator to rewrite a registry that is correct. validate.go, this file's
	// enumeration guard, and README.md all promise this; nothing asserted it.
	if len(errs) != 1 {
		t.Fatalf("Validate() returned %d errors (%v), want exactly 1: the unenumerable universe is reported once, not derived per glob trigger", len(errs), errs)
	}
}

// TestValidate_GlobTriggerEscapingRootFails covers the glob-shaped sibling of
// the literal containment guard: a trigger reaching out of the tree names
// nothing the registry can select on, so it is reported rather than resolved
// against unrelated host files.
func TestValidate_GlobTriggerEscapingRootFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	reg := globGate("../etc/*.conf")

	errs := reg.Validate(root)
	if got := errorNaming(errs, "openapi-surface", "../etc/*.conf"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the escaping glob trigger", errs)
	}
}

// TestValidate_CommittedRegistryGlobTriggersAllResolve runs the check against
// the real registry and the real tree. It is the case that makes the gate
// non-vacuous in practice: delete or rename a file a glob trigger names, and
// this test goes red in the same run that the registry gate does.
func TestValidate_CommittedRegistryGlobTriggersAllResolve(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}

	var stale []string
	for _, e := range reg.Validate(repoRoot) {
		if msg := e.Error(); strings.Contains(msg, "tracked path") {
			stale = append(stale, msg)
		}
	}
	if len(stale) != 0 {
		t.Fatalf("committed registry has %d trigger(s) that resolve to nothing:\n%s", len(stale), strings.Join(stale, "\n"))
	}
}

// TestValidate_GlobTriggerGoesStaleWhenItsLastFileIsDeleted walks the failure
// the issue describes, in one test: a registry that validates clean, then the
// file its glob names is deleted without the registry being updated, and the
// same registry must now fail. Asserting only the end state would leave the
// case unable to distinguish a working check from one that rejects the
// fixture for an unrelated reason, and this is the transition #6142 got no
// signal from.
func TestValidate_GlobTriggerGoesStaleWhenItsLastFileIsDeleted(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/storage/cypher/documentation_edges.go")
	reg := globGate("go/internal/storage/cypher/*documentation*.go")

	if errs := reg.Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v before the deletion, want none", errs)
	}

	// git rm is the whole deletion: index entry and working file both go. -f
	// because the fixture is never committed, so every file in it reads as a
	// staged addition git would otherwise refuse to drop.
	cmd := exec.Command("git", "-C", root, "rm", "-q", "-f", "go/internal/storage/cypher/documentation_edges.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rm: %v\n%s", err, out)
	}

	errs := reg.Validate(root)
	if got := errorNaming(errs, "openapi-surface", "go/internal/storage/cypher/*documentation*.go"); got == "" {
		t.Fatalf("Validate() errors = %v after deleting the only file the trigger named, want one naming the now-stale trigger", errs)
	}
}
