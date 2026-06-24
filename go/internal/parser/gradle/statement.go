// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"sort"
	"strings"
)

var configurationKeywords = map[string]struct{}{
	"implementation":            {},
	"api":                       {},
	"compileOnly":               {},
	"compileOnlyApi":            {},
	"runtimeOnly":               {},
	"compile":                   {},
	"runtime":                   {},
	"testImplementation":        {},
	"testApi":                   {},
	"testCompileOnly":           {},
	"testRuntimeOnly":           {},
	"androidTestImplementation": {},
	"annotationProcessor":       {},
	"kapt":                      {},
	"ksp":                       {},
	"developmentOnly":           {},
	"classpath":                 {},
	"providedCompile":           {},
	"providedRuntime":           {},
	"detektPlugins":             {},
}

// parseDependencyStatement turns a single Gradle dependency declaration into
// a content_entity-shaped row. Returns false when the statement does not
// open with a recognized configuration keyword or does not carry a
// resolvable Maven coordinate.
func parseDependencyStatement(
	statement string,
	parentSection string,
	properties map[string]string,
	lineNumber int,
) (map[string]any, bool) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return nil, false
	}
	primary := primaryPart(statement)
	configuration, payload := splitConfigurationAndArgs(primary)
	if configuration == "" {
		return nil, false
	}
	if _, ok := configurationKeywords[configuration]; !ok {
		return nil, false
	}
	payload = stripOuterParens(strings.TrimSpace(payload))

	wrapperKind := ""
	inner := payload
	for {
		name, args, ok := parseSingleCall(inner)
		if !ok {
			break
		}
		switch name {
		case "platform", "enforcedPlatform":
			wrapperKind = name
			inner = strings.TrimSpace(args)
			continue
		case "project", "files", "fileTree", "gradleApi", "localGroovy":
			return nil, false
		case "kotlin":
			coord, ok := kotlinHelperCoordinate(args)
			if !ok {
				return nil, false
			}
			return buildRow(configuration, parentSection, wrapperKind, coord.group, coord.artifact, coord.version, properties, lineNumber)
		}
		break
	}

	coord, ok := extractCoordinate(inner)
	if !ok {
		return nil, false
	}
	return buildRow(configuration, parentSection, wrapperKind, coord.group, coord.artifact, coord.version, properties, lineNumber)
}

// primaryPart returns the statement prefix up to the first top-level `{`,
// which lets configuration closures such as `implementation('g:a') { exclude
// ... }` be parsed without their closure body interfering.
func primaryPart(statement string) string {
	depth := 0
	inString := false
	var stringQuote byte
	for index := 0; index < len(statement); index++ {
		current := statement[index]
		if inString {
			if current == '\\' && index+1 < len(statement) {
				index++
				continue
			}
			if current == stringQuote {
				inString = false
			}
			continue
		}
		switch current {
		case '\'', '"':
			inString = true
			stringQuote = current
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '{':
			if depth == 0 {
				return strings.TrimSpace(statement[:index])
			}
		}
	}
	return strings.TrimSpace(statement)
}

func splitConfigurationAndArgs(statement string) (string, string) {
	index := 0
	for index < len(statement) {
		current := statement[index]
		if current == ' ' || current == '\t' || current == '(' {
			break
		}
		index++
	}
	if index == 0 {
		return "", ""
	}
	configuration := statement[:index]
	rest := strings.TrimSpace(statement[index:])
	return configuration, rest
}

func stripOuterParens(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "(") {
		return value
	}
	depth := 0
	matchingClose := -1
	inString := false
	var stringQuote byte
	for index := 0; index < len(value) && matchingClose < 0; index++ {
		current := value[index]
		if inString {
			if current == '\\' && index+1 < len(value) {
				index++
				continue
			}
			if current == stringQuote {
				inString = false
			}
			continue
		}
		switch current {
		case '\'', '"':
			inString = true
			stringQuote = current
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				matchingClose = index
			}
		}
	}
	if matchingClose != len(value)-1 {
		return value
	}
	return strings.TrimSpace(value[1:matchingClose])
}

