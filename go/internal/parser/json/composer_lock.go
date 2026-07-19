// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"fmt"
	"sort"
	"strings"
)

// composerLockRuntimeSection names the array in composer.lock that lists
// resolved runtime (non-dev) packages. Composer guarantees this section is
// populated whenever `composer install` succeeds against a manifest that
// declares any `require` entries.
const composerLockRuntimeSection = "packages"

// composerLockDevSection names the array in composer.lock that lists
// resolved development packages. The supply chain reducer uses this section
// to bound impact to non-production code paths.
const composerLockDevSection = "packages-dev"

// composerLockDependencyVariables converts a composer.lock document into
// the parser's normalized dependency rows. Each package becomes one row
// with the exact installed version, the `packages` or `packages-dev`
// scope, and a `lockfile: true` flag so downstream code can distinguish
// resolver-locked truth from manifest-declared ranges.
// composerLockDependencyVariables converts one composer.lock document into
// dependency rows for the "packages" (runtime) and "packages-dev" (dev)
// sections.
func composerLockDependencyVariables(document map[string]any, source []byte, lang string) []map[string]any {
	rows := composerLockSectionRows(document, source, composerLockRuntimeSection, lang)
	rows = append(rows, composerLockSectionRows(document, source, composerLockDevSection, lang)...)
	return rows
}

// composerLockSectionRows converts one composer.lock array section into
// rows. line_number is each package's real source line within that section's
// JSON array (issue #5329): "packages"/"packages-dev" are arrays, not
// keyed objects, so lines are captured by original array position
// (lockfileArrayElementLines) and re-associated with each row after
// composerLockSortedEntries reorders rows alphabetically for deterministic
// output. line_number is omitted when source is unavailable or the line
// lookup fails.
func composerLockSectionRows(
	document map[string]any,
	source []byte,
	section string,
	lang string,
) []map[string]any {
	raw, ok := document[section].([]any)
	if !ok {
		return nil
	}
	entries, originalIndexes := composerLockSortedEntries(raw)
	chains := composerLockDependencyChains(entries)
	elementLines := lockfileArrayElementLines(source, section)
	rows := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		row := composerLockDependencyRow(entry, section, lang, chains)
		if row == nil {
			continue
		}
		if originalIndex := originalIndexes[i]; originalIndex < len(elementLines) {
			if line := elementLines[originalIndex]; line > 0 {
				row["line_number"] = line
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// composerLockSortedEntries sorts raw's object entries alphabetically by
// package name for deterministic row output, and returns each sorted
// entry's index in the original (JSON source) array order alongside it, so
// callers can look up that entry's real source line by original position
// after sorting.
func composerLockSortedEntries(raw []any) ([]map[string]any, []int) {
	type indexedEntry struct {
		entry         map[string]any
		sortKey       string
		originalIndex int
	}
	indexed := make([]indexedEntry, 0, len(raw))
	for originalIndex, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		indexed = append(indexed, indexedEntry{
			entry:         entry,
			sortKey:       strings.ToLower(composerLockEntryName(entry)),
			originalIndex: originalIndex,
		})
	}
	sort.SliceStable(indexed, func(i, j int) bool {
		return indexed[i].sortKey < indexed[j].sortKey
	})
	entries := make([]map[string]any, len(indexed))
	originalIndexes := make([]int, len(indexed))
	for i, item := range indexed {
		entries[i] = item.entry
		originalIndexes[i] = item.originalIndex
	}
	return entries, originalIndexes
}

func composerLockEntryName(entry map[string]any) string {
	raw, ok := entry["name"]
	if !ok {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "<nil>" {
		return ""
	}
	return value
}

func composerLockEntryVersion(entry map[string]any) string {
	raw, ok := entry["version"]
	if !ok {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "<nil>" {
		return ""
	}
	return value
}

func composerLockDependencyRow(
	entry map[string]any,
	section string,
	lang string,
	chains map[string]composerLockDependencyChain,
) map[string]any {
	name := composerLockEntryName(entry)
	if name == "" {
		return nil
	}
	version := composerLockEntryVersion(entry)
	if version == "" {
		return nil
	}
	row := map[string]any{
		"name":            name,
		"value":           version,
		"section":         section,
		"config_kind":     "dependency",
		"package_manager": "composer",
		"lockfile":        true,
		"lang":            lang,
	}
	if section == composerLockDevSection {
		row["dependency_scope"] = "dev"
		row["development_dependency"] = true
	} else {
		row["dependency_scope"] = "runtime"
	}
	if chain, ok := chains[strings.ToLower(name)]; ok {
		row["dependency_path"] = chain.path
		row["dependency_depth"] = len(chain.path)
		row["direct_dependency"] = len(chain.path) == 1
	}
	return row
}

type composerLockDependencyChain struct {
	path []string
}

func composerLockDependencyChains(entries []map[string]any) map[string]composerLockDependencyChain {
	byName := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := composerLockEntryName(entry)
		if name == "" {
			continue
		}
		byName[strings.ToLower(name)] = name
	}
	childrenByParent := make(map[string][]string)
	requiredBy := make(map[string][]string)
	for _, entry := range entries {
		parent := strings.ToLower(composerLockEntryName(entry))
		if parent == "" {
			continue
		}
		for _, child := range composerLockEntryRequireNames(entry, byName) {
			childrenByParent[parent] = append(childrenByParent[parent], child)
			requiredBy[child] = append(requiredBy[child], parent)
		}
	}
	roots := composerLockRootNames(byName, requiredBy)
	return composerLockShortestChains(roots, byName, childrenByParent)
}

func composerLockEntryRequireNames(entry map[string]any, packageNames map[string]string) []string {
	raw, ok := entry["require"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if _, ok := packageNames[normalized]; ok {
			names = append(names, normalized)
		}
	}
	sort.Strings(names)
	return names
}

func composerLockRootNames(
	packageNames map[string]string,
	requiredBy map[string][]string,
) []string {
	roots := make([]string, 0, len(packageNames))
	for name := range packageNames {
		if len(requiredBy[name]) == 0 {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	return roots
}

func composerLockShortestChains(
	roots []string,
	packageNames map[string]string,
	childrenByParent map[string][]string,
) map[string]composerLockDependencyChain {
	type queueItem struct {
		name string
		path []string
	}
	chains := make(map[string]composerLockDependencyChain, len(packageNames))
	queue := make([]queueItem, 0, len(roots))
	for _, root := range roots {
		queue = append(queue, queueItem{name: root, path: []string{packageNames[root]}})
	}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if existing, ok := chains[item.name]; ok && len(existing.path) <= len(item.path) {
			continue
		}
		chains[item.name] = composerLockDependencyChain{path: append([]string(nil), item.path...)}
		children := append([]string(nil), childrenByParent[item.name]...)
		sort.Strings(children)
		for _, child := range children {
			queue = append(queue, queueItem{
				name: child,
				path: append(append([]string(nil), item.path...), packageNames[child]),
			})
		}
	}
	return chains
}
