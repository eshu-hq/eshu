// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/project"
)

// ProducerFunctionPackage returns the composer package that publishes the
// namespaced free function fqn declared in the file at path, or "" when no
// consumer can be shown to load it (#8280). psr-4 never loads a function, so
// the nearest composer.json between the file and repoRoot must list the file
// in autoload.files, which composer requires on every autoload, and one of its
// autoload psr-4 prefixes must cover fqn, so a consumer's composer.lock maps
// the function's namespace to this package. The manifest walk, the composer
// name grammar, the vendor/ rule, and the fixture-directory stop are the ones
// ProducerPackage applies. A file a listed file includes (functions_include.php
// requiring functions.php) is not listed itself and stays a miss.
func ProducerFunctionPackage(repoRoot string, path string, fqn string) string {
	repoRoot = project.CleanPath(repoRoot)
	path = project.CleanPath(path)
	if repoRoot == "" || path == "" || fqn == "" || underVendor(repoRoot, path) {
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
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return ""
		}
		if _, listed := parsed.files[filepath.ToSlash(relative)]; !listed {
			return ""
		}
		for _, mapping := range parsed.psr4 {
			if coversClass(mapping.prefix, fqn) {
				return parsed.name
			}
		}
		return ""
	}
	return ""
}

// autoloadFiles returns the cleaned slash paths of an autoload.files value, an
// array of strings. A non-array value, a non-string entry, and a path that
// leaves the manifest directory list nothing.
func autoloadFiles(value any) map[string]struct{} {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		raw, ok := entry.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(raw))))
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
			continue
		}
		files[cleaned] = struct{}{}
	}
	return files
}
