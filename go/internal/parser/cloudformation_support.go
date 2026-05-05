package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	awsResourceTypePattern = regexp.MustCompile(`^AWS::\w+::\w+`)
	samResourceTypePattern = regexp.MustCompile(`^AWS::Serverless::\w+`)
)

type cloudFormationParseResult struct {
	resources  []map[string]any
	params     []map[string]any
	outputs    []map[string]any
	conditions []map[string]any
	imports    []map[string]any
	exports    []map[string]any
}

func isCloudFormationTemplate(document map[string]any) bool {
	if _, ok := document["AWSTemplateFormatVersion"]; ok {
		return true
	}

	switch transform := document["Transform"].(type) {
	case string:
		if transform == "AWS::Serverless-2016-10-31" {
			return true
		}
	case []any:
		for _, item := range transform {
			if fmt.Sprint(item) == "AWS::Serverless-2016-10-31" {
				return true
			}
		}
	}

	resources, ok := document["Resources"].(map[string]any)
	if !ok {
		return false
	}

	for _, item := range resources {
		body, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resourceType, _ := body["Type"].(string)
		if awsResourceTypePattern.MatchString(resourceType) || samResourceTypePattern.MatchString(resourceType) {
			return true
		}
	}

	return false
}

func parseCloudFormationTemplate(
	document map[string]any,
	path string,
	lineNumber int,
	lang string,
) cloudFormationParseResult {
	result := cloudFormationParseResult{}
	conditionEvaluations := evaluateCloudFormationConditions(document)
	withFormat := func(row map[string]any) map[string]any {
		row["file_format"] = lang
		return row
	}

	if params, ok := document["Parameters"].(map[string]any); ok {
		for _, name := range sortedMapKeys(params) {
			body, _ := params[name].(map[string]any)
			row := withFormat(map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"path":        path,
				"lang":        lang,
				"param_type":  "String",
			})
			setOptionalString(row, "param_type", body["Type"])
			setOptionalString(row, "default", body["Default"])
			setOptionalString(row, "description", body["Description"])
			if allowedValues, ok := body["AllowedValues"].([]any); ok && len(allowedValues) > 0 {
				row["allowed_values"] = joinInterfaceValues(allowedValues)
			}
			result.params = append(result.params, row)
		}
	}

	if conditions, ok := document["Conditions"].(map[string]any); ok {
		for _, name := range sortedMapKeys(conditions) {
			row := withFormat(map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"path":        path,
				"lang":        lang,
				"expression":  fmt.Sprint(conditions[name]),
			})
			if evaluation, ok := conditionEvaluations[name]; ok && evaluation.Resolved {
				row["evaluated"] = true
				row["evaluated_value"] = evaluation.Value
			}
			result.conditions = append(result.conditions, row)
		}
	}

	if resources, ok := document["Resources"].(map[string]any); ok {
		for _, name := range sortedMapKeys(resources) {
			body, _ := resources[name].(map[string]any)
			row := withFormat(map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"path":        path,
				"lang":        lang,
			})
			resourceType := fmt.Sprint(body["Type"])
			if strings.TrimSpace(resourceType) != "" && resourceType != "<nil>" {
				row["resource_type"] = resourceType
			}
			setOptionalString(row, "condition", body["Condition"])
			if conditionName, ok := row["condition"].(string); ok {
				if evaluation, ok := conditionEvaluations[conditionName]; ok && evaluation.Resolved {
					row["condition_evaluated"] = true
					row["condition_value"] = evaluation.Value
				}
			}
			if properties, ok := body["Properties"].(map[string]any); ok {
				setOptionalString(row, "template_url", properties["TemplateURL"])
			}
			if dependsOn := body["DependsOn"]; dependsOn != nil {
				switch typed := dependsOn.(type) {
				case []any:
					row["depends_on"] = joinInterfaceValues(typed)
				default:
					row["depends_on"] = fmt.Sprint(dependsOn)
				}
			}
			result.resources = append(result.resources, row)
		}
		rawImports := make([]string, 0)
		collectCloudFormationImports(resources, &rawImports)
		for _, name := range rawImports {
			result.imports = append(result.imports, withFormat(map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"path":        path,
				"lang":        lang,
			}))
		}
	}

	if outputs, ok := document["Outputs"].(map[string]any); ok {
		for _, name := range sortedMapKeys(outputs) {
			body, _ := outputs[name].(map[string]any)
			row := withFormat(map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"path":        path,
				"lang":        lang,
			})
			setOptionalString(row, "description", body["Description"])
			setOptionalString(row, "value", body["Value"])
			if exportBody, ok := body["Export"].(map[string]any); ok {
				setOptionalString(row, "export_name", exportBody["Name"])
				if exportName, ok := row["export_name"].(string); ok && strings.TrimSpace(exportName) != "" {
					result.exports = append(result.exports, withFormat(map[string]any{
						"name":        exportName,
						"line_number": lineNumber,
						"path":        path,
						"lang":        lang,
					}))
				}
			}
			setOptionalString(row, "condition", body["Condition"])
			if conditionName, ok := row["condition"].(string); ok {
				if evaluation, ok := conditionEvaluations[conditionName]; ok && evaluation.Resolved {
					row["condition_evaluated"] = true
					row["condition_value"] = evaluation.Value
				}
			}
			result.outputs = append(result.outputs, row)
		}
	}

	sortNamedMaps(result.resources)
	sortNamedMaps(result.params)
	sortNamedMaps(result.outputs)
	sortNamedMaps(result.conditions)
	sortNamedMaps(result.imports)
	sortNamedMaps(result.exports)
	return result
}

func collectCloudFormationImports(value any, collected *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "Fn::ImportValue" {
				*collected = append(*collected, fmt.Sprint(child))
				continue
			}
			collectCloudFormationImports(child, collected)
		}
	case []any:
		for _, child := range typed {
			collectCloudFormationImports(child, collected)
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

func sortNamedMaps(values []map[string]any) {
	sort.SliceStable(values, func(i, j int) bool {
		return lineNumberLess(values[i], values[j]) < 0
	})
}
