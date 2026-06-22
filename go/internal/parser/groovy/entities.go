package groovy

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	groovyRootJenkinsPipelineEntrypoint = "groovy.jenkins_pipeline_entrypoint"
	groovyRootSharedLibraryCall         = "groovy.shared_library_call"
)

var (
	// groovyJenkinsEntrypointPattern detects a top-level declarative or scripted
	// Jenkins pipeline (`pipeline {` / `node {`) so the parser can attach a
	// synthetic Jenkinsfile entrypoint root. The block opener is a Jenkins DSL
	// idiom rather than a distinct Groovy syntax node, so it is matched on source
	// text after the tree-sitter parse rather than from a dedicated AST node.
	groovyJenkinsEntrypointPattern = regexp.MustCompile(`(?m)^\s*(pipeline|node)\s*\{`)
	// groovyFunctionCallIgnoredNames are Groovy keywords and Jenkins DSL block
	// names that must not be reported as method calls. The tree-sitter call
	// extraction in tree_sitter_syntax.go consults this set.
	groovyFunctionCallIgnoredNames = map[string]struct{}{"if": {}, "for": {}, "while": {}, "switch": {}, "catch": {}, "return": {}, "new": {}, "def": {}, "class": {}, "pipeline": {}, "node": {}, "stage": {}, "steps": {}, "script": {}, "environment": {}, "parameters": {}, "options": {}, "post": {}, "agent": {}, "when": {}}
)

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
