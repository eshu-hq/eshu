// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	stdjson "encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Config carries parent-owned helpers that the JSON adapter needs without
// importing the parent parser package.
type Config struct {
	LineageExtractor LineageExtractor
}

// LineageExtractor resolves compiled dbt SQL lineage for manifest parsing.
type LineageExtractor func(compiledSQL string, modelName string, relationColumnNames map[string][]string) CompiledModelLineage

// ColumnLineage describes one dbt output column and its source columns.
type ColumnLineage struct {
	OutputColumn        string
	SourceColumns       []string
	TransformKind       string
	TransformExpression string
}

// CompiledModelLineage summarizes compiled dbt SQL lineage for one model.
type CompiledModelLineage struct {
	ColumnLineage        []ColumnLineage
	UnresolvedReferences []map[string]string
	ProjectionCount      int
}

// Parse decodes one JSON file and returns the parser payload expected by the
// parent engine.
func Parse(
	path string,
	isDependency bool,
	options shared.Options,
	config Config,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := jsonBasePayload(path, isDependency)
	normalized, translate := normalizeJSONSource(source, filepath.Base(path))
	if strings.TrimSpace(normalized) == "" {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	normalizedBytes := []byte(normalized)
	var document any
	if err := stdjson.Unmarshal(normalizedBytes, &document); err != nil {
		return nil, fmt.Errorf("parse json file %q: %w", path, err)
	}

	object, ok := document.(map[string]any)
	if !ok {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	filename := strings.ToLower(filepath.Base(path))

	// See jsonFilenameNeedsOrderedEntries: only filenames that read
	// topLevelEntries below pay for the full ordered walk; every other file
	// gets json_metadata via the cheaper key-order-only scan (issue #4873).
	// sourceIndex answers offset->line lookups for offsets into
	// normalizedBytes (the exact buffer this ordered walk decodes) but is
	// built over source, the real on-disk bytes, with every query translated
	// back through `translate` first (issue #5358): normalizedBytes has had
	// leading blank lines, a `{{ }}` banner, or (for JSONC filenames)
	// `/* */` comments removed, each of which can drop or shift '\n' bytes
	// relative to source, so indexing normalizedBytes directly would report
	// too-small line numbers for every entity after a stripped region. It
	// stays nil for files that do not need ordered entries.
	var topLevelEntries []orderedJSONEntry
	var sourceIndex *newlineIndex
	if jsonFilenameNeedsOrderedEntries(filename) {
		sourceIndex = buildTranslatedNewlineIndex(source, translate)
		entries, err := unmarshalOrderedJSONObjectAt(normalizedBytes, 0, sourceIndex)
		if err == nil {
			topLevelEntries = entries
			payload["json_metadata"] = map[string]any{"top_level_keys": orderedJSONKeys(entries)}
		}
	} else if keys, err := topLevelJSONKeyOrder(normalizedBytes); err == nil {
		payload["json_metadata"] = map[string]any{"top_level_keys": keys}
	}

	languageName := "json"
	if cloudformation.IsTemplate(object) {
		// Build the position-aware walk ONLY inside the IsTemplate branch so
		// generic JSON files never pay for it. sourceIndex/normalizedBytes MUST
		// be the exact pair the generic ordered-entry path uses above
		// (buildTranslatedNewlineIndex over the real on-disk source, feeding the
		// normalized buffer) -- feeding raw bytes or an untranslated index would
		// bake wrong lines into the CloudFormation entity's canonical identity
		// (issue #5358, #5348).
		sourceIndex := buildTranslatedNewlineIndex(source, translate)
		positions, positionFallbacks := cloudformationPositionsFromDocument(normalizedBytes, sourceIndex, object)
		for _, fallback := range positionFallbacks {
			shared.AppendBucket(payload, "cloudformation_position_fallbacks", map[string]any{
				"path":        path,
				"section":     fallback.Section,
				"reason":      fallback.Reason,
				"line_number": firstPositiveInt(fallback.Line, 1),
			})
		}
		result := cloudformation.ParseWithPositions(object, path, 1, languageName, positions)
		payload["cloudformation_resources"] = result.Resources
		payload["cloudformation_parameters"] = result.Params
		payload["cloudformation_outputs"] = result.Outputs
		payload["cloudformation_conditions"] = result.Conditions
		payload["cloudformation_cross_stack_imports"] = result.Imports
		payload["cloudformation_cross_stack_exports"] = result.Exports
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	if applyJSONReplayDocument(payload, object, filename, config.LineageExtractor) {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	// Lockfile branches below build their own newlineIndex lazily (a cheap
	// O(n) byte scan) rather than reusing sourceIndex: sourceIndex is only
	// built for jsonFilenameNeedsOrderedEntries filenames, and lockfiles
	// deliberately stay off that list (issue #4873) because their dependency
	// rows are re-sorted for deterministic output, not walked in JSON source
	// order the way package.json/composer.json/tsconfig* are.
	switch {
	case filename == "packages.lock.json":
		payload["variables"] = nugetPackagesLockDependencyVariables(object, normalizedBytes, languageName)
	case filename == "package-lock.json":
		payload["variables"] = packageLockDependencyVariables(object, normalizedBytes, languageName)
	case filename == "composer.lock":
		payload["variables"] = composerLockDependencyVariables(object, normalizedBytes, languageName)
	case filename == "pipfile.lock":
		payload["variables"] = pipfileLockDependencyVariables(object, normalizedBytes)
	case filename == "package.resolved":
		payload["variables"] = swiftPackageResolvedDependencyVariables(object, normalizedBytes, languageName)
	case !shouldSkipJSONEntities(filename):
		switch {
		case filename == "package.json":
			payload["variables"] = packageJSONDependencyVariables(object, languageName, topLevelEntries, sourceIndex)
			payload["functions"] = jsonScriptFunctions(object, languageName, topLevelEntries, sourceIndex)
		case filename == "composer.json":
			payload["variables"] = composerManifestDependencyVariables(object, languageName, topLevelEntries, sourceIndex)
		case isTypeScriptConfigFilename(filename):
			payload["variables"] = tsconfigVariables(object, languageName, topLevelEntries, sourceIndex)
		}
	}

	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

func packageJSONDependencyVariables(
	document map[string]any,
	lang string,
	topLevelEntries []orderedJSONEntry,
	idx *newlineIndex,
) []map[string]any {
	sections := []struct {
		name  string
		scope string
		dev   bool
	}{
		{name: "dependencies", scope: "runtime"},
		{name: "devDependencies", scope: "dev", dev: true},
		{name: "optionalDependencies", scope: "optional"},
		{name: "peerDependencies", scope: "peer"},
	}
	rows := make([]map[string]any, 0)
	for _, section := range sections {
		sectionRows := dependencyVariablesWithScope(
			document,
			lang,
			section.name,
			"npm",
			topLevelEntries,
			idx,
			section.scope,
			section.dev,
		)
		rows = append(rows, sectionRows...)
	}
	return rows
}

func jsonBasePayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "json", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	payload["cloudformation_resources"] = []map[string]any{}
	payload["cloudformation_parameters"] = []map[string]any{}
	payload["cloudformation_outputs"] = []map[string]any{}
	payload["cloudformation_conditions"] = []map[string]any{}
	payload["cloudformation_cross_stack_imports"] = []map[string]any{}
	payload["cloudformation_cross_stack_exports"] = []map[string]any{}
	payload["analytics_models"] = []map[string]any{}
	payload["data_assets"] = []map[string]any{}
	payload["data_columns"] = []map[string]any{}
	payload["query_executions"] = []map[string]any{}
	payload["dashboard_assets"] = []map[string]any{}
	payload["data_quality_checks"] = []map[string]any{}
	payload["data_owners"] = []map[string]any{}
	payload["data_contracts"] = []map[string]any{}
	payload["data_relationships"] = []map[string]any{}
	payload["data_governance_annotations"] = []map[string]any{}
	payload["data_intelligence_coverage"] = map[string]any{
		"confidence":            0.0,
		"state":                 "unavailable",
		"unresolved_references": []string{},
	}
	payload["json_metadata"] = map[string]any{"top_level_keys": []string{}}
	return payload
}

// normalizeJSONSource, identityOffsetTranslator, and mapOffset live in
// source_normalize.go (issue #5358: normalizeJSONSource grew an
// offsetTranslator return value to keep line_number honest, which pushed
// this file past the 500-line cap).

// shouldSkipJSONEntities keeps package-lock.json and composer.lock listed
// so the generic dependency and tsconfig branches do not
// attempt to parse them; both files are routed to their own lockfile-aware
// emitter via the switch in Parse before this guard is consulted.
func shouldSkipJSONEntities(filename string) bool {
	switch filename {
	case "package-lock.json", "composer.lock":
		return true
	}
	return strings.HasSuffix(filename, ".min.json")
}

func isTypeScriptConfigFilename(filename string) bool {
	lower := strings.ToLower(filename)
	return isLowerTypeScriptConfigFilename(lower)
}

func isLowerTypeScriptConfigFilename(lower string) bool {
	return strings.HasPrefix(lower, "tsconfig") && strings.HasSuffix(lower, ".json")
}

func isJSONCConfigFilename(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".jsonc") || isLowerTypeScriptConfigFilename(lower)
}

// dependencyVariablesWithScope emits one row per key in document[section].
// Row order follows the JSON source (via orderedJSONSectionKeys); line_number
// is the section entry's real source line from idx/topLevelEntries when
// available (issue #5329) and is omitted — never fabricated — when the
// ordered walk could not be run for this file (topLevelEntries empty or idx
// nil), which forces the sortedMapKeys(raw) fallback inside
// orderedJSONSectionKeys.
func dependencyVariablesWithScope(
	document map[string]any,
	lang string,
	section string,
	packageManager string,
	topLevelEntries []orderedJSONEntry,
	idx *newlineIndex,
	dependencyScope string,
	developmentDependency bool,
) []map[string]any {
	raw, ok := document[section].(map[string]any)
	if !ok {
		return []map[string]any{}
	}

	lines := orderedJSONSectionLines(topLevelEntries, section, idx)
	rows := make([]map[string]any, 0, len(raw))
	for _, name := range orderedJSONSectionKeys(topLevelEntries, section, raw) {
		row := map[string]any{
			"name":            name,
			"value":           fmt.Sprint(raw[name]),
			"section":         section,
			"config_kind":     "dependency",
			"package_manager": packageManager,
			"lang":            lang,
		}
		if line, ok := lines[name]; ok {
			row["line_number"] = line
		}
		if dependencyScope != "" {
			row["dependency_scope"] = dependencyScope
			row["development_dependency"] = developmentDependency
		}
		rows = append(rows, row)
	}
	return rows
}

// jsonScriptFunctions emits one row per "scripts" entry. line_number/end_line
// carry the script key's real source line (both set to the same value: an
// npm script is a one-line shell command string, not a multi-statement
// function body) and are omitted together when no ordered walk data exists
// for this file.
func jsonScriptFunctions(document map[string]any, lang string, topLevelEntries []orderedJSONEntry, idx *newlineIndex) []map[string]any {
	raw, ok := document["scripts"].(map[string]any)
	if !ok {
		return []map[string]any{}
	}

	lines := orderedJSONSectionLines(topLevelEntries, "scripts", idx)
	rows := make([]map[string]any, 0, len(raw))
	for _, name := range orderedJSONSectionKeys(topLevelEntries, "scripts", raw) {
		row := map[string]any{
			"name": name,
			"args": []string{},
			// npm scripts are shell command strings, not parsed source functions,
			// so there is no statement AST to measure. Emit 0 (unknown) instead of
			// a fabricated 1 so complexity rankings exclude them via the existing
			// coalesce(..., 0) > 0 filter rather than surface a misleading value.
			// See issue #3488.
			"cyclomatic_complexity": 0,
			"source":                fmt.Sprint(raw[name]),
			"function_kind":         "json_script",
			"context":               "scripts",
			"context_type":          "json",
			"lang":                  lang,
		}
		if line, ok := lines[name]; ok {
			row["line_number"] = line
			row["end_line"] = line
		}
		rows = append(rows, row)
	}
	return rows
}

// tsconfigVariables emits rows for "extends", "references", and
// "compilerOptions.paths". Each row's line_number is that entry's own real
// source line (issue #5329) — a top-level entry lookup for "extends", a
// per-array-element walk of the "references" array's raw bytes for
// reference rows, and a nested-object lookup under "compilerOptions" for
// path rows — never a shared incrementing counter across the three
// sections. line_number is omitted when no ordered walk data is available.
func tsconfigVariables(document map[string]any, lang string, topLevelEntries []orderedJSONEntry, idx *newlineIndex) []map[string]any {
	rows := make([]map[string]any, 0)

	if extendsValue, ok := document["extends"].(string); ok {
		row := map[string]any{
			"name":        "extends",
			"value":       extendsValue,
			"section":     "extends",
			"config_kind": "extends",
			"lang":        lang,
		}
		if line, ok := orderedJSONEntryLine(topLevelEntries, "extends"); ok {
			row["line_number"] = line
		}
		rows = append(rows, row)
	}

	if references, ok := document["references"].([]any); ok {
		referenceLines := tsconfigReferenceLines(topLevelEntries, idx)
		for i, item := range references {
			reference, ok := item.(map[string]any)
			if !ok {
				continue
			}
			referencePath, _ := reference["path"].(string)
			if strings.TrimSpace(referencePath) == "" {
				continue
			}
			row := map[string]any{
				"name":        "reference:" + referencePath,
				"value":       referencePath,
				"section":     "references",
				"config_kind": "reference",
				"lang":        lang,
			}
			if i < len(referenceLines) && referenceLines[i] > 0 {
				row["line_number"] = referenceLines[i]
			}
			rows = append(rows, row)
		}
	}

	compilerOptions, ok := document["compilerOptions"].(map[string]any)
	if !ok {
		return rows
	}
	paths, ok := compilerOptions["paths"].(map[string]any)
	if !ok {
		return rows
	}
	compilerOptionsEntries, ok := orderedJSONSectionEntries(topLevelEntries, "compilerOptions", idx)
	if !ok {
		compilerOptionsEntries = nil
	}
	pathsLines := orderedJSONSectionLines(compilerOptionsEntries, "paths", idx)
	for _, alias := range orderedJSONSectionKeys(compilerOptionsEntries, "paths", paths) {
		row := map[string]any{
			"name":        "path:" + alias,
			"value":       normalizeJSONArrayValue(paths[alias]),
			"section":     "compilerOptions.paths",
			"config_kind": "path",
			"lang":        lang,
		}
		if line, ok := pathsLines[alias]; ok {
			row["line_number"] = line
		}
		rows = append(rows, row)
	}
	return rows
}

// tsconfigReferenceLines returns the real source line of each element of the
// top-level "references" array, in array order, or nil when idx is nil or
// the array's raw bytes are unavailable (topLevelEntries empty).
func tsconfigReferenceLines(topLevelEntries []orderedJSONEntry, idx *newlineIndex) []int {
	if idx == nil {
		return nil
	}
	raw, start, ok := orderedJSONEntryRaw(topLevelEntries, "references")
	if !ok {
		return nil
	}
	lines, err := unmarshalOrderedJSONArrayLines(raw, start, idx)
	if err != nil {
		return nil
	}
	return lines
}

func orderedJSONSectionKeys(entries []orderedJSONEntry, key string, fallback map[string]any) []string {
	if len(entries) > 0 {
		nested, ok, err := orderedJSONNestedObject(entries, key)
		if err == nil && ok {
			return orderedJSONKeys(nested)
		}
	}
	return sortedMapKeys(fallback)
}

func normalizeJSONArrayValue(value any) string {
	items, ok := value.([]any)
	if !ok {
		return fmt.Sprint(value)
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprint(item))
	}
	return strings.Join(parts, ",")
}
