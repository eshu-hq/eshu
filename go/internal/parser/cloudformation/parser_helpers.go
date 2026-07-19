// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import (
	"fmt"
	"sort"
	"strings"
)

func appendOutputs(
	result *Result,
	document map[string]any,
	conditionEvaluations map[string]conditionEvaluation,
	lineNumber int,
	path string,
	lang string,
	withFormat func(map[string]any) map[string]any,
	sectionPositions SectionPositions,
) {
	outputs, ok := document["Outputs"].(map[string]any)
	if !ok {
		return
	}
	for _, name := range sortedMapKeys(outputs) {
		body, _ := outputs[name].(map[string]any)
		startLine, endLine, knownLine := sectionPositions.linesFor(name, lineNumber)
		row := withFormat(map[string]any{
			"name":        name,
			"line_number": startLine,
			"path":        path,
			"lang":        lang,
		})
		if knownLine {
			row["end_line"] = endLine
		}
		setOptionalString(row, "description", body["Description"])
		setOptionalString(row, "value", body["Value"])
		if exportBody, ok := body["Export"].(map[string]any); ok {
			setOptionalString(row, "export_name", exportBody["Name"])
			if exportName, ok := row["export_name"].(string); ok && strings.TrimSpace(exportName) != "" {
				// An Export always nests inside its Output in the template
				// shape, so it inherits the owning Output's own EntityPosition
				// rather than getting a separately-walked line.
				exportRow := withFormat(map[string]any{
					"name":        exportName,
					"line_number": startLine,
					"path":        path,
					"lang":        lang,
				})
				if knownLine {
					exportRow["end_line"] = endLine
				}
				result.Exports = append(result.Exports, exportRow)
			}
		}
		setOptionalString(row, "condition", body["Condition"])
		if conditionName, ok := row["condition"].(string); ok {
			if evaluation, ok := conditionEvaluations[conditionName]; ok && evaluation.Resolved {
				row["condition_evaluated"] = true
				row["condition_value"] = evaluation.Value
			}
		}
		result.Outputs = append(result.Outputs, row)
	}
}

func collectImports(value any, collected *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "Fn::ImportValue" {
				*collected = append(*collected, fmt.Sprint(child))
				continue
			}
			collectImports(child, collected)
		}
	case []any:
		for _, child := range typed {
			collectImports(child, collected)
		}
	}
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func setOptionalString(target map[string]any, key string, value any) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return
	}
	target[key] = text
}

func joinInterfaceValues(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
