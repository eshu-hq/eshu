// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeadCodeIsPythonFrameworkRoot reports whether the candidate is a Python framework root.
func DeadCodeIsPythonFrameworkRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
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

// DeadCodeIsPythonAnonymousLambda reports whether the candidate is an anonymous Python lambda.
func DeadCodeIsPythonAnonymousLambda(result map[string]any, entity *querycontract.EntityContent) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "python" {
		return false
	}
	name := strings.TrimSpace(querycontract.StringVal(result, "name"))
	if entity != nil && strings.TrimSpace(entity.EntityName) != "" {
		name = strings.TrimSpace(entity.EntityName)
	}
	if !strings.HasPrefix(name, "lambda@") {
		return false
	}
	metadata, _ := result["metadata"].(map[string]any)
	if strings.TrimSpace(querycontract.StringVal(metadata, "semantic_kind")) == "lambda" {
		return true
	}
	if entity != nil && strings.TrimSpace(querycontract.StringVal(entity.Metadata, "semantic_kind")) == "lambda" {
		return true
	}
	return false
}
