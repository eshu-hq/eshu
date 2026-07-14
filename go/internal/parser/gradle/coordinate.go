// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"regexp"
	"strings"
	"sync"
)

type coordinate struct {
	group    string
	artifact string
	version  string
}

var (
	coordinatePattern = regexp.MustCompile(`^([^:\s'"]+):([^:\s'"]+)(?::([^\s'"]+))?$`)
	mapFormIndicator  = regexp.MustCompile(`\b(?:group|name)\s*[:=]\s*['"]`)
)

// extractCoordinate pulls a Maven coordinate from a dependency declaration
// payload. It first checks for map-form (`group:`, `name:`, `version:`) to
// avoid mis-treating a `group: 'foo'` value as a coordinate string; then
// falls back to the first quoted string literal, and finally back to map
// form as a last resort.
func extractCoordinate(value string) (coordinate, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return coordinate{}, false
	}
	if looksLikeMapForm(value) {
		if mapped, ok := parseMapCoordinate(value); ok {
			return mapped, true
		}
	}
	if first, ok := firstStringLiteral(value); ok {
		if coord, ok := parseCoordinateLiteral(first); ok {
			return coord, true
		}
	}
	if mapped, ok := parseMapCoordinate(value); ok {
		return mapped, true
	}
	return coordinate{}, false
}

func looksLikeMapForm(value string) bool {
	return mapFormIndicator.MatchString(value)
}

func firstStringLiteral(value string) (string, bool) {
	inString := false
	var stringQuote byte
	start := -1
	for index := 0; index < len(value); index++ {
		current := value[index]
		if !inString {
			if current == '\'' || current == '"' {
				inString = true
				stringQuote = current
				start = index + 1
			}
			continue
		}
		if current == '\\' && index+1 < len(value) {
			index++
			continue
		}
		if current == stringQuote {
			return value[start:index], true
		}
	}
	return "", false
}

func parseCoordinateLiteral(literal string) (coordinate, bool) {
	literal = strings.TrimSpace(literal)
	parts := strings.SplitN(literal, ":", 3)
	if len(parts) < 2 {
		return coordinate{}, false
	}
	group := strings.TrimSpace(parts[0])
	artifact := strings.TrimSpace(parts[1])
	if group == "" || artifact == "" {
		return coordinate{}, false
	}
	if !coordinatePattern.MatchString(literal) {
		if len(parts) == 2 {
			return coordinate{group: group, artifact: artifact}, true
		}
		return coordinate{}, false
	}
	version := ""
	if len(parts) == 3 {
		version = strings.TrimSpace(parts[2])
	}
	return coordinate{group: group, artifact: artifact, version: version}, true
}

func parseMapCoordinate(value string) (coordinate, bool) {
	groupValue := extractMapEntry(value, "group")
	nameValue := extractMapEntry(value, "name")
	versionValue := extractMapEntry(value, "version")
	if groupValue == "" || nameValue == "" {
		return coordinate{}, false
	}
	return coordinate{group: groupValue, artifact: nameValue, version: versionValue}, true
}

var mapEntryPatterns = []string{
	`(?m)\b%s\s*:\s*(?:['"])([^'"]+)['"]`,
	`(?m)\b%s\s*=\s*(?:['"])([^'"]+)['"]`,
}

// mapEntryPatternCache caches the compiled mapEntryPatterns regexes per key so
// repeated extractMapEntry calls for the same key (parseMapCoordinate always
// calls it with "group", "name", and "version") reuse the compiled
// *regexp.Regexp instead of recompiling on every call. The key set is small
// and effectively fixed in this codebase's callers, but the cache stays
// correct for any key: a *regexp.Regexp is safe for concurrent use, and
// sync.Map.LoadOrStore makes first-compile-per-key race-safe.
var mapEntryPatternCache sync.Map // key -> []*regexp.Regexp

func mapEntryRegexesFor(key string) []*regexp.Regexp {
	if cached, ok := mapEntryPatternCache.Load(key); ok {
		return cached.([]*regexp.Regexp)
	}
	compiled := make([]*regexp.Regexp, 0, len(mapEntryPatterns))
	for _, template := range mapEntryPatterns {
		compiled = append(compiled, regexp.MustCompile(strings.Replace(template, "%s", regexp.QuoteMeta(key), 1)))
	}
	actual, _ := mapEntryPatternCache.LoadOrStore(key, compiled)
	return actual.([]*regexp.Regexp)
}

func extractMapEntry(value, key string) string {
	for _, expr := range mapEntryRegexesFor(key) {
		match := expr.FindStringSubmatch(value)
		if len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

// kotlinHelperCoordinate synthesizes an `org.jetbrains.kotlin:kotlin-<name>`
// coordinate from a `kotlin("name", "version")` Kotlin DSL helper call.
func kotlinHelperCoordinate(args string) (coordinate, bool) {
	args = strings.TrimSpace(args)
	literals := splitCallArguments(args)
	if len(literals) == 0 {
		return coordinate{}, false
	}
	name := stringLiteralValue(literals[0])
	if name == "" {
		return coordinate{}, false
	}
	version := ""
	if len(literals) >= 2 {
		version = stringLiteralValue(literals[1])
	}
	return coordinate{group: "org.jetbrains.kotlin", artifact: "kotlin-" + name, version: version}, true
}

func splitCallArguments(value string) []string {
	args := make([]string, 0)
	depth := 0
	inString := false
	var stringQuote byte
	start := 0
	for index := 0; index < len(value); index++ {
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
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}
	if start < len(value) {
		args = append(args, strings.TrimSpace(value[start:]))
	}
	return args
}

func stringLiteralValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return ""
	}
	if (value[0] != '\'' && value[0] != '"') || value[0] != value[len(value)-1] {
		return ""
	}
	return value[1 : len(value)-1]
}
