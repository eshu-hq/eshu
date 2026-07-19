// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

// lockfileSectionLines returns a name->line map for the direct children of
// the top-level object key topKey inside source, or nil when topKey is
// absent, not an object, or source is empty. It is the shared real-line
// lookup for lockfile producers (package-lock.json, packages.lock.json,
// Pipfile.lock) whose dependency rows are indexed by object key rather than
// array position (issue #5329): each producer re-sorts its rows for
// deterministic output, so lines are always resolved by name, never by scan
// position.
//
// This intentionally mirrors the #4873 lockfile-performance rule: it never
// runs the RawMessage-copying unmarshalOrderedJSONObject walk over the whole
// document, only jsonObjectExtractKey (a single targeted RawMessage capture
// for topKey) followed by jsonObjectKeyLines (a value-skipping flat scan),
// so a multi-megabyte package-lock.json still pays for one bounded pass.
func lockfileSectionLines(source []byte, topKey string) map[string]int {
	if len(source) == 0 {
		return nil
	}
	idx := buildNewlineIndex(source)
	raw, start, found, err := jsonObjectExtractKey(source, topKey, 0)
	if err != nil || !found {
		return nil
	}
	lines, err := jsonObjectKeyLines(raw, start, idx)
	if err != nil {
		return nil
	}
	return lines
}

// lockfileNestedSectionLines is lockfileSectionLines for a value nested two
// object levels deep: source[topKey][nestedKey] must be an object whose keys
// get real lines. packages.lock.json (NuGet) needs this: "dependencies" keys
// by target framework before keying by package name.
func lockfileNestedSectionLines(source []byte, topKey string, nestedKey string) map[string]int {
	if len(source) == 0 {
		return nil
	}
	idx := buildNewlineIndex(source)
	topRaw, topStart, found, err := jsonObjectExtractKey(source, topKey, 0)
	if err != nil || !found {
		return nil
	}
	nestedRaw, nestedStart, found, err := jsonObjectExtractKey(topRaw, nestedKey, topStart)
	if err != nil || !found {
		return nil
	}
	lines, err := jsonObjectKeyLines(nestedRaw, nestedStart, idx)
	if err != nil {
		return nil
	}
	return lines
}

// lockfileArrayElementLines returns the real source line of each element of
// the array at source[topKey], in array order, or nil when unavailable.
// composer.lock ("packages"/"packages-dev") and Package.resolved ("pins")
// need this: their rows come from JSON arrays rather than keyed objects, so
// a name->line map cannot express the lookup — callers must correlate by the
// original array index instead (see composerLockSortedEntries's originalIndex
// field and swiftPackageResolvedDependencyVariables's loop index).
func lockfileArrayElementLines(source []byte, topKey string) []int {
	if len(source) == 0 {
		return nil
	}
	idx := buildNewlineIndex(source)
	raw, start, found, err := jsonObjectExtractKey(source, topKey, 0)
	if err != nil || !found {
		return nil
	}
	lines, err := unmarshalOrderedJSONArrayLines(raw, start, idx)
	if err != nil {
		return nil
	}
	return lines
}
