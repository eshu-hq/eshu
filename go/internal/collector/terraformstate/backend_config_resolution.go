// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
)

var backendInterpolationPattern = regexp.MustCompile(`\$\{\s*([^}]+?)\s*\}`)

type backendResolutionContext struct {
	variables          map[string]string
	ambiguousVariables map[string]struct{}
	locals             map[string]string
	ambiguousLocals    map[string]struct{}
}

func newBackendResolutionContext(contextValue BackendConfigContext, backendPath string) backendResolutionContext {
	moduleDir := backendModuleDir(backendPath)
	variables, ambiguousVariables := collectBackendNamedValues(contextValue.Variables, "default", moduleDir)
	locals, ambiguousLocals := collectBackendNamedValues(contextValue.Locals, "value", moduleDir)
	return backendResolutionContext{
		variables:          variables,
		ambiguousVariables: ambiguousVariables,
		locals:             locals,
		ambiguousLocals:    ambiguousLocals,
	}
}

func collectBackendNamedValues(
	rows []map[string]any,
	valueKey string,
	moduleDir string,
) (map[string]string, map[string]struct{}) {
	values := map[string]string{}
	ambiguous := map[string]struct{}{}
	for _, row := range rows {
		if backendModuleDir(backendStringValue(row, "path")) != moduleDir {
			continue
		}
		name := strings.TrimSpace(backendStringValue(row, "name"))
		value := strings.TrimSpace(backendStringValue(row, valueKey))
		if name == "" || value == "" {
			continue
		}
		if _, seen := values[name]; seen {
			delete(values, name)
			ambiguous[name] = struct{}{}
			continue
		}
		if _, seen := ambiguous[name]; seen {
			continue
		}
		values[name] = value
	}
	return values, ambiguous
}

func backendModuleDir(relativePath string) string {
	cleaned := cleanBackendConfigRelativePath(relativePath)
	if cleaned == "" {
		return ""
	}
	dir := pathpkg.Dir(cleaned)
	if dir == "." {
		return ""
	}
	return dir
}

type backendAttributeDecision struct {
	value          string
	ok             bool
	reason         string
	expressionKind string
}

func resolveOptionalBackendConfigAttribute(
	values map[string]any,
	name string,
	resolution backendResolutionContext,
) string {
	value, ok := resolveBackendConfigAttribute(values, name, resolution)
	if !ok {
		return ""
	}
	return value
}

func resolveBackendConfigAttribute(
	values map[string]any,
	name string,
	resolution backendResolutionContext,
) (string, bool) {
	decision := resolveBackendConfigAttributeDecision(values, name, resolution)
	return decision.value, decision.ok
}

func resolveBackendConfigAttributeDecision(
	values map[string]any,
	name string,
	resolution backendResolutionContext,
) backendAttributeDecision {
	value := strings.TrimSpace(backendStringValue(values, name))
	if isExactBackendConfigAttribute(values, name, value) {
		return backendAttributeDecision{value: value, ok: true, expressionKind: "literal"}
	}
	if value == "" {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonNonExactValue,
			expressionKind: "missing",
		}
	}
	if !isResolvableBackendConfigExpression(value) {
		return backendExpressionFailure(value)
	}
	return resolution.resolveExpression(value, map[string]struct{}{})
}

func isResolvableBackendConfigExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "var.") ||
		strings.HasPrefix(value, "local.") ||
		strings.Contains(value, "${")
}

func (r backendResolutionContext) resolveExpression(
	expression string,
	seen map[string]struct{},
) backendAttributeDecision {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonNonExactValue,
			expressionKind: "missing",
		}
	}
	if kind, name, ok := splitBackendReference(expression); ok {
		return r.resolveReference(kind, name, seen)
	}
	if !strings.Contains(expression, "${") {
		if isExactBackendConfigValue(expression) {
			return backendAttributeDecision{value: expression, ok: true, expressionKind: "literal"}
		}
		return backendExpressionFailure(expression)
	}

	resolved := expression
	for _, match := range backendInterpolationPattern.FindAllStringSubmatch(expression, -1) {
		if len(match) < 2 {
			return backendAttributeDecision{
				ok:             false,
				reason:         backendWarningReasonUnresolvedInterpolation,
				expressionKind: "interpolation",
			}
		}
		inner := strings.TrimSpace(match[1])
		decision := r.resolveExpression(inner, seen)
		if !decision.ok {
			if decision.expressionKind == "" {
				decision.expressionKind = "interpolation"
			}
			return decision
		}
		resolved = strings.ReplaceAll(resolved, match[0], decision.value)
	}
	if !isExactBackendConfigValue(resolved) {
		return backendExpressionFailure(expression)
	}
	return backendAttributeDecision{value: resolved, ok: true, expressionKind: "interpolation"}
}

