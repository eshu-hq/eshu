package query

import (
	"slices"
	"strings"
)

func deadCodeIsPythonFrameworkRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "python" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	if slices.Contains(rootKinds, "python.fastapi_route_decorator") ||
		slices.Contains(rootKinds, "python.flask_route_decorator") ||
		slices.Contains(rootKinds, "python.celery_task_decorator") ||
		slices.Contains(rootKinds, "python.click_command_decorator") ||
		slices.Contains(rootKinds, "python.typer_command_decorator") ||
		slices.Contains(rootKinds, "python.typer_callback_decorator") ||
		slices.Contains(rootKinds, "python.script_main_guard") ||
		slices.Contains(rootKinds, "python.aws_lambda_handler") ||
		slices.Contains(rootKinds, "python.dataclass_model") ||
		slices.Contains(rootKinds, "python.dataclass_post_init") ||
		slices.Contains(rootKinds, "python.property_decorator") ||
		slices.Contains(rootKinds, "python.module_all_export") ||
		slices.Contains(rootKinds, "python.package_init_export") ||
		slices.Contains(rootKinds, "python.dunder_method") ||
		slices.Contains(rootKinds, "python.public_api_member") ||
		slices.Contains(rootKinds, "python.public_api_base") {
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}

func deadCodeIsPythonAnonymousLambda(result map[string]any, entity *EntityContent) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "python" {
		return false
	}
	name := strings.TrimSpace(StringVal(result, "name"))
	if entity != nil && strings.TrimSpace(entity.EntityName) != "" {
		name = strings.TrimSpace(entity.EntityName)
	}
	if !strings.HasPrefix(name, "lambda@") {
		return false
	}
	metadata, _ := result["metadata"].(map[string]any)
	if strings.TrimSpace(StringVal(metadata, "semantic_kind")) == "lambda" {
		return true
	}
	if entity != nil && strings.TrimSpace(StringVal(entity.Metadata, "semantic_kind")) == "lambda" {
		return true
	}
	return false
}
