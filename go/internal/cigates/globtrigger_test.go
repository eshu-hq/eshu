// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestValidate_GlobTriggerMatchingOnlyADirectoryFails pins the universe to the
// paths Select can actually receive. This case used to assert the opposite —
// that a directory-only glob PASSES — on the reasoning that the literal half of
// the check accepts a directory via os.Stat, so the glob half should too. That
// reasoning made the validator certify a trigger that can never fire, which is
// the exact defect #6159 exists to remove.
//
// Select matches triggers against CHANGED paths, and every caller supplies
// files: `git diff --name-only` names files, and GitHub's pull-files response
// names files. So MatchGlob("go/internal/*", "go/internal/cigates") is true
// while MatchGlob("go/internal/*", "go/internal/cigates/glob.go") is false, and
// only the second is a question Select ever asks. Deriving ancestor directories
// into the universe let "go/cmd/collector-**" sit in the committed registry as
// the golden-corpus gate's only claim on the collector binaries while selecting
// nothing (#6223 review).
//
// The error must name the directory and the "dir/**" spelling: without that a
// reader sees "matches nothing" and concludes the surface was deleted, when the
// surface is present and the trigger is one segment short.
func TestValidate_GlobTriggerMatchingOnlyADirectoryFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/cigates/glob.go")
	// "go/internal/*" is three segments; the tracked file is four, so the only
	// possible match is the directory "go/internal/cigates" — which Select is
	// never handed.
	reg := globGate("go/internal/*")

	errs := reg.Validate(root)
	got := errorNaming(errs, "openapi-surface", "go/internal/*")
	if got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the trigger: a glob whose only match is a directory can never select its gate", errs)
	}
	for _, want := range []string{"go/internal/cigates", "go/internal/cigates/**"} {
		if !strings.Contains(got, want) {
			t.Errorf("Validate() error = %q, want it to contain %q so the reader is told the surface exists and how to spell the trigger", got, want)
		}
	}
}

// TestValidate_GlobTriggerGlueingDoubleStarToASegmentFails is the live
// registry defect this rule caught, kept as its own case because the spelling
// is the trap rather than the concept. GitHub Actions and dorny/paths-filter
// let "**" cross "/", so "go/cmd/collector-**" matches every file under every
// collector command there and the gate workflow using it is correct. This
// package's MatchGlob treats "**" as a WHOLE segment, so glued to a prefix it
// degrades to an ordinary single-segment wildcard and stops at the directory.
//
// Copying a trigger from a workflow's on.pull_request.paths is the normal way
// the registry is kept in lockstep with CI (#5538 did exactly that), so this
// dialect gap is a mistake that will be made again, and the check must catch
// it rather than manufacture a directory match for it.
func TestValidate_GlobTriggerGlueingDoubleStarToASegmentFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/cmd/collector-tempo/main.go")
	reg := globGate("go/cmd/collector-**")

	errs := reg.Validate(root)
	if got := errorNaming(errs, "openapi-surface", "go/cmd/collector-**"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the trigger: \"**\" glued to a segment does not cross \"/\" in this dialect, so it matches only the directory", errs)
	}

	// The corrected spelling must pass, or the message above sends the reader
	// somewhere that does not work either.
	if errs := globGate("go/cmd/collector-*/**").Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v, want none: \"go/cmd/collector-*/**\" is the spelling that does select the tracked file", errs)
	}
}

// TestValidate_LiteralTriggerNamingADirectoryFails closes the same hole on the
// literal half. #6055 stat-checked a literal trigger and accepted a directory
// because os.Stat does, which is the assumption the glob half was then built to
// match. It is wrong for the same reason and in the same way: selection is
// handed file paths, and MatchGlob("go/internal/cigates",
// "go/internal/cigates/glob.go") is false, so a literal trigger naming a
// directory can never select its gate either.
//
// No trigger in the committed registry has this shape today (measured: 0 of
// 916 literal triggers stat as a directory), so this closes the shape rather
// than repairing a live break — but leaving the two halves disagreeing would
// invite exactly the "make the glob half accept directories too" fix that
// caused the defect above.
func TestValidate_LiteralTriggerNamingADirectoryFails(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	writeTracked(t, root, "go/internal/cigates/glob.go")
	reg := globGate("go/internal/cigates")

	errs := reg.Validate(root)
	got := errorNaming(errs, "openapi-surface", "go/internal/cigates")
	if got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the trigger: a literal trigger naming a directory can never select its gate", errs)
	}
	if !strings.Contains(got, "go/internal/cigates/**") {
		t.Errorf("Validate() error = %q, want it to name the \"dir/**\" spelling that does select", got)
	}

	// The file the directory contains is still a perfectly good literal
	// trigger: this rejects directories, not the literal shape.
	if errs := globGate("go/internal/cigates/glob.go").Validate(root); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v, want none for a literal trigger naming a tracked file", errs)
	}
}