func (r backendResolutionContext) resolveReference(
	kind string,
	name string,
	seen map[string]struct{},
) backendAttributeDecision {
	switch kind {
	case "var":
		return r.resolveVariableReference(name)
	case "local":
		return r.resolveLocalReference(name, seen)
	default:
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonUnsupportedReference,
			expressionKind: backendExpressionKind(kind + "." + name),
		}
	}
}

func (r backendResolutionContext) resolveVariableReference(name string) backendAttributeDecision {
	if _, ambiguous := r.ambiguousVariables[name]; ambiguous {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonAmbiguousVariableDefault,
			expressionKind: "var_reference",
		}
	}
	value, ok := r.variables[name]
	if !ok || !isExactBackendConfigValue(value) {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonMissingVariableDefault,
			expressionKind: "var_reference",
		}
	}
	return backendAttributeDecision{value: value, ok: true, expressionKind: "var_reference"}
}

func (r backendResolutionContext) resolveLocalReference(
	name string,
	seen map[string]struct{},
) backendAttributeDecision {
	if _, ambiguous := r.ambiguousLocals[name]; ambiguous {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonAmbiguousLocalValue,
			expressionKind: "local_reference",
		}
	}
	key := "local." + name
	if _, active := seen[key]; active {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonCyclicLocalValue,
			expressionKind: "local_reference",
		}
	}
	value, ok := r.locals[name]
	if !ok {
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonMissingLocalValue,
			expressionKind: "local_reference",
		}
	}
	seen[key] = struct{}{}
	defer delete(seen, key)
	decision := r.resolveExpression(value, seen)
	if !decision.ok && decision.expressionKind == "" {
		decision.expressionKind = "local_reference"
	}
	return decision
}

func backendExpressionFailure(expression string) backendAttributeDecision {
	expression = strings.TrimSpace(expression)
	switch {
	case strings.Contains(expression, "terraform.workspace"):
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonWorkspaceInterpolation,
			expressionKind: "terraform_workspace",
		}
	case strings.Contains(expression, "("):
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonFunctionCall,
			expressionKind: "function_call",
		}
	case strings.HasPrefix(expression, "module.") ||
		strings.HasPrefix(expression, "path.") ||
		strings.HasPrefix(expression, "terraform.") ||
		strings.HasPrefix(expression, "cty."):
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonUnsupportedReference,
			expressionKind: backendExpressionKind(expression),
		}
	case strings.Contains(expression, "${"):
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonUnresolvedInterpolation,
			expressionKind: "interpolation",
		}
	default:
		return backendAttributeDecision{
			ok:             false,
			reason:         backendWarningReasonNonExactValue,
			expressionKind: backendExpressionKind(expression),
		}
	}
}

func splitBackendReference(expression string) (string, string, bool) {
	for _, prefix := range []string{"var.", "local."} {
		if !strings.HasPrefix(expression, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(expression, prefix))
		if !isBackendIdentifier(name) {
			return "", "", false
		}
		return strings.TrimSuffix(prefix, "."), name, true
	}
	return "", "", false
}

func isBackendIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		isLetter := char == '_' || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')
		isDigit := char >= '0' && char <= '9'
		if index == 0 {
			if !isLetter {
				return false
			}
			continue
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}

func isExactBackendConfigAttribute(values map[string]any, name string, value string) bool {
	literalKey := name + "_is_literal"
	switch literal := values[literalKey].(type) {
	case bool:
		return literal && isExactBackendConfigValue(value)
	case string:
		return strings.EqualFold(strings.TrimSpace(literal), "true") && isExactBackendConfigValue(value)
	default:
		return false
	}
}

func isExactBackendConfigValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "${") || strings.Contains(value, "(") {
		return false
	}
	for _, dynamicPrefix := range []string{"var.", "local.", "module.", "path.", "terraform.", "cty."} {
		if strings.HasPrefix(value, dynamicPrefix) {
			return false
		}
	}
	return true
}

func backendExpressionKind(expression string) string {
	expression = strings.TrimSpace(expression)
	switch {
	case strings.HasPrefix(expression, "var."):
		return "var_reference"
	case strings.HasPrefix(expression, "local."):
		return "local_reference"
	case strings.HasPrefix(expression, "module."):
		return "module_reference"
	case strings.Contains(expression, "terraform.workspace"):
		return "terraform_workspace"
	case strings.HasPrefix(expression, "path."):
		return "path_reference"
	case strings.HasPrefix(expression, "terraform."):
		return "terraform_reference"
	case strings.Contains(expression, "("):
		return "function_call"
	case strings.Contains(expression, "${"):
		return "interpolation"
	case expression == "":
		return "missing"
	default:
		return "non_literal"
	}
}

func backendStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func backendIntValue(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

type jsonNumber interface {
	String() string
}

func cleanBackendConfigRelativePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = strings.TrimPrefix(path, "./")
	return strings.Trim(path, "/")
}
