package haskell

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	haskellModulePattern   = regexp.MustCompile(`^\s*module\s+([A-Za-z0-9_.']+)\s+where`)
	haskellImportPattern   = regexp.MustCompile(`^\s*import\s+([A-Za-z0-9_.']+)`)
	haskellFunctionPattern = regexp.MustCompile(`^\s*([a-zA-Z_][A-Za-z0-9_']*)\b.*=`)
	haskellDataPattern     = regexp.MustCompile(`^\s*data\s+([A-Z][A-Za-z0-9_']*)`)
	haskellClassPattern    = regexp.MustCompile(`^\s*class\s+([A-Z][A-Za-z0-9_']*)\b`)
	haskellVariablePattern = regexp.MustCompile(`^\s*([a-z][A-Za-z0-9_']*)\s*(?:$|=)`)
)

// Parse reads path and returns the legacy Haskell parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "haskell", isDependency)
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
			shared.AppendBucket(payload, "modules", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellImportPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellDataPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "classes", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "haskell",
			})
		}
		if matches := haskellClassPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "classes", map[string]any{
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
				shared.AppendBucket(payload, "functions", item)
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
					shared.AppendBucket(payload, "variables", map[string]any{
						"name":        name,
						"line_number": lineNumber,
						"end_line":    lineNumber,
						"lang":        "haskell",
					})
				}
			}
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	return payload, nil
}

// PreScan returns Haskell declaration names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}
