// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// trackedPaths is the path universe a glob registry trigger is resolved
// against: every repo-relative path git tracks at a repo root, plus every
// directory implied by one. Paths are stored pre-split on "/" so a match costs
// no allocation, and indexed by first segment so a trigger whose own first
// segment is a literal ("go/**", "scripts/lib/*.sh") never looks at the other
// buckets.
//
// # Why the tracked set, and not a filesystem walk
//
// The registry's job is to select gates from the paths in a change, and both
// CI (a pull-request file list) and the local lane (a git diff) hand it TRACKED
// paths. Resolving a trigger against a filesystem walk instead would let an
// untracked build artifact, a gitignored cache, or a scratch file satisfy a
// trigger that CI can never select on — a gate reading as wired while it is
// dead, which is the exact defect #6159 closes. It would also make the verdict
// depend on what the developer's tree happens to hold, so the same registry
// would validate locally and fail in CI, or worse, the reverse.
//
// The cost of that choice is a dependency on git and on repoRoot being a work
// tree. That is deliberate and fails closed: see loadTrackedPaths.
//
// # Why directories are in the universe
//
// git tracks files, never directories, but a trigger naming a directory is
// legitimate — the literal half of this check accepts one, because os.Stat
// does. A glob such as "go/internal/*" whose only hits are directory prefixes
// is a working trigger, so the implied ancestors of every tracked file are
// part of the universe.
type trackedPaths struct {
	// all holds every path in the universe, pre-split on "/".
	all [][]string
	// byHead indexes all by its first segment.
	byHead map[string][][]string
}

// loadTrackedPaths enumerates the tracked path universe at repoRoot ONCE per
// Validate call. The committed registry carries ~500 glob triggers against a
// ~22k-path universe, so resolving triggers against a per-trigger enumeration
// would repeat the same work 500 times — the shape checkTriggerPathsExist's
// own comment records a previous version of this file falling into.
//
// Every failure is an error, never a skip. If the tracked set cannot be read —
// git missing, repoRoot not a work tree, git exiting non-zero, or an empty
// tracked set, which is not a repository anyone runs this against — then no
// glob trigger has been verified, and an unverifiable trigger must not read as
// present. That is the same fail-closed rule the literal check applies to a
// stat it could not complete.
//
// The child process inherits this process's environment on purpose. Under a
// git hook, GIT_DIR and GIT_INDEX_FILE describe the very tree being committed,
// which is the state a pre-commit gate should be validating; clearing them
// would silently validate a different one.
func loadTrackedPaths(repoRoot string) (*trackedPaths, error) {
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "-z")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// -z keeps paths raw: without it git applies core.quotepath and would hand
	// back an escaped, quoted string for any path outside ASCII, which would
	// then match no trigger and read as a stale entry.
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("tracked paths under %q could not be enumerated: %s", repoRoot, detail)
	}

	tp := &trackedPaths{byHead: make(map[string][][]string)}
	seen := make(map[string]struct{})
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		tp.add(path, seen)
		// git tracks no directories, so every ancestor of a tracked file is
		// added here or a directory-shaped trigger would read as stale. An
		// ancestor already in the universe brought its own ancestors with it,
		// so the walk stops there rather than re-deriving the whole prefix
		// chain once per file in the tree.
		for i := strings.LastIndexByte(path, '/'); i > 0; i = strings.LastIndexByte(path, '/') {
			path = path[:i]
			if !tp.add(path, seen) {
				break
			}
		}
	}
	if len(tp.all) == 0 {
		return nil, fmt.Errorf(
			"tracked paths under %q could not be enumerated: git reported no tracked files, so no glob trigger can be verified",
			repoRoot,
		)
	}
	return tp, nil
}

// add records path in the universe, reporting whether it was new. Ancestor
// directories are shared by many files, so the dedupe is what keeps the
// universe proportional to the tree rather than to the file count times its
// depth.
func (t *trackedPaths) add(path string, seen map[string]struct{}) bool {
	if _, ok := seen[path]; ok {
		return false
	}
	seen[path] = struct{}{}
	segments := strings.Split(path, "/")
	t.all = append(t.all, segments)
	t.byHead[segments[0]] = append(t.byHead[segments[0]], segments)
	return true
}

// matchesAny reports whether pattern selects at least one path in the
// universe, under the same semantics as MatchGlob: `**` matches zero or more
// segments, `*` stays inside one, and an anchored or directory-style pattern
// (leading or trailing "/") matches nothing at all.
//
// It shares MatchGlob's matcher rather than calling MatchGlob per path, which
// would re-split the pattern and the path on every one of the ~11M
// (trigger, path) pairs the committed registry produces. Measured on this
// repository: 666ms for the per-path MatchGlob form against 53ms for this one
// (plus 3ms to build the index), same verdicts.
// TestTrackedPaths_MatchesAnyAgreesWithMatchGlob pins that equivalence, since
// the two now share a matcher core but not the guard clauses around it.
func (t *trackedPaths) matchesAny(pattern string) bool {
	if strings.HasPrefix(pattern, "/") || strings.HasSuffix(pattern, "/") {
		return false
	}
	patternSegments := strings.Split(pattern, "/")
	candidates := t.all
	// A first segment holding no "*" (so neither a wildcard nor "**") must
	// match the path's first segment literally, which is what makes the index
	// sound: nothing outside that bucket can match.
	if !strings.Contains(patternSegments[0], "*") {
		candidates = t.byHead[patternSegments[0]]
	}
	for _, path := range candidates {
		if matchSegments(patternSegments, path) {
			return true
		}
	}
	return false
}

// hasGlobTrigger reports whether any gate carries a non-literal trigger. It
// gates the enumeration: a registry of purely literal triggers (and the
// gates-less fixtures cmd/ci-gates validates) needs no tracked path set, and
// must not fail merely because it was pointed at a tree git does not track.
func (r *Registry) hasGlobTrigger() bool {
	for _, g := range r.Gates {
		for _, trigger := range g.Triggers {
			if !isLiteralTrigger(trigger) {
				return true
			}
		}
	}
	return false
}

// checkGlobTriggerResolves reports a glob trigger that selects nothing in the
// tree (#6159). A glob was exempt from the #6055 existence check on the
// reasoning that it could legitimately match zero files today and still be
// valid future-proofing. It cannot: an entry that matches nothing can never
// select its gate, so the gate reads as wired for a surface it no longer
// guards, and nothing in the registry's own output distinguishes that from a
// trigger that works. Two stale glob-shaped entries survived a full review
// round in #6142 with the registry gate green the whole time.
//
// A trigger reaching out of the tree ("../etc/*.conf") lands here as well:
// every path in the universe is repo-relative, so an escaping pattern matches
// none of them and is reported by the same rule, without ever resolving it
// against unrelated host files.
//
// tracked is nil only when the universe could not be enumerated, which
// Validate has already reported as its own error; skipping here reports that
// failure once instead of once per trigger, and the run still fails.
func checkGlobTriggerResolves(tracked *trackedPaths, gateID, trigger, repoRoot string) error {
	if tracked == nil || tracked.matchesAny(trigger) {
		return nil
	}
	return fmt.Errorf(
		"gate %q: trigger %q matches no tracked path under %q — a glob that resolves to nothing can never select this gate, so the surface it names silently loses its guard",
		gateID, trigger, repoRoot,
	)
}