// TestValidate_GlobTriggerNoMatchIsBoundedOnAdversarialPattern pins the cost
// of resolution on the path Validate actually takes. matchesAny does not go
// through MatchGlob — it shares the matcher core via matchSegmentsBounded and
// decides memoization once per pattern — so the bound pinned in
// TestMatchGlob_NoMatchIsBoundedOnAdversarialPattern does not cover this
// caller, and a mutation disabling the memo here survives that test.
//
// This is the exposure the always-on check creates: MatchGlob answers one
// (trigger, path) pair, while Validate runs every trigger against the whole
// ~20k-file universe. A pattern that takes 24.7s to answer "no match" for ONE
// candidate does not make the gate slow, it stops the gate finishing.
//
// Sizes and budget are the same measured pair the MatchGlob case uses, and for
// the same reason — see that comment for the growth table and why a 2s budget
// over a smaller fixture would not separate memoized from unmemoized at all.
// Both spellings are here because collapsing consecutive "**" into one, the
// cheaper fix offered alongside memoization, answers only the first.
//
// The DEEP tracked file is load-bearing and is the reason this case is not a
// copy of the MatchGlob one. The blow-up is exponential in the number of "**"
// segments AND the length of the path they are matched against, and the
// standard fixture's deepest tracked path is four segments — against which even
// 17 "**" resolves instantly. Written without it, this case passed with the
// memo disabled: it asserted the right thing about a candidate too shallow to
// make the assertion mean anything.
func TestValidate_GlobTriggerNoMatchIsBoundedOnAdversarialPattern(t *testing.T) {
	t.Parallel()
	root := hermeticGlobRepo(t)
	// 32 segments, matching the longest candidate in the growth table above.
	writeTracked(t, root, strings.TrimSuffix(strings.Repeat("s/", 32), "/")+".go")

	for _, tc := range []struct {
		name    string
		trigger string
	}{
		{"consecutive double stars", strings.Repeat("**/", 17) + "zzz-no-such-literal"},
		{"double stars separated by literals", strings.Repeat("**/s/", 15) + "zzz-no-such-literal"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const budget = 2 * time.Second
			done := make(chan []error, 1)
			go func() { done <- globGate(tc.trigger).Validate(root) }()
			select {
			case errs := <-done:
				if errorNaming(errs, "openapi-surface", tc.trigger) == "" {
					t.Fatalf("Validate() errors = %v, want one naming the trigger: it matches nothing", errs)
				}
			case <-time.After(budget):
				t.Fatalf("Validate() did not answer within %v for %q — the exponential \"**\" backtracking is back on the resolution path", budget, tc.trigger)
			}
		})
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

	// No substring filter. An earlier version of this guard collected only
	// errors containing "tracked path", but the stale-trigger error says
	// "matches no tracked file" -- so seeding a stale glob into the committed
	// registry left this test GREEN, which is precisely the defect #6159
	// exists to remove, sitting inside the test written to catch it. Asserting
	// on the whole error set instead cannot drift when a message is reworded:
	// the committed registry is expected to validate completely clean, which
	// is the same thing scripts/verify-ci-gates-registry.sh asserts.
	if errs := reg.Validate(repoRoot); len(errs) != 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		t.Fatalf("committed registry produced %d validation error(s):\n%s", len(errs), strings.Join(msgs, "\n"))
	}
}

// TestValidate_CommittedRegistryGuardCatchesAStaleGlob proves the guard above
// can actually fail. It rebuilds the committed registry in memory with one
// bogus glob trigger appended and asserts Validate reports it -- the negative
// half the original guard lacked, which is how a filter that matched no real
// error survived review as a passing test.
func TestValidate_CommittedRegistryGuardCatchesAStaleGlob(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}
	if len(reg.Gates) == 0 {
		t.Fatal("committed registry has no gates")
	}
	reg.Gates[0].Triggers = append(reg.Gates[0].Triggers, "go/internal/cigates/*this-surface-does-not-exist*.go")
	errs := reg.Validate(repoRoot)
	if len(errs) == 0 {
		t.Fatal("Validate() accepted a glob trigger matching no tracked file; the committed-registry guard cannot fail and would not notice a real stale trigger")
	}
	var named bool
	for _, e := range errs {
		if strings.Contains(e.Error(), "this-surface-does-not-exist") {
			named = true
		}
	}
	if !named {
		t.Fatalf("Validate() errored but named no trigger: %v", errs)
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
