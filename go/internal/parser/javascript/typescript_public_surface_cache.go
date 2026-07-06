// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// typeScriptPublicSurfaceNodeFacts holds the extracted, target-independent
// facts the public-surface reexport/type-reference BFS walks need from one
// source file node in the reexport graph. Only plain data is retained (no
// tree-sitter tree or node), so the cache stays memory-bounded regardless of
// how many packages a long-running scan visits (issue #4765).
type typeScriptPublicSurfaceNodeFacts struct {
	// reexports lists every static re-export edge this file emits (named,
	// star, and imported-export-clause forms). Identical for every caller
	// regardless of which target file or exported-name set is being resolved.
	reexports []javaScriptTypeScriptSurfaceReexport
	// namedImportsByLocalName maps each named-import local name to its
	// imported name and module source, reused by the imported-type-reference
	// walk to resolve a public declaration's mentioned identifiers back to a
	// source module.
	namedImportsByLocalName map[string]javaScriptTypeScriptImportedBinding
	// declarationMentions maps each public-kind exported declaration's name to
	// the set of imported local names it mentions anywhere in its body. This
	// is independent of which target file or exported-name set a given walk
	// item is resolving, so it is computed once per file and filtered
	// in-memory per caller instead of re-walking the AST per (file, target)
	// pair.
	declarationMentions map[string]map[string]struct{}
	// ok is false when the file was missing, empty, unparsable, or of an
	// unsupported extension. A false entry is still cached so a dangling
	// re-export target is not re-attempted on every lookup.
	ok bool
}

// packageSurfaceCache memoizes per-file public-surface facts for the
// TypeScript dead-code reexport/type-reference BFS walks, keyed by
// (package root, file path) so the identical closure is computed once per
// package and reused across every file in that package instead of being
// re-walked (and every node re-parsed) once per file (issue #4765). It reuses
// configScopeCache's generation-safe single-flight-coalesced bounded-LRU
// implementation (see config_scope_cache.go) instead of hand-rolling a second
// unbounded cache: entries are invalidated by the file's own (mtime, size)
// generation, and the shared configScopeCacheCapacity bound keeps a
// long-running scan across many repositories from growing this cache
// forever.
var packageSurfaceCache = newConfigScopeCache[typeScriptPublicSurfaceNodeFacts]()

// SetPackageSurfaceComputeHookForTest installs a process-global hook invoked
// on every real sibling-file parse performed by the package-root public
// surface cache (a cache miss, not a cache hit). It returns a restore
// function that must be deferred; the restore also clears the cache so a
// later test starts from a clean state instead of reusing entries a prior
// test's temp-dir paths happened to populate. Test-only: callers MUST NOT run
// this test in parallel with any other test that also installs this hook or
// exercises the package surface cache (mirrors the constraint documented on
// SetConfigScopeComputeHooksForTest).
func SetPackageSurfaceComputeHookForTest(hook func(path string)) func() {
	restore := packageSurfaceCache.setComputeHookForTest(func(key string) {
		if hook != nil {
			hook(packageSurfaceHookPathFromKey(key))
		}
	})
	return func() {
		restore()
		packageSurfaceCache.clearForTest()
	}
}

// packageSurfaceHookPathFromKey extracts the file path portion of the
// internal cache key string the hook observes (see packageSurfaceCacheEncode).
func packageSurfaceHookPathFromKey(key string) string {
	if idx := strings.LastIndex(key, "\x00"); idx >= 0 {
		return key[idx+1:]
	}
	return key
}

// packageSurfaceCacheEncode joins (packageRoot, path) into the single string
// key configScopeCache's compute hook reports, using a NUL separator since it
// cannot legally appear in a filesystem path.
func packageSurfaceCacheEncode(packageRoot string, path string) string {
	return packageRoot + "\x00" + path
}

