// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"path"
	"strings"
)

// PathRules is the compiled form of the Options rules that judge a path by its
// repo-relative location alone: ignored directory names, hidden paths and their
// preserved prefixes, ignored extensions, and ignored/preserved path globs.
// The filesystem walk applies these same rules, so a caller that needs to judge
// a path the walk never visited (such as the resolved target of a symlink) can
// ask the same question without copying the matchers. Repo-local .gitignore and
// .eshuignore rules are not part of PathRules; they are applied per repository
// group after the walk.
//
// The zero value excludes nothing. A PathRules is read-only after NewPathRules
// returns and is safe for concurrent use.
type PathRules struct {
	ignoredDirs        map[string]struct{}
	ignoredExts        []string
	ignoreHidden       bool
	preservedHidden    []string
	ignoredPathGlobs   []normalizedPathGlobRule
	preservedPathGlobs []string
}

// NewPathRules compiles the location-only rules from opts.
func NewPathRules(opts Options) PathRules {
	return PathRules{
		ignoredDirs:        normalizeIgnoredDirs(opts.IgnoredDirs),
		ignoredExts:        normalizeExtensions(opts.IgnoredExtensions),
		ignoreHidden:       opts.IgnoreHidden,
		preservedHidden:    normalizePrefixes(opts.PreservedHiddenPrefixes),
		ignoredPathGlobs:   normalizePathGlobRules(opts.IgnoredPathGlobs),
		preservedPathGlobs: normalizePathGlobPatterns(opts.PreservedPathGlobs),
	}
}

// ExcludesFile reports whether the walk would skip a regular file at the
// repo-relative path rel: because an ancestor directory is pruned (ignored
// name, hidden, or a path glob), or because the file itself is hidden, has an
// ignored extension, or matches an ignored path glob. It does not ask whether
// a parser supports the file. An empty or root path is excluded.
func (r PathRules) ExcludesFile(rel string) bool {
	rel = normalizeRepoRelativePath(rel)
	if rel == "" {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if bucket, _ := r.dirSkip(strings.Join(parts[:i], "/"), parts[i-1]); bucket != skipNone {
			return true
		}
	}
	if bucket, _ := r.fileSkipBeforeMatch(rel, path.Base(rel)); bucket != skipNone {
		return true
	}
	_, ok := r.fileSkipByGlob(rel)
	return ok
}

// skipBucket names the DiscoveryStats bucket a walk skip is counted in.
type skipBucket int

const (
	skipNone skipBucket = iota
	// skipDirByName counts in DirsSkippedByName (ignored names and ".hidden").
	skipDirByName
	// skipDirByUser counts in DirsSkippedByUser under the glob rule's reason.
	skipDirByUser
	// skipFileHidden counts in FilesSkippedHidden.
	skipFileHidden
	// skipFileByExtension counts in FilesSkippedByExtension.
	skipFileByExtension
)

// dirSkip reports whether the walk prunes the directory at rel (whose last
// segment is name), the bucket it is counted in, and the bucket key.
func (r PathRules) dirSkip(rel string, name string) (skipBucket, string) {
	if isIgnoredDir(name, r.ignoredDirs) {
		return skipDirByName, strings.ToLower(name)
	}
	if r.ignoreHidden && isHiddenPath(rel) && !preservesHiddenPath(rel, r.preservedHidden) {
		return skipDirByName, ".hidden"
	}
	if reason, ok := matchedIgnoredPathGlob(rel, true, r.ignoredPathGlobs, r.preservedPathGlobs); ok {
		return skipDirByUser, reason
	}
	return skipNone, ""
}

// fileSkipBeforeMatch reports the skips the walk applies to a file before it
// asks the supported-file matcher: hidden paths, then ignored extensions.
func (r PathRules) fileSkipBeforeMatch(rel string, name string) (skipBucket, string) {
	if shouldSkipFile(rel, r.ignoreHidden, r.preservedHidden) {
		return skipFileHidden, ""
	}
	if ext := matchedIgnoredExtension(name, r.ignoredExts); ext != "" {
		return skipFileByExtension, ext
	}
	return skipNone, ""
}

// fileSkipByGlob reports whether a supported file matches an ignored path glob
// that no preserved glob overrides, and the operator-facing reason.
func (r PathRules) fileSkipByGlob(rel string) (string, bool) {
	return matchedIgnoredPathGlob(rel, false, r.ignoredPathGlobs, r.preservedPathGlobs)
}
