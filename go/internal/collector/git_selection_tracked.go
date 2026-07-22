// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"path/filepath"
)

// collectorTrackedResolver lazily and memoized resolves git-tracked file sets
// for the filesystem managed-copy walk (copyRepositoryTree), keyed by the
// NEAREST enclosing git root for each queried path — not always walkRoot
// (issue #5658 P1a). A nested repository inside a filesystem-source checkout
// (e.g. a submodule directory with its own ".git") has its own tracked set,
// distinct from the outer repo's: the outer repo's own `git ls-files` lists
// the nested repo's gitlink path (e.g. "modules/nested") but NOT that nested
// repo's own tracked files (e.g. "modules/nested/terraform.tfstate").
// Resolving only once at walkRoot would silently drop a force-added file
// inside a nested repo whose OWN .gitignore/.eshuignore rule matches it.
//
// Both trackedFile and trackedUnderDir are nil-receiver-safe (a nil
// *collectorTrackedResolver always reports false): fingerprintTree leaves
// collectorIgnoreCaches.tracked nil and never resolves a tracked set, since
// it walks the raw FilesystemRoot, which may span multiple repos or no git
// checkout at all.
//
// Resolution is lazy and memoized twice over (issue #5658 P1b):
//   - rootByDir memoizes the upward nearest-git-root walk per directory, so
//     repeated queries for files in the same directory never re-walk it.
//   - setByRoot memoizes resolve(root) per git root, so a root with more than
//     one gitignore/eshuignore-matched candidate still spawns at most one
//     `git ls-files` subprocess. A root's PRESENCE as a setByRoot key (not the
//     value, which may legitimately be nil) marks that resolve() already ran
//     for it — callers must check via the map's second (ok) return, never by
//     testing the value for nil.
//
// Neither method calls resolve() unless a caller actually needs a
// tracked-status decision — shouldSkipFilesystemEntry checks the gitignore
// match first and calls trackedFile/trackedUnderDir only on a match, so a
// repo (or nested repo) with no ignore-matched candidate never spawns the
// subprocess at all.
type collectorTrackedResolver struct {
	// resolve queries one git root's tracked file set. Production wires this
	// to a closure over ctx calling gitTrackedFiles directly (this walk
	// operates on real source paths, not gitTreePath-mirrored ones, so it
	// does not need buildGitTrackedResolver's managed-copy indirection).
	// Tests inject a call-counting/fake stand-in.
	resolve func(gitRoot string) (map[string]struct{}, bool)
	// walkRoot is copyRepositoryTree's sourceRoot: the fallback git root
	// when no nearer ".git" marker is found between a queried path and
	// walkRoot, and the upper bound the nearest-root walk never climbs past.
	walkRoot string
	// ctx and logger are threaded to warnGitTrackedFilesUnavailable exactly
	// as buildGitTrackedResolver (git_tracked_files.go) does for the
	// discovery-side (site 1) resolver — logger may be nil.
	ctx    context.Context
	logger *slog.Logger

	rootByDir map[string]string
	setByRoot map[string]map[string]struct{}
}

// trackedFile reports whether fullPath — an absolute file path somewhere
// under r.walkRoot — is tracked by the nearest enclosing git repository
// (issue #5591, #5658 P1a). A nil receiver reports false.
func (r *collectorTrackedResolver) trackedFile(fullPath string) bool {
	if r == nil {
		return false
	}
	root := r.nearestGitRoot(filepath.Dir(fullPath))
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return false
	}
	return isCollectorTrackedFile(rel, r.trackedSetForRoot(root))
}

// trackedUnderDir reports whether any path the nearest enclosing git
// repository tracks lives at or beneath dirPath — an absolute directory path
// somewhere under r.walkRoot, and possibly itself a nested repository root
// (issue #5591, #5658 P1a). shouldSkipFilesystemEntry uses this before
// pruning a whole directory on a gitignore match: a directory-level prune
// would hide every tracked file beneath it too. A nil receiver reports false.
func (r *collectorTrackedResolver) trackedUnderDir(dirPath string) bool {
	if r == nil {
		return false
	}
	root := r.nearestGitRoot(dirPath)
	rel, err := filepath.Rel(root, dirPath)
	if err != nil {
		return false
	}
	return collectorTrackedPathsUnderDir(rel, r.trackedSetForRoot(root))
}

// nearestGitRoot walks upward from startDir (a directory) to r.walkRoot,
// returning the first directory with its own ".git" marker (hasGitDirMarker
// — already handles a directory, worktree/submodule gitlink file, or
// symlink), or r.walkRoot itself when no nearer marker exists. Every visited
// directory is memoized in r.rootByDir so a repeated query for the same
// directory (or a descendant of one already visited) never re-walks it.
func (r *collectorTrackedResolver) nearestGitRoot(startDir string) string {
	current := filepath.Clean(startDir)
	visited := make([]string, 0, 8)
	for {
		if root, ok := r.rootByDir[current]; ok {
			for _, dir := range visited {
				r.rootByDir[dir] = root
			}
			return root
		}

		visited = append(visited, current)
		if hasGitDirMarker(current) {
			for _, dir := range visited {
				r.rootByDir[dir] = current
			}
			return current
		}
		if current == r.walkRoot {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	for _, dir := range visited {
		r.rootByDir[dir] = r.walkRoot
	}
	return r.walkRoot
}

// trackedSetForRoot resolves (and memoizes) the tracked file set for one git
// root, calling r.resolve at most once per root regardless of how many
// trackedFile/trackedUnderDir queries land on it (issue #5658 P1b). A failed
// resolution stores nil and, when root has its own ".git" marker (so
// `git ls-files` was expected to succeed), fires the same
// warnGitTrackedFilesUnavailable WARN buildGitTrackedResolver's case 1 uses —
// this is the filesystem managed-copy path's mirror of that diagnosability
// guarantee (issue #5591 P2).
func (r *collectorTrackedResolver) trackedSetForRoot(root string) map[string]struct{} {
	if tracked, attempted := r.setByRoot[root]; attempted {
		return tracked
	}

	tracked, ok := r.resolve(root)
	if !ok {
		tracked = nil
		if hasGitDirMarker(root) {
			warnGitTrackedFilesUnavailable(r.ctx, r.logger, root)
		}
	}
	r.setByRoot[root] = tracked
	return tracked
}
