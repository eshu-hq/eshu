package parser

import (
	"regexp"
	"slices"
	"strings"
)

var (
	dartImportPattern    = regexp.MustCompile(`^\s*(?:import|export)\s+'([^']+)'`)
	dartClassPattern     = regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*)`)
	dartMixinPattern     = regexp.MustCompile(`^\s*mixin\s+([A-Za-z_]\w*)`)
	dartEnumPattern      = regexp.MustCompile(`^\s*enum\s+([A-Za-z_]\w*)`)
	dartExtensionPattern = regexp.MustCompile(`^\s*extension\s+([A-Za-z_]\w*)\s+on\b`)
	dartFunctionPattern  = regexp.MustCompile(`^\s*(?:static\s+)?(?:[\w<>\?\[\], ]+\s+)?([A-Za-z_]\w*)\s*\([^)]*\)\s*(?:async\*?|async|=>|\{)`)
	dartVariablePattern  = regexp.MustCompile(`^\s*(?:final|var|const)\s+(?:[\w<>\?\[\], ]+\s+)?([A-Za-z_]\w*)\s*=`)
	dartCallPattern      = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

func (e *Engine) parseDart(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "dart", isDependency)
	lines := strings.Split(string(source), "\n")
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		if matches := dartImportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "dart",
			})
		}
		for _, pattern := range []*regexp.Regexp{
			dartClassPattern, dartMixinPattern, dartEnumPattern, dartExtensionPattern,
		} {
			if matches := pattern.FindStringSubmatch(trimmed); len(matches) == 2 {
				appendBucket(payload, "classes", map[string]any{
					"name":        matches[1],
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "dart",
				})
			}
		}
		if matches := dartFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			switch name {
			case "if", "for", "while", "switch":
				continue
			}
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "dart",
				"decorators":  []string{},
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			appendBucket(payload, "functions", item)
		}
		if matches := dartVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			if _, ok := seenVariables[name]; !ok {
				seenVariables[name] = struct{}{}
				appendBucket(payload, "variables", map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "dart",
				})
			}
		}
		for _, match := range dartCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			name := match[1]
			switch name {
			case "if", "for", "while", "switch":
				continue
			}
			appendUniqueRegexCall(payload, seenCalls, name, lineNumber, "dart")
		}
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	return payload, nil
}

func (e *Engine) preScanDart(path string) ([]string, error) {
	payload, err := e.parseDart(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}

func appendUniqueRegexCall(
	payload map[string]any,
	seen map[string]struct{},
	fullName string,
	lineNumber int,
	lang string,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	appendBucket(payload, "function_calls", map[string]any{
		"name":        fullName,
		"full_name":   fullName,
		"line_number": lineNumber,
		"lang":        lang,
	})
}
