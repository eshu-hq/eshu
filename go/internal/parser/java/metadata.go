package java

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	serviceLoaderProviderKind       = "java.service_loader_provider"
	springAutoConfigurationKind     = "java.spring_autoconfiguration_class"
	springFactoriesPath             = "meta-inf/spring.factories"
	springAutoConfigurationPath     = "meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports"
	metaInfServicesPathPrefix       = "meta-inf/services/"
	metaInfServicesPathSegment      = "/meta-inf/services/"
	springFactoriesPathSuffix       = "/meta-inf/spring.factories"
	springAutoConfigurationSuffix   = "/meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports"
	javaMetadataClassNameExpression = `^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)+$`
)

var metadataClassNamePattern = regexp.MustCompile(javaMetadataClassNameExpression)

// ClassReference is a class name proven by bounded Java metadata files such as
// META-INF/services or Spring Boot auto-configuration lists.
type ClassReference struct {
	Name       string
	FullName   string
	LineNumber int
	Kind       string
}

type metadataClassName struct {
	className  string
	lineNumber int
}

// MetadataClassReferences extracts statically named Java classes from
// ServiceLoader and Spring metadata files. Invalid, dynamic, duplicate, or
// unsupported metadata lines are ignored instead of becoming graph evidence.
func MetadataClassReferences(path string, source string) []ClassReference {
	kind := metadataReferenceKind(path)
	if kind == "" {
		return nil
	}
	values := metadataClassNames(path, source)
	refs := make([]ClassReference, 0, len(values))
	for _, value := range values {
		refs = append(refs, ClassReference{
			Name:       typeLeafName(value.className),
			FullName:   value.className,
			LineNumber: value.lineNumber,
			Kind:       kind,
		})
	}
	return refs
}

func metadataReferenceKind(path string) string {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.HasPrefix(normalized, metaInfServicesPathPrefix) ||
		strings.Contains(normalized, metaInfServicesPathSegment) {
		return serviceLoaderProviderKind
	}
	if normalized == springAutoConfigurationPath ||
		strings.HasSuffix(normalized, springAutoConfigurationSuffix) ||
		normalized == springFactoriesPath ||
		strings.HasSuffix(normalized, springFactoriesPathSuffix) {
		return springAutoConfigurationKind
	}
	return ""
}

func metadataClassNames(path string, source string) []metadataClassName {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if normalized == springFactoriesPath || strings.HasSuffix(normalized, springFactoriesPathSuffix) {
		return springFactoriesClassNames(source)
	}
	return lineClassNames(source)
}

func lineClassNames(source string) []metadataClassName {
	var names []metadataClassName
	seen := make(map[string]struct{})
	for index, line := range strings.Split(source, "\n") {
		if candidate := metadataCleanLine(line); candidate != "" {
			names = appendMetadataClassName(names, seen, candidate, index+1)
		}
	}
	return names
}

func springFactoriesClassNames(source string) []metadataClassName {
	var names []metadataClassName
	seen := make(map[string]struct{})
	joined, lineNumber := "", 1
	for index, line := range strings.Split(source, "\n") {
		cleaned := metadataCleanLine(line)
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
		for _, candidate := range springFactoriesClassNameTokens(joined) {
			names = appendMetadataClassName(names, seen, candidate, lineNumber)
		}
		joined = ""
	}
	if joined != "" {
		for _, candidate := range springFactoriesClassNameTokens(joined) {
			names = appendMetadataClassName(names, seen, candidate, lineNumber)
		}
	}
	return names
}

func springFactoriesClassNameTokens(line string) []string {
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

func appendMetadataClassName(
	names []metadataClassName,
	seen map[string]struct{},
	candidate string,
	lineNumber int,
) []metadataClassName {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !metadataClassNamePattern.MatchString(candidate) {
		return names
	}
	if _, ok := seen[candidate]; ok {
		return names
	}
	seen[candidate] = struct{}{}
	return append(names, metadataClassName{className: candidate, lineNumber: lineNumber})
}

func metadataCleanLine(line string) string {
	value, _, _ := strings.Cut(line, "#")
	return strings.TrimSpace(value)
}

func typeLeafName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.LastIndex(value, "."); idx >= 0 {
		value = strings.TrimSpace(value[idx+1:])
	}
	return strings.TrimSpace(value)
}
