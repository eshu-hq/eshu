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

// psr4Mapping is one autoload psr-4 entry of a composer.json: a namespace
// prefix ending in '\' and the directories, relative to the manifest, that
// hold its classes.
type psr4Mapping struct {
	prefix string
	dirs   []string
}

// manifest is the part of a composer.json the key rules read. name is empty
// when the document has no name in the composer grammar. psr4 holds the
// producer mappings. ownPrefixes holds every namespace prefix the manifest
// autoloads from its own tree (psr-4 and psr-0, autoload and autoload-dev,
// non-empty keys only) for the consumer rule.
type manifest struct {
	name        string
	psr4        []psr4Mapping
	ownPrefixes []string
}

// manifestCache memoizes parsed composer.json files by (path, stat), the same
// bounded process-global cache the JavaScript adapter uses for package.json.
var manifestCache = project.NewScopeCache[manifest]()

// ManifestName returns the "name" of a composer.json document when it is a
// string in the composer package-name grammar, and "" otherwise (invalid JSON,
// a non-object document, a missing, non-string, or malformed name). It is the
// one name rule both the parser's package_id stamp and the definition loader's
// producer anchor apply, so the two cannot disagree.
func ManifestName(content []byte) string {
	var document struct {
		Name any `json:"name"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return ""
	}
	name, _ := document.Name.(string)
	if !ValidPackageName(name) {
		return ""
	}
	return name
}

// parseManifest reads the name, the producer psr-4 map, and the own prefixes
// of a composer.json document. Producer mappings come from autoload.psr-4
// only: autoload-dev is loaded for the root package alone, so no consumer can
// reach those classes, and classmap, files, and psr-0 do not prove which file
// holds a class from its name. An empty prefix (the psr-4 fallback for every
// namespace) and a prefix without the trailing '\' composer requires produce
// no mapping. The own prefixes take every autoload and autoload-dev psr-4 and
// psr-0 key, because a class under any of them can live in the manifest's own
// tree.
func parseManifest(content []byte) manifest {
	type autoload struct {
		PSR4 map[string]any `json:"psr-4"`
		PSR0 map[string]any `json:"psr-0"`
	}
	var document struct {
		Autoload    autoload `json:"autoload"`
		AutoloadDev autoload `json:"autoload-dev"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		return manifest{}
	}
	parsed := manifest{name: ManifestName(content)}
	for _, section := range []autoload{document.Autoload, document.AutoloadDev} {
		for _, keys := range []map[string]any{section.PSR4, section.PSR0} {
			for rawPrefix := range keys {
				if prefix := normalizePrefix(rawPrefix); prefix != "" {
					parsed.ownPrefixes = append(parsed.ownPrefixes, prefix)
				}
			}
		}
	}
	if parsed.name == "" {
		return parsed
	}
	for rawPrefix, value := range document.Autoload.PSR4 {
		prefix := normalizePrefix(rawPrefix)
		if prefix == "" || !strings.HasSuffix(prefix, `\`) {
			continue
		}
		if dirs := autoloadDirs(value); len(dirs) > 0 {
			parsed.psr4 = append(parsed.psr4, psr4Mapping{prefix: prefix, dirs: dirs})
		}
	}
	return parsed
}

// readManifest returns the cached parse of the composer.json at path, and
// false when no file exists there. An unreadable file parses as an unnamed
// manifest, which still ends the nearest-manifest walk: it fails closed to no
// key instead of letting an outer manifest claim the file.
func readManifest(path string) (manifest, bool) {
	stat, ok := project.StatFor(path)
	if !ok {
		return manifest{}, false
	}
	return manifestCache.Get(path, stat, func() manifest {
		content, err := os.ReadFile(path) // #nosec G304 -- reads an indexed repository composer.json at a path found by the nearest-file walk under the scan target
		if err != nil {
			return manifest{}
		}
		return parseManifest(content)
	}), true
}

// ProducerPackage returns the composer package that publishes the class or
// interface fqcn declared in the file at path, or "" when composer could not
// autoload it for a consumer. The nearest composer.json between the file and
// repoRoot decides; it must carry a name in the composer grammar, and one of its
// autoload psr-4 prefixes must cover fqcn with the file sitting exactly at the
// mapped directory plus the remaining namespace segments plus ".php". A file
// under a vendor/ directory is a vendored copy, never a publisher, and a
// manifest under a test, tests, fixtures, testdata, or examples directory
// (FixtureDirPattern) is test scaffolding: the walk stops there with no key
// instead of falling through to an outer manifest (#8223). The path
// comparison is case-sensitive: PHP class names are case-insensitive, so a
// class whose declared case differs from its file name is a miss, never a false
// link.
func ProducerPackage(repoRoot string, path string, fqcn string) string {
	repoRoot = project.CleanPath(repoRoot)
	path = project.CleanPath(path)
	if repoRoot == "" || path == "" || fqcn == "" || underVendor(repoRoot, path) {
		return ""
	}
	for _, dir := range ancestorDirs(repoRoot, path) {
		parsed, ok := readManifest(filepath.Join(dir, "composer.json"))
		if !ok {
			continue
		}
		if parsed.name == "" || manifestInFixtureDir(repoRoot, dir) {
			return ""
		}
		for _, mapping := range parsed.psr4 {
			if !coversClass(mapping.prefix, fqcn) {
				continue
			}
			remainder := filepath.FromSlash(strings.ReplaceAll(fqcn[len(mapping.prefix):], `\`, "/")) + ".php"
			for _, mapped := range mapping.dirs {
				if filepath.Join(dir, filepath.FromSlash(mapped), remainder) == path {
					return parsed.name
				}
			}
		}
		return ""
	}
	return ""
}

// ancestorDirs returns path's directory and each parent up to and including
// repoRoot, nearest first. A path outside repoRoot yields nothing.
func ancestorDirs(repoRoot string, path string) []string {
	if !project.PathWithin(repoRoot, path) {
		return nil
	}
	var dirs []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == repoRoot {
			return dirs
		}
		parent := filepath.Dir(dir)
		if parent == dir || !project.PathWithin(repoRoot, parent) {
			return dirs
		}
	}
}

// underVendor reports whether path sits under a directory named exactly
// vendor below repoRoot, where composer installs dependencies.
func underVendor(repoRoot string, path string) bool {
	relative, ok := project.RelativeSlashPath(repoRoot, filepath.Dir(path))
	if !ok {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "vendor" {
			return true
		}
	}
	return false
}

// manifestInFixtureDir reports whether the composer.json in dir sits under a
// fixture-style directory relative to repoRoot, the same predicate the
// producer anchor applies to a stored manifest path.
func manifestInFixtureDir(repoRoot string, dir string) bool {
	relative, ok := project.RelativeSlashPath(repoRoot, filepath.Join(dir, "composer.json"))
	return ok && InFixtureDir(relative)
}
