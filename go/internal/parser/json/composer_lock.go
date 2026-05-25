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
func composerLockDependencyVariables(document map[string]any, lang string) []map[string]any {
	rows := composerLockSectionRows(document, composerLockRuntimeSection, lang, 1)
	rows = append(rows, composerLockSectionRows(document, composerLockDevSection, lang, len(rows)+1)...)
	return rows
}

func composerLockSectionRows(
	document map[string]any,
	section string,
	lang string,
	startLine int,
) []map[string]any {
	raw, ok := document[section].([]any)
	if !ok {
		return nil
	}
	entries := composerLockSortedEntries(raw)
	rows := make([]map[string]any, 0, len(entries))
	lineNumber := startLine
	for _, entry := range entries {
		row := composerLockDependencyRow(entry, section, lang, lineNumber)
		if row == nil {
			continue
		}
		rows = append(rows, row)
		lineNumber++
	}
	return rows
}

func composerLockSortedEntries(raw []any) []map[string]any {
	type indexedEntry struct {
		entry   map[string]any
		sortKey string
	}
	indexed := make([]indexedEntry, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		indexed = append(indexed, indexedEntry{
			entry:   entry,
			sortKey: strings.ToLower(composerLockEntryName(entry)),
		})
	}
	sort.SliceStable(indexed, func(i, j int) bool {
		return indexed[i].sortKey < indexed[j].sortKey
	})
	entries := make([]map[string]any, len(indexed))
	for i, item := range indexed {
		entries[i] = item.entry
	}
	return entries
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
	lineNumber int,
) map[string]any {
	name := composerLockEntryName(entry)
	if name == "" {
		return nil
	}
	version := composerLockEntryVersion(entry)
	if version == "" {
		return nil
	}
	return map[string]any{
		"name":            name,
		"line_number":     lineNumber,
		"value":           version,
		"section":         section,
		"config_kind":     "dependency",
		"package_manager": "composer",
		"lockfile":        true,
		"lang":            lang,
	}
}
