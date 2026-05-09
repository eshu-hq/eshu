package json

import (
	"bytes"
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
	normalized := normalizeJSONSource(source, filepath.Base(path))
	if strings.TrimSpace(normalized) == "" {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	var document any
	if err := stdjson.Unmarshal([]byte(normalized), &document); err != nil {
		return nil, fmt.Errorf("parse json file %q: %w", path, err)
	}

	object, ok := document.(map[string]any)
	if !ok {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	topLevelEntries, err := unmarshalOrderedJSONObject([]byte(normalized))
	if err == nil {
		payload["json_metadata"] = map[string]any{"top_level_keys": orderedJSONKeys(topLevelEntries)}
	}

	languageName := "json"
	if cloudformation.IsTemplate(object) {
		result := cloudformation.Parse(object, path, 1, languageName)
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

	filename := strings.ToLower(filepath.Base(path))
	if applyJSONReplayDocument(payload, object, filename, config.LineageExtractor) {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	if !shouldSkipJSONEntities(filename) {
		switch {
		case filename == "package.json":
			payload["variables"] = dependencyVariables(object, languageName, "dependencies", "npm", topLevelEntries)
			devVariables := dependencyVariables(object, languageName, "devDependencies", "npm", topLevelEntries)
			payload["variables"] = append(payload["variables"].([]map[string]any), devVariables...)
			payload["functions"] = jsonScriptFunctions(object, languageName, topLevelEntries)
		case filename == "composer.json":
			payload["variables"] = dependencyVariables(object, languageName, "require", "composer", topLevelEntries)
			devVariables := dependencyVariables(object, languageName, "require-dev", "composer", topLevelEntries)
			payload["variables"] = append(payload["variables"].([]map[string]any), devVariables...)
		case isTypeScriptConfigFilename(filename):
			payload["variables"] = tsconfigVariables(object, languageName, topLevelEntries)
		}
	}

	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
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

func normalizeJSONSource(source []byte, filename string) string {
	trimmed := strings.TrimLeft(string(bytes.TrimPrefix(source, []byte("\xef\xbb\xbf"))), "\ufeff")
	if strings.TrimSpace(trimmed) == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	start := 0
	for start < len(lines) {
		candidate := strings.TrimSpace(lines[start])
		if strings.HasPrefix(candidate, "{{") && strings.HasSuffix(candidate, "}}") {
			start++
			continue
		}
		break
	}

	normalized := strings.TrimLeft(strings.Join(lines[start:], "\n"), " \t\r\n")
	if isTypeScriptConfigFilename(filename) {
		normalized = stripJSONCComments(normalized)
		normalized = stripTrailingCommas(normalized)
	}
	return normalized
}

func shouldSkipJSONEntities(filename string) bool {
	switch filename {
	case "package-lock.json", "composer.lock":
		return true
	}
	return strings.HasSuffix(filename, ".min.json")
}

func isTypeScriptConfigFilename(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasPrefix(lower, "tsconfig") && strings.HasSuffix(lower, ".json")
}

func dependencyVariables(
	document map[string]any,
	lang string,
	section string,
	packageManager string,
	topLevelEntries []orderedJSONEntry,
) []map[string]any {
	raw, ok := document[section].(map[string]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(raw))
	lineNumber := 1
	for _, name := range orderedJSONSectionKeys(topLevelEntries, section, raw) {
		rows = append(rows, map[string]any{
			"name":            name,
			"line_number":     lineNumber,
			"value":           fmt.Sprint(raw[name]),
			"section":         section,
			"config_kind":     "dependency",
			"package_manager": packageManager,
			"lang":            lang,
		})
		lineNumber++
	}
	return rows
}

func jsonScriptFunctions(document map[string]any, lang string, topLevelEntries []orderedJSONEntry) []map[string]any {
	raw, ok := document["scripts"].(map[string]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(raw))
	lineNumber := 1
	for _, name := range orderedJSONSectionKeys(topLevelEntries, "scripts", raw) {
		rows = append(rows, map[string]any{
			"name":                  name,
			"line_number":           lineNumber,
			"end_line":              lineNumber,
			"args":                  []string{},
			"cyclomatic_complexity": 1,
			"source":                fmt.Sprint(raw[name]),
			"function_kind":         "json_script",
			"context":               "scripts",
			"context_type":          "json",
			"lang":                  lang,
		})
		lineNumber++
	}
	return rows
}

func tsconfigVariables(document map[string]any, lang string, topLevelEntries []orderedJSONEntry) []map[string]any {
	rows := make([]map[string]any, 0)
	lineNumber := 1

	if extendsValue, ok := document["extends"].(string); ok {
		rows = append(rows, map[string]any{
			"name":        "extends",
			"line_number": lineNumber,
			"value":       extendsValue,
			"section":     "extends",
			"config_kind": "extends",
			"lang":        lang,
		})
		lineNumber++
	}

	if references, ok := document["references"].([]any); ok {
		for _, item := range references {
			reference, ok := item.(map[string]any)
			if !ok {
				continue
			}
			referencePath, _ := reference["path"].(string)
			if strings.TrimSpace(referencePath) == "" {
				continue
			}
			rows = append(rows, map[string]any{
				"name":        "reference:" + referencePath,
				"line_number": lineNumber,
				"value":       referencePath,
				"section":     "references",
				"config_kind": "reference",
				"lang":        lang,
			})
			lineNumber++
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
	compilerOptionsEntries, ok, err := orderedJSONNestedObject(topLevelEntries, "compilerOptions")
	if err != nil || !ok {
		compilerOptionsEntries = nil
	}
	for _, alias := range orderedJSONSectionKeys(compilerOptionsEntries, "paths", paths) {
		rows = append(rows, map[string]any{
			"name":        "path:" + alias,
			"line_number": lineNumber,
			"value":       normalizeJSONArrayValue(paths[alias]),
			"section":     "compilerOptions.paths",
			"config_kind": "path",
			"lang":        lang,
		})
		lineNumber++
	}
	return rows
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

func stripJSONCComments(source string) string {
	var builder strings.Builder
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			builder.WriteByte(current)
			if escapeNext {
				escapeNext = false
				continue
			}
			if current == '\\' {
				escapeNext = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			builder.WriteByte(current)
			continue
		}
		if current == '/' && index+1 < len(source) {
			next := source[index+1]
			if next == '/' {
				for index < len(source) && source[index] != '\n' {
					index++
				}
				if index < len(source) {
					builder.WriteByte(source[index])
				}
				continue
			}
			if next == '*' {
				index += 2
				for index+1 < len(source) && (source[index] != '*' || source[index+1] != '/') {
					index++
				}
				index++
				continue
			}
		}
		builder.WriteByte(current)
	}
	return builder.String()
}

func stripTrailingCommas(source string) string {
	var builder strings.Builder
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			builder.WriteByte(current)
			if escapeNext {
				escapeNext = false
				continue
			}
			if current == '\\' {
				escapeNext = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			builder.WriteByte(current)
			continue
		}
		if current == ',' {
			rest := strings.TrimLeft(source[index+1:], " \t\r\n")
			if strings.HasPrefix(rest, "}") || strings.HasPrefix(rest, "]") {
				continue
			}
		}
		builder.WriteByte(current)
	}
	return builder.String()
}
