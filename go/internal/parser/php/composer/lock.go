// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
)

// lockPrefix is one namespace prefix a locked package autoloads. name is ""
// when the package's name is outside the composer grammar or the prefix comes
// from psr-0: such a prefix still competes in the longest-match rule, so it
// can block a shorter prefix of another package, but it never produces a key.
type lockPrefix struct {
	prefix string
	name   string
}

// lockIndex is the prefix list of one composer.lock, read from both packages
// and packages-dev.
type lockIndex struct {
	prefixes []lockPrefix
}

// lockCache memoizes parsed composer.lock files by (path, stat). A lock is
// often hundreds of kilobytes and every PHP file under it reads it, so it is
// parsed once per content generation, not once per file.
var lockCache = project.NewScopeCache[lockIndex]()

// parseLock builds the prefix list of a composer.lock document. A document
// that does not decode yields an empty list, so its classes stay unkeyed.
func parseLock(content []byte) lockIndex {
	type lockedPackage struct {
		Name     any `json:"name"`
		Autoload struct {
			PSR4 map[string]any `json:"psr-4"`
			PSR0 map[string]any `json:"psr-0"`
		} `json:"autoload"`
	}
	var document struct {
		Packages    []lockedPackage `json:"packages"`
		PackagesDev []lockedPackage `json:"packages-dev"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return lockIndex{}
	}
	var index lockIndex
	for _, locked := range append(document.Packages, document.PackagesDev...) {
		name, _ := locked.Name.(string)
		if !ValidPackageName(name) {
			name = ""
		}
		for rawPrefix := range locked.Autoload.PSR4 {
			if prefix := normalizePrefix(rawPrefix); prefix != "" {
				index.prefixes = append(index.prefixes, lockPrefix{prefix: prefix, name: name})
			}
		}
		for rawPrefix := range locked.Autoload.PSR0 {
			if prefix := normalizePrefix(rawPrefix); prefix != "" {
				index.prefixes = append(index.prefixes, lockPrefix{prefix: prefix})
			}
		}
	}
	return index
}

// lookup returns the package whose prefix is the longest one covering fqcn,
// and whether any prefix covers it at all. A tie between two packages, or a
// longest prefix that yields no key, returns ("", true): the class is covered
// but cannot be keyed, so the caller must stop rather than ask another lock.
func (index lockIndex) lookup(fqcn string) (string, bool) {
	best := -1
	name := ""
	tied := false
	for _, entry := range index.prefixes {
		if !coversClass(entry.prefix, fqcn) || len(entry.prefix) < best {
			continue
		}
		if len(entry.prefix) > best {
			best, name, tied = len(entry.prefix), entry.name, false
			continue
		}
		if entry.name != name {
			tied = true
		}
	}
	if best < 0 {
		return "", false
	}
	if tied {
		return "", true
	}
	return name, true
}

// readLock returns the cached prefix list of the composer.lock at path, and
// false when no file exists there.
func readLock(path string) (lockIndex, bool) {
	stat, ok := project.StatFor(path)
	if !ok {
		return lockIndex{}, false
	}
	return lockCache.Get(path, stat, func() lockIndex {
		content, err := os.ReadFile(path)
		if err != nil {
			return lockIndex{}
		}
		return parseLock(content)
	}), true
}

// LockedPackage returns the composer package that provides the class fqcn to
// the file at path, or "" when none can be proven. It reads every composer.lock
// from the file's directory up to repoRoot, nearest first, and the first lock
// whose packages or packages-dev autoload a prefix covering fqcn decides: the
// longest covering prefix wins, and a tie, a psr-0 prefix, or a name outside
// the composer grammar yields "". A lock lists only installed dependencies,
// never the root package, so the consumer's own classes are never resolved.
func LockedPackage(repoRoot string, path string, fqcn string) string {
	repoRoot = project.CleanPath(repoRoot)
	path = project.CleanPath(path)
	if repoRoot == "" || path == "" || fqcn == "" {
		return ""
	}
	for _, dir := range ancestorDirs(repoRoot, path) {
		index, ok := readLock(filepath.Join(dir, "composer.lock"))
		if !ok {
			continue
		}
		if name, covered := index.lookup(fqcn); covered {
			return name
		}
	}
	return ""
}

// HasLock reports whether any composer.lock sits between the file at path and
// repoRoot. Callers use it to skip binding work for a file no lock can key.
func HasLock(repoRoot string, path string) bool {
	repoRoot = project.CleanPath(repoRoot)
	path = project.CleanPath(path)
	for _, dir := range ancestorDirs(repoRoot, path) {
		if _, ok := project.StatFor(filepath.Join(dir, "composer.lock")); ok {
			return true
		}
	}
	return false
}
