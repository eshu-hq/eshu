package parser

import (
	"regexp"
	"slices"
	"strings"
)

var (
	perlPackagePattern  = regexp.MustCompile(`^\s*package\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)\s*;`)
	perlUsePattern      = regexp.MustCompile(`^\s*use\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	perlSubPattern      = regexp.MustCompile(`^\s*sub\s+([A-Za-z_]\w*)`)
	perlVariablePattern = regexp.MustCompile(`\b(?:my|our)\s+[@$%]?([A-Za-z_]\w*)`)
	perlCallPattern     = regexp.MustCompile(`([A-Za-z_:]+::[A-Za-z_]\w*|[A-Za-z_]\w*)\s*\(`)

	haskellModulePattern   = regexp.MustCompile(`^\s*module\s+([A-Za-z0-9_.']+)\s+where`)
	haskellImportPattern   = regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_.']+)`)
	haskellFunctionPattern = regexp.MustCompile(`^\s*([a-zA-Z_][A-Za-z0-9_']*)\b.*=`)
	haskellDataPattern     = regexp.MustCompile(`^\s*data\s+([A-Z][A-Za-z0-9_']*)`)
	haskellClassPattern    = regexp.MustCompile(`^\s*class\s+([A-Z][A-Za-z0-9_']*)\b`)
	haskellVariablePattern = regexp.MustCompile(`^\s*([a-z][A-Za-z0-9_']*)\s*(?:$|=)`)
)

func (e *Engine) parsePerl(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "perl", isDependency)
	lines := strings.Split(string(source), "\n")
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if matches := perlPackagePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "classes", map[string]any{
				"name":        lastPathSegment(matches[1], "::"),
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			})
		}
		if matches := perlUsePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "perl",
			})
		}
		if matches := perlSubPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			item := map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
				"decorators":  []string{},
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			appendBucket(payload, "functions", item)
		}
		for _, match := range perlVariablePattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			name := match[1]
			if _, ok := seenVariables[name]; ok {
				continue
			}
			seenVariables[name] = struct{}{}
			appendBucket(payload, "variables", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			})
		}
		for _, match := range perlCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			appendUniqueRegexCall(payload, seenCalls, match[1], lineNumber, "perl")
		}
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	return payload, nil
}

func (e *Engine) preScanPerl(path string) ([]string, error) {
	payload, err := e.parsePerl(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}

func (e *Engine) parseHaskell(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "haskell", isDependency)
	payload["modules"] = []map[string]any{}
	lines := strings.Split(string(source), "\n")
	seenFunctions := make(map[string]struct{})
	seenVariables := make(map[string]struct{})
	inWhereBlock := false

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		if matches := haskellModulePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "modules", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellImportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellDataPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "classes", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellClassPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			appendBucket(payload, "classes", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			if _, ok := seenFunctions[name]; !ok && name != "where" {
				seenFunctions[name] = struct{}{}
				item := map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "haskell",
					"decorators":  []string{},
				}
				if options.IndexSource {
					item["source"] = rawLine
				}
				appendBucket(payload, "functions", item)
			}
		}
		if strings.HasSuffix(trimmed, "where") || trimmed == "where" {
			inWhereBlock = true
			continue
		}
		if inWhereBlock {
			if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") {
				inWhereBlock = false
			} else if matches := haskellVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
				name := matches[1]
				if _, ok := seenVariables[name]; !ok {
					seenVariables[name] = struct{}{}
					appendBucket(payload, "variables", map[string]any{
						"name":        name,
						"line_number": lineNumber,
						"end_line":    lineNumber,
						"lang":        "haskell",
					})
				}
			}
		}
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "modules")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	return payload, nil
}

func (e *Engine) preScanHaskell(path string) ([]string, error) {
	payload, err := e.parseHaskell(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}
