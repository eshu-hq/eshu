package groovy

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	groovyRootJenkinsPipelineEntrypoint = "groovy.jenkins_pipeline_entrypoint"
	groovyRootSharedLibraryCall         = "groovy.shared_library_call"
)

var (
	groovyClassDeclarationPattern    = regexp.MustCompile(`^\s*(?:@\w+(?:\([^)]*\))?\s*)*(?:(?:abstract|final|public)\s+)*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	groovyFunctionDeclarationPattern = regexp.MustCompile(
		`^\s*(?:(?:public|private|protected|static|final|synchronized)\s+)*(?:(?:def|void|boolean|byte|short|int|long|float|double|char|String|Object|Map|List|Set|Closure|[A-Z][A-Za-z0-9_<>,.?\[\]]*)\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)
	groovyFunctionCallPattern      = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	groovyJenkinsEntrypointPattern = regexp.MustCompile(`(?m)^\s*(pipeline|node)\s*\{`)
	groovyLineCommentPattern       = regexp.MustCompile(`^\s*//`)
	groovyBlockCommentStartPattern = regexp.MustCompile(`/\*`)
	groovyBlockCommentEndPattern   = regexp.MustCompile(`\*/`)
	groovyFunctionCallIgnoredNames = map[string]struct{}{"if": {}, "for": {}, "while": {}, "switch": {}, "catch": {}, "return": {}, "new": {}, "def": {}, "class": {}, "pipeline": {}, "node": {}, "stage": {}, "steps": {}, "script": {}, "environment": {}, "parameters": {}, "options": {}, "post": {}, "agent": {}, "when": {}}
)

// ExtractClassEntities returns class declarations that can become content
// entities. It is intentionally lexical: Groovy metaprogramming remains a
// named exactness blocker in query responses rather than a hidden claim.
func ExtractClassEntities(sourceText string) []map[string]any {
	lines := strings.Split(sourceText, "\n")
	classes := make([]map[string]any, 0)
	inBlockComment := false
	for i, line := range lines {
		if skipGroovyEntityLine(line, &inBlockComment) {
			continue
		}
		matches := groovyClassDeclarationPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		classes = append(classes, map[string]any{
			"name":        matches[1],
			"line_number": i + 1,
			"end_line":    i + 1,
		})
	}
	return classes
}

// ExtractFunctionEntities returns method and function declarations plus a
// synthetic Jenkinsfile root when the file is a pipeline entrypoint without
// ordinary declarations.
func ExtractFunctionEntities(path string, sourceText string) []map[string]any {
	lines := strings.Split(sourceText, "\n")
	functions := make([]map[string]any, 0)
	currentClass := ""
	classDepth := 0
	braceDepth := 0
	inBlockComment := false

	for i, line := range lines {
		skip := skipGroovyEntityLine(line, &inBlockComment)
		if !skip {
			if matches := groovyClassDeclarationPattern.FindStringSubmatch(line); matches != nil {
				currentClass = matches[1]
				classDepth = braceDepth + 1
			}
			if matches := groovyFunctionDeclarationPattern.FindStringSubmatch(line); matches != nil {
				item := map[string]any{
					"name":        matches[1],
					"line_number": i + 1,
					"end_line":    i + 1,
				}
				if currentClass != "" && braceDepth >= classDepth {
					item["class_context"] = currentClass
				}
				if isSharedLibraryVarsFile(path) && matches[1] == "call" {
					item["framework"] = "jenkins"
					item["dead_code_root_kinds"] = []string{groovyRootSharedLibraryCall}
				}
				functions = append(functions, item)
			}
		}
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		if currentClass != "" && braceDepth < classDepth {
			currentClass = ""
			classDepth = 0
		}
	}

	if isJenkinsfile(path) && groovyJenkinsEntrypointPattern.MatchString(sourceText) && !hasGroovyRoot(functions, groovyRootJenkinsPipelineEntrypoint) {
		functions = append(functions, map[string]any{
			"name":                 "Jenkinsfile",
			"line_number":          firstGroovyJenkinsEntrypointLine(sourceText),
			"end_line":             firstGroovyJenkinsEntrypointLine(sourceText),
			"framework":            "jenkins",
			"dead_code_root_kinds": []string{groovyRootJenkinsPipelineEntrypoint},
		})
	}
	return functions
}

// ExtractFunctionCallEntities returns lexical call evidence used by reducer
// call materialization. It does not try to resolve Groovy dynamic dispatch.
func ExtractFunctionCallEntities(sourceText string) []map[string]any {
	lines := strings.Split(sourceText, "\n")
	calls := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	inBlockComment := false
	for i, line := range lines {
		if skipGroovyEntityLine(line, &inBlockComment) {
			continue
		}
		declaredName := ""
		if matches := groovyFunctionDeclarationPattern.FindStringSubmatch(line); matches != nil {
			declaredName = matches[1]
		}
		for _, matches := range groovyFunctionCallPattern.FindAllStringSubmatch(line, -1) {
			name := strings.TrimSpace(matches[1])
			if name == "" || name == declaredName {
				continue
			}
			if _, ignored := groovyFunctionCallIgnoredNames[name]; ignored {
				continue
			}
			key := name + "\x00" + strconv.Itoa(i+1)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			calls = append(calls, map[string]any{
				"name":        name,
				"line_number": i + 1,
			})
		}
	}
	slices.SortFunc(calls, func(left, right map[string]any) int {
		if delta := intValue(left["line_number"]) - intValue(right["line_number"]); delta != 0 {
			return delta
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
	return calls
}

func isJenkinsfile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "jenkinsfile" || strings.HasPrefix(base, "jenkinsfile.")
}

func firstGroovyJenkinsEntrypointLine(sourceText string) int {
	lines := strings.Split(sourceText, "\n")
	for i, line := range lines {
		if groovyJenkinsEntrypointPattern.MatchString(line) {
			return i + 1
		}
	}
	return 1
}

func hasGroovyRoot(functions []map[string]any, rootKind string) bool {
	for _, function := range functions {
		for _, value := range stringSlice(function["dead_code_root_kinds"]) {
			if value == rootKind {
				return true
			}
		}
	}
	return false
}

func skipGroovyEntityLine(line string, inBlockComment *bool) bool {
	trimmed := strings.TrimSpace(line)
	if *inBlockComment {
		if groovyBlockCommentEndPattern.MatchString(trimmed) {
			*inBlockComment = false
		}
		return true
	}
	if groovyBlockCommentStartPattern.MatchString(trimmed) {
		*inBlockComment = !groovyBlockCommentEndPattern.MatchString(trimmed)
		return true
	}
	return trimmed == "" || groovyLineCommentPattern.MatchString(trimmed)
}

func stringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func intValue(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}
