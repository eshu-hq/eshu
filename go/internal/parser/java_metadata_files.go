package parser

import (
	"path/filepath"
	"regexp"
	"strings"
)

var javaMetadataClassNamePattern = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)+$`)

func parseJavaMetadata(path string, isDependency bool) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	payload := basePayload(path, "java_metadata", isDependency)
	for _, ref := range javaMetadataClassReferences(path, string(source)) {
		appendBucket(payload, "function_calls", ref)
	}
	sortNamedBucket(payload, "function_calls")
	return payload, nil
}

func javaMetadataClassReferences(path string, source string) []map[string]any {
	kind := javaMetadataReferenceKind(path)
	if kind == "" {
		return nil
	}
	values := javaMetadataClassNames(path, source)
	refs := make([]map[string]any, 0, len(values))
	for _, value := range values {
		refs = append(refs, map[string]any{
			"name":             javaTypeLeafName(value.className),
			"full_name":        value.className,
			"line_number":      value.lineNumber,
			"lang":             "java_metadata",
			"call_kind":        kind,
			"referenced_class": value.className,
		})
	}
	return refs
}

func javaMetadataReferenceKind(path string) string {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.HasPrefix(normalized, "meta-inf/services/") ||
		strings.Contains(normalized, "/meta-inf/services/") {
		return "java.service_loader_provider"
	}
	if normalized == "meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports" ||
		strings.HasSuffix(normalized, "/meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports") ||
		normalized == "meta-inf/spring.factories" ||
		strings.HasSuffix(normalized, "/meta-inf/spring.factories") {
		return "java.spring_autoconfiguration_class"
	}
	return ""
}

type javaMetadataClassName struct {
	className  string
	lineNumber int
}

func javaMetadataClassNames(path string, source string) []javaMetadataClassName {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if normalized == "meta-inf/spring.factories" || strings.HasSuffix(normalized, "/meta-inf/spring.factories") {
		return javaSpringFactoriesClassNames(source)
	}
	return javaLineClassNames(source)
}

func javaLineClassNames(source string) []javaMetadataClassName {
	var names []javaMetadataClassName
	seen := make(map[string]struct{})
	for index, line := range strings.Split(source, "\n") {
		if candidate := javaMetadataCleanLine(line); candidate != "" {
			names = appendJavaMetadataClassName(names, seen, candidate, index+1)
		}
	}
	return names
}

func javaSpringFactoriesClassNames(source string) []javaMetadataClassName {
	var names []javaMetadataClassName
	seen := make(map[string]struct{})
	joined, lineNumber := "", 1
	for index, line := range strings.Split(source, "\n") {
		cleaned := javaMetadataCleanLine(line)
		if cleaned == "" && joined == "" {
			continue
		}
		if joined == "" {
			lineNumber = index + 1
		}
		continued := strings.HasSuffix(cleaned, `\`)
		cleaned = strings.TrimSuffix(cleaned, `\`)
		joined += cleaned
		if continued {
			continue
		}
		for _, candidate := range javaSpringFactoriesClassNameTokens(joined) {
			names = appendJavaMetadataClassName(names, seen, candidate, lineNumber)
		}
		joined = ""
	}
	if joined != "" {
		for _, candidate := range javaSpringFactoriesClassNameTokens(joined) {
			names = appendJavaMetadataClassName(names, seen, candidate, lineNumber)
		}
	}
	return names
}

func javaSpringFactoriesClassNameTokens(line string) []string {
	_, value, ok := strings.Cut(line, "=")
	if !ok {
		value = line
	}
	parts := strings.Split(value, ",")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if candidate := strings.TrimSpace(part); candidate != "" {
			tokens = append(tokens, candidate)
		}
	}
	return tokens
}

func appendJavaMetadataClassName(
	names []javaMetadataClassName,
	seen map[string]struct{},
	candidate string,
	lineNumber int,
) []javaMetadataClassName {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !javaMetadataClassNamePattern.MatchString(candidate) {
		return names
	}
	if _, ok := seen[candidate]; ok {
		return names
	}
	seen[candidate] = struct{}{}
	return append(names, javaMetadataClassName{className: candidate, lineNumber: lineNumber})
}

func javaMetadataCleanLine(line string) string {
	value, _, _ := strings.Cut(line, "#")
	return strings.TrimSpace(value)
}