// parseSingleCall recognizes a wrapper call of the form `name(args)`. It
// requires `value` to start with the identifier and end with the matching
// close paren; otherwise the value is not a single call expression and the
// caller should treat it as a literal coordinate payload.
func parseSingleCall(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	openParen := strings.IndexByte(value, '(')
	if openParen <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(value[:openParen])
	for _, ch := range name {
		if !isIdentifierByte(ch) {
			return "", "", false
		}
	}
	if !strings.HasSuffix(value, ")") {
		return "", "", false
	}
	depth := 0
	inString := false
	var stringQuote byte
	close := -1
	for index := openParen; index < len(value) && close < 0; index++ {
		current := value[index]
		if inString {
			if current == '\\' && index+1 < len(value) {
				index++
				continue
			}
			if current == stringQuote {
				inString = false
			}
			continue
		}
		switch current {
		case '\'', '"':
			inString = true
			stringQuote = current
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				close = index
			}
		}
	}
	if close != len(value)-1 {
		return "", "", false
	}
	inner := strings.TrimSpace(value[openParen+1 : close])
	return name, inner, true
}

func isIdentifierByte(ch rune) bool {
	switch {
	case ch == '_':
		return true
	case ch >= 'a' && ch <= 'z':
		return true
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= '0' && ch <= '9':
		return true
	}
	return false
}

func buildRow(
	configuration string,
	parentSection string,
	wrapperKind string,
	group string,
	artifact string,
	version string,
	properties map[string]string,
	lineNumber int,
) (map[string]any, bool) {
	if group == "" || artifact == "" {
		return nil, false
	}
	section := configuration
	if parentSection != "" {
		section = parentSection + ":" + configuration
	}
	scope := configurationScope(configuration)
	if wrapperKind != "" {
		// Section keeps the wrapper distinction (`:platform` vs
		// `:enforcedPlatform`) so callers can see which form was declared,
		// but the scope vocabulary stays `platform` for both. This matches
		// the documented scope set in README.md / doc.go and keeps
		// downstream impact-priority logic from treating
		// `enforcedPlatform` as an unknown scope.
		section = section + ":" + wrapperKind
		scope = "platform"
	}

	resolutionState := "resolved"
	unresolvedKeys := []string(nil)
	rawVersion := strings.TrimSpace(version)
	value := rawVersion
	switch {
	case rawVersion == "":
		resolutionState = "partial"
	case strings.Contains(rawVersion, "$"):
		resolved, unresolved := resolveInterpolations(rawVersion, properties)
		if len(unresolved) > 0 {
			resolutionState = "unresolved"
			unresolvedKeys = unresolved
			value = rawVersion
		} else {
			value = resolved
		}
	}

	row := map[string]any{
		"name":                        group + ":" + artifact,
		"line_number":                 lineNumber,
		"value":                       value,
		"section":                     section,
		"config_kind":                 "dependency",
		"package_manager":             "gradle",
		"lang":                        "gradle",
		"dependency_scope":            scope,
		"dependency_resolution_state": resolutionState,
		"direct_dependency":           true,
		"dependency_path_kind":        "manifest",
		"dependency_optional":         false,
	}
	if len(unresolvedKeys) > 0 {
		row["dependency_unresolved_keys"] = unresolvedKeys
	}
	return row, true
}

func configurationScope(configuration string) string {
	switch configuration {
	case "testImplementation", "testApi", "testCompileOnly", "testRuntimeOnly", "androidTestImplementation":
		return "test"
	case "compileOnly", "compileOnlyApi", "providedCompile":
		return "provided"
	case "runtimeOnly", "runtime", "providedRuntime":
		return "runtime"
	case "annotationProcessor", "kapt", "ksp":
		return "annotationProcessor"
	case "classpath":
		return "classpath"
	default:
		return "compile"
	}
}

func resolveInterpolations(raw string, properties map[string]string) (string, []string) {
	unresolved := make([]string, 0)
	seen := make(map[string]struct{})

	record := func(key string) {
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		unresolved = append(unresolved, key)
	}

	resolved := interpolationCurly.ReplaceAllStringFunc(raw, func(match string) string {
		expr := strings.TrimSpace(match[2 : len(match)-1])
		key := expr
		if dot := strings.LastIndex(expr, "."); dot >= 0 {
			key = expr[dot+1:]
		}
		if value, ok := properties[key]; ok && value != "" {
			return value
		}
		record(expr)
		return match
	})
	resolved = interpolationBare.ReplaceAllStringFunc(resolved, func(match string) string {
		key := match[1:]
		if value, ok := properties[key]; ok && value != "" {
			return value
		}
		record(key)
		return match
	})
	sort.Strings(unresolved)
	return resolved, unresolved
}
