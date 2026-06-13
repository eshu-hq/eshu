package postgres

import (
	"encoding/json"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

var terraformBackendInterpolationPattern = regexp.MustCompile(`\$\{\s*([^}]+?)\s*\}`)

type terraformBackendFactContext struct {
	Backends  []map[string]any `json:"terraform_backends"`
	Variables []map[string]any `json:"terraform_variables"`
	Locals    []map[string]any `json:"terraform_locals"`
}

type terraformBackendResolutionContext struct {
	variables          map[string]string
	ambiguousVariables map[string]struct{}
	locals             map[string]string
	ambiguousLocals    map[string]struct{}
}

func terraformBackendCandidatesFromContext(
	repoID string,
	contextValue terraformBackendFactContext,
) []terraformstate.DiscoveryCandidate {
	candidates := make([]terraformstate.DiscoveryCandidate, 0, len(contextValue.Backends))
	for _, backend := range contextValue.Backends {
		resolution := newTerraformBackendResolutionContext(contextValue, stringValue(backend, "path"))
		candidate, ok := terraformBackendCandidate(repoID, backend, resolution)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func mergeTerraformBackendFactContext(
	dst terraformBackendFactContext,
	src terraformBackendFactContext,
) terraformBackendFactContext {
	dst.Backends = append(dst.Backends, src.Backends...)
	dst.Variables = append(dst.Variables, src.Variables...)
	dst.Locals = append(dst.Locals, src.Locals...)
	return dst
}

func decodeTerraformBackendFactContext(rawContext []byte) (terraformBackendFactContext, error) {
	trimmed := strings.TrimSpace(string(rawContext))
	if trimmed == "" {
		return terraformBackendFactContext{}, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var backends []map[string]any
		if err := json.Unmarshal(rawContext, &backends); err != nil {
			return terraformBackendFactContext{}, err
		}
		return terraformBackendFactContext{Backends: backends}, nil
	}

	var contextValue terraformBackendFactContext
	if err := json.Unmarshal(rawContext, &contextValue); err != nil {
		return terraformBackendFactContext{}, err
	}
	return contextValue, nil
}

func newTerraformBackendResolutionContext(
	contextValue terraformBackendFactContext,
	backendPath string,
) terraformBackendResolutionContext {
	moduleDir := terraformBackendModuleDir(backendPath)
	variables, ambiguousVariables := collectTerraformBackendNamedValues(
		contextValue.Variables,
		"default",
		moduleDir,
	)
	locals, ambiguousLocals := collectTerraformBackendNamedValues(
		contextValue.Locals,
		"value",
		moduleDir,
	)
	return terraformBackendResolutionContext{
		variables:          variables,
		ambiguousVariables: ambiguousVariables,
		locals:             locals,
		ambiguousLocals:    ambiguousLocals,
	}
}

func collectTerraformBackendNamedValues(
	rows []map[string]any,
	valueKey string,
	moduleDir string,
) (map[string]string, map[string]struct{}) {
	values := map[string]string{}
	ambiguous := map[string]struct{}{}
	for _, row := range rows {
		if terraformBackendModuleDir(stringValue(row, "path")) != moduleDir {
			continue
		}
		name := strings.TrimSpace(stringValue(row, "name"))
		value := strings.TrimSpace(stringValue(row, valueKey))
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

func terraformBackendModuleDir(relativePath string) string {
	cleaned := cleanFactRelativePath(relativePath)
	if cleaned == "" {
		return ""
	}
	dir := pathpkg.Dir(cleaned)
	if dir == "." {
		return ""
	}
	return dir
}

func resolveBackendAttribute(
	values map[string]any,
	name string,
	value string,
	resolution terraformBackendResolutionContext,
) (string, bool) {
	if isExactBackendAttribute(values, name, value) {
		return strings.TrimSpace(value), true
	}
	if !isResolvableBackendExpression(value) {
		return "", false
	}
	return resolution.resolveExpression(value, map[string]struct{}{})
}

func resolveOptionalBackendAttribute(
	values map[string]any,
	name string,
	resolution terraformBackendResolutionContext,
) string {
	value := strings.TrimSpace(stringValue(values, name))
	if value == "" {
		return ""
	}
	resolved, ok := resolveBackendAttribute(values, name, value, resolution)
	if !ok {
		return ""
	}
	return resolved
}

func isResolvableBackendExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "var.") ||
		strings.HasPrefix(value, "local.") ||
		strings.Contains(value, "${")
}

func (r terraformBackendResolutionContext) resolveExpression(
	expression string,
	seen map[string]struct{},
) (string, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", false
	}
	if kind, name, ok := splitTerraformBackendReference(expression); ok {
		return r.resolveReference(kind, name, seen)
	}
	if !strings.Contains(expression, "${") {
		if isExactBackendValue(expression) {
			return expression, true
		}
		return "", false
	}

	resolved := terraformBackendInterpolationPattern.ReplaceAllStringFunc(expression, func(match string) string {
		parts := terraformBackendInterpolationPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return "\x00"
		}
		value, ok := r.resolveExpression(strings.TrimSpace(parts[1]), seen)
		if !ok {
			return "\x00"
		}
		return value
	})
	if strings.Contains(resolved, "\x00") || !isExactBackendValue(resolved) {
		return "", false
	}
	return resolved, true
}

func (r terraformBackendResolutionContext) resolveReference(
	kind string,
	name string,
	seen map[string]struct{},
) (string, bool) {
	switch kind {
	case "var":
		if _, ambiguous := r.ambiguousVariables[name]; ambiguous {
			return "", false
		}
		value, ok := r.variables[name]
		if !ok || !isExactBackendValue(value) {
			return "", false
		}
		return value, true
	case "local":
		if _, ambiguous := r.ambiguousLocals[name]; ambiguous {
			return "", false
		}
		key := "local." + name
		if _, active := seen[key]; active {
			return "", false
		}
		value, ok := r.locals[name]
		if !ok {
			return "", false
		}
		seen[key] = struct{}{}
		defer delete(seen, key)
		return r.resolveExpression(value, seen)
	default:
		return "", false
	}
}

func splitTerraformBackendReference(expression string) (string, string, bool) {
	for _, prefix := range []string{"var.", "local."} {
		if !strings.HasPrefix(expression, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(expression, prefix))
		if !isTerraformBackendIdentifier(name) {
			return "", "", false
		}
		return strings.TrimSuffix(prefix, "."), name, true
	}
	return "", "", false
}

func isTerraformBackendIdentifier(value string) bool {
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