// packageSurfaceFacts returns the cached (or freshly computed) public-surface
// facts for path within packageRoot's reexport graph, computing them at most
// once per distinct (packageRoot, path, mtime, size) generation. Concurrent
// callers for the SAME key coalesce onto the one in-flight computation;
// callers for a different key (a different package, path, or changed
// generation) get their own slot, per configScopeCache's single-flight
// discipline.
func packageSurfaceFacts(
	packageRoot string,
	path string,
	siblingParser *javaScriptSiblingParser,
) typeScriptPublicSurfaceNodeFacts {
	cleaned := cleanJavaScriptPath(path)
	encodedKey := packageSurfaceCacheEncode(cleanJavaScriptPath(packageRoot), cleaned)
	stat, statOK := statForConfigScopeCache(cleaned)
	if !statOK {
		// Stat failed (missing/unreadable file): fall back to the uncached
		// path so callers see the same not-found behavior as before this
		// cache existed, without polluting the cache with an unstable key.
		return computePackageSurfaceFacts(cleaned, siblingParser)
	}
	return packageSurfaceCache.get(encodedKey, stat, func() typeScriptPublicSurfaceNodeFacts {
		return computePackageSurfaceFacts(cleaned, siblingParser)
	})
}

// computePackageSurfaceFacts performs the actual uncached parse+extract for
// one file: the static re-export edges, the named-import bindings, and, for
// every public-kind exported declaration, the set of imported local names it
// mentions. This is the only place that invokes the sibling parser for the
// public-surface walks.
func computePackageSurfaceFacts(path string, siblingParser *javaScriptSiblingParser) typeScriptPublicSurfaceNodeFacts {
	root, source, ok := siblingParser.rootForFile(path)
	if !ok {
		return typeScriptPublicSurfaceNodeFacts{}
	}
	return typeScriptPublicSurfaceNodeFacts{
		reexports:               javaScriptTypeScriptStaticReexportsFromRoot(root, source),
		namedImportsByLocalName: javaScriptTypeScriptNamedImportsByLocalName(root, source),
		declarationMentions:     javaScriptTypeScriptPublicDeclarationMentionsByName(root, source),
		ok:                      true,
	}
}

// javaScriptTypeScriptPublicDeclarationMentionsByName extracts, for every
// public-kind exported declaration (interface, type alias, class, or enum),
// the set of imported local names mentioned anywhere in its body. This
// mirrors javaScriptTypeScriptImportedTypeReferencesFromPublicDeclarations but
// runs once per file against every named import in the file (not filtered to
// one target's bindings), so the per-(file, target) filtering the BFS needs
// becomes a cheap in-memory map lookup instead of a repeat AST walk.
func javaScriptTypeScriptPublicDeclarationMentionsByName(
	root *tree_sitter.Node,
	source []byte,
) map[string]map[string]struct{} {
	mentions := make(map[string]map[string]struct{})
	if root == nil {
		return mentions
	}
	importsByLocalName := javaScriptTypeScriptNamedImportsByLocalName(root, source)
	if len(importsByLocalName) == 0 {
		return mentions
	}
	parents := buildJavaScriptParentLookup(root)
	walkNamed(root, func(node *tree_sitter.Node) {
		if !javaScriptIsExported(node, parents) {
			return
		}
		if !javaScriptTypeScriptIsPublicDeclarationKind(node) {
			return
		}
		name := javaScriptTypeScriptDeclarationName(node, source)
		if name == "" {
			return
		}
		mentioned := javaScriptTypeScriptDeclarationMentionedNames(node, source, importsByLocalName)
		if len(mentioned) == 0 {
			return
		}
		// Union across duplicate-named declarations (TypeScript declaration
		// merging: two "export interface Widget {...}" blocks in one file
		// each contribute their own mentioned imports). Overwriting here would
		// silently drop the first declaration's mentions when a later
		// same-named declaration mentions a different imported type.
		if mentions[name] == nil {
			mentions[name] = make(map[string]struct{}, len(mentioned))
		}
		for localName := range mentioned {
			mentions[name][localName] = struct{}{}
		}
	})
	return mentions
}
