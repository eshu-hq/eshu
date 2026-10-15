// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
)

// lockPrefix is one namespace prefix a locked package autoloads. name is ""
// when the package's name is outside the composer grammar, the prefix comes
// from psr-0, or it is a psr-4 prefix without the trailing '\': such a prefix still competes in the longest-match rule, so it
// can block a shorter prefix of another package, but it never produces a key.
type lockPrefix struct {
	prefix string
	name   string
}

// lockIndex is the prefix table of one composer.lock, read from both packages
// and packages-dev. Prefixes that end in '\' (every valid psr-4 prefix) are
// grouped by prefix so a lookup costs one map probe per namespace segment of
// the class; the rest (psr-0 prefixes such as "Twig_") are scanned.
type lockIndex struct {
	bySegmentPrefix map[string][]string
	unsegmented     []lockPrefix
}

// lockCache memoizes parsed composer.lock files by (path, stat). A lock is
// often hundreds of kilobytes and every PHP file under it reads it, so it is
// parsed once per content generation, not once per file.
var lockCache = project.NewScopeCache[lockIndex]()

// parseLock builds the prefix table of a composer.lock document. A document
// that does not decode yields an empty table, so its classes stay unkeyed.
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
	index := lockIndex{bySegmentPrefix: map[string][]string{}}
	if err := json.Unmarshal(content, &document); err != nil {
		return index
	}
	add := func(prefix string, name string) {
		if prefix == "" {
			return
		}
		if strings.HasSuffix(prefix, `\`) {
			index.bySegmentPrefix[prefix] = append(index.bySegmentPrefix[prefix], name)
			return
		}
		// Composer refuses a psr-4 prefix without the trailing '\', and the
		// producer rule drops it, so such an entry (and every psr-0 one) only
		// blocks: it competes in the longest-match rule but never keys.
		index.unsegmented = append(index.unsegmented, lockPrefix{prefix: prefix})
	}
	for _, locked := range append(document.Packages, document.PackagesDev...) {
		name, _ := locked.Name.(string)
		if !ValidPackageName(name) {
			name = ""
		}
		for rawPrefix := range locked.Autoload.PSR4 {
			add(normalizePrefix(rawPrefix), name)
		}
		for rawPrefix := range locked.Autoload.PSR0 {
			add(normalizePrefix(rawPrefix), "")
		}
	}
	return index
}

// size reports how many prefixes the table holds.
func (index lockIndex) size() int {
	count := len(index.unsegmented)
	for _, names := range index.bySegmentPrefix {
		count += len(names)
	}
	return count
}

// lookup returns the package whose prefix is the longest one covering fqcn,
// and whether any prefix covers it at all. A tie between two packages, or a
// longest prefix that yields no key, returns ("", true): the class is covered
// but cannot be keyed, so the caller must stop rather than ask another lock.
func (index lockIndex) lookup(fqcn string) (string, bool) {
	name, _, covered := index.lookupPrefix(fqcn)
	return name, covered
}

// lookupPrefix is lookup that also returns the length of the winning prefix,
// which the own-namespace rule compares against.
func (index lockIndex) lookupPrefix(fqcn string) (string, int, bool) {
	best := -1
	var names []string
	consider := func(prefix string, candidates ...string) {
		if !coversClass(prefix, fqcn) || len(prefix) < best {
			return
		}
		if len(prefix) > best {
			best, names = len(prefix), nil
		}
		names = append(names, candidates...)
	}
	for cut := strings.LastIndex(fqcn, `\`); cut >= 0; cut = strings.LastIndex(fqcn[:cut], `\`) {
		prefix := fqcn[:cut+1]
		if candidates, ok := index.bySegmentPrefix[prefix]; ok {
			consider(prefix, candidates...)
			break
		}
	}
	for _, entry := range index.unsegmented {
		consider(entry.prefix, entry.name)
	}
	if best < 0 {
		return "", 0, false
	}
	for _, name := range names[1:] {
		if name != names[0] {
			return "", best, true
		}
	}
	return names[0], best, true
}

// readLock returns the cached prefix table of the composer.lock at path, and
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

// Locks is the chain of composer.lock files that can provide classes to one
// file: every lock from the file's directory up to the repository root, nearest
// first. Build it once per file with LocksFor and ask it per class.
type Locks struct {
	indexes []lockIndex
	// ownPrefixes are the non-empty namespace prefixes the consumer's own
	// composer.json files (file directory up to the repository root)
	// autoload. The empty fallback is not kept: Composer searches it after
	// every prefixed directory, so it never outranks a locked prefix.
	ownPrefixes []string
}

// LocksFor reads every composer.lock from path's directory up to repoRoot,
// nearest first, through the (path, stat) cache. A path outside repoRoot, or a
// tree with no lock, yields an empty chain.
func LocksFor(repoRoot string, path string) Locks {
	repoRoot = project.CleanPath(repoRoot)
	path = project.CleanPath(path)
	if repoRoot == "" || path == "" {
		return Locks{}
	}
	var locks Locks
	for _, dir := range ancestorDirs(repoRoot, path) {
		if index, ok := readLock(filepath.Join(dir, "composer.lock")); ok && index.size() > 0 {
			locks.indexes = append(locks.indexes, index)
		}
		if own, ok := readManifest(filepath.Join(dir, "composer.json")); ok {
			locks.ownPrefixes = append(locks.ownPrefixes, own.ownPrefixes...)
		}
	}
	return locks
}

// Empty reports whether the chain holds no lock that could key a class.
func (l Locks) Empty() bool {
	return len(l.indexes) == 0
}

// Package returns the composer package that provides the class fqcn, or ""
// when none can be proven. The first lock, nearest first, whose packages or
// packages-dev autoload a prefix covering fqcn decides: the longest covering
// prefix wins, and a tie between packages, a psr-0 prefix, a psr-4 prefix
// without the trailing '\', or a name outside the composer grammar yields "".
//
// The consumer's own composer.json files can claim the class too. Composer
// tries the longest psr-4 prefix first, so an own prefix suppresses the key
// only when it is at least as long as the winning locked prefix: own MyCorp\
// yields to a locked MyCorp\Logging\, but own App\ ties laravel/pint's
// App\ (the namespace of every Laravel application), and binding the
// application's own class to the dev tool would be a false link. The
// comparison treats own psr-0 prefixes the same way, which is stricter than
// Composer (it reads psr-0 after psr-4) and can only drop a key.
func (l Locks) Package(fqcn string) string {
	if fqcn == "" {
		return ""
	}
	for _, index := range l.indexes {
		name, prefixLength, covered := index.lookupPrefix(fqcn)
		if !covered {
			continue
		}
		if name == "" || l.longestOwnPrefix(fqcn) >= prefixLength {
			return ""
		}
		return name
	}
	return ""
}

// longestOwnPrefix returns the length of the longest own prefix covering
// fqcn, or -1 when none does.
func (l Locks) longestOwnPrefix(fqcn string) int {
	longest := -1
	for _, prefix := range l.ownPrefixes {
		if len(prefix) > longest && coversClass(prefix, fqcn) {
			longest = len(prefix)
		}
	}
	return longest
}
