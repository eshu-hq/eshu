package cloudformation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	awsResourceTypePattern = regexp.MustCompile(`^AWS::\w+::\w+`)
	samResourceTypePattern = regexp.MustCompile(`^AWS::Serverless::\w+`)
)

// Result groups CloudFormation entity buckets extracted from one template.
type Result struct {
	Resources  []map[string]any
	Params     []map[string]any
	Outputs    []map[string]any
	Conditions []map[string]any
	Imports    []map[string]any
	Exports    []map[string]any
}

// IsTemplate reports whether document has bounded CloudFormation or SAM shape.
func IsTemplate(document map[string]any) bool {
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

// Parse extracts CloudFormation buckets from a JSON or YAML template document.
func Parse(
	document map[string]any,
	path string,
	lineNumber int,
	lang string,
) Result {
	result := Result{}
	conditionEvaluations := evaluateConditions(document)
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
			result.Params = append(result.Params, row)
		}
	}

	appendConditions(&result, document, conditionEvaluations, lineNumber, path, lang, withFormat)
	appendResources(&result, document, conditionEvaluations, lineNumber, path, lang, withFormat)
	appendOutputs(&result, document, conditionEvaluations, lineNumber, path, lang, withFormat)

	shared.SortNamedMaps(result.Resources)
	shared.SortNamedMaps(result.Params)
	shared.SortNamedMaps(result.Outputs)
	shared.SortNamedMaps(result.Conditions)
	shared.SortNamedMaps(result.Imports)
	shared.SortNamedMaps(result.Exports)
	return result
}

func appendConditions(
	result *Result,
	document map[string]any,
	conditionEvaluations map[string]conditionEvaluation,
	lineNumber int,
	path string,
	lang string,
	withFormat func(map[string]any) map[string]any,
) {
	conditions, ok := document["Conditions"].(map[string]any)
	if !ok {
		return
	}
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
		result.Conditions = append(result.Conditions, row)
	}
}

func appendResources(
	result *Result,
	document map[string]any,
	conditionEvaluations map[string]conditionEvaluation,
	lineNumber int,
	path string,
	lang string,
	withFormat func(map[string]any) map[string]any,
) {
	resources, ok := document["Resources"].(map[string]any)
	if !ok {
		return
	}
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
		result.Resources = append(result.Resources, row)
	}
	rawImports := make([]string, 0)
	collectImports(resources, &rawImports)
	for _, name := range rawImports {
		result.Imports = append(result.Imports, withFormat(map[string]any{
			"name":        name,
			"line_number": lineNumber,
			"path":        path,
			"lang":        lang,
		}))
	}
}
