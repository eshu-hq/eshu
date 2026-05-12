package haskell

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	haskellModulePattern        = regexp.MustCompile(`^\s*module\s+([A-Za-z0-9_.']+)`)
	haskellFunctionPattern      = regexp.MustCompile(`^\s*([a-z_][A-Za-z0-9_']*)\b.*=`)
	haskellTypeDeclarationRegex = regexp.MustCompile(`^\s*(data|newtype|type)\s+(?:family\s+)?([A-Z][A-Za-z0-9_']*)`)
	haskellClassPattern         = regexp.MustCompile(`^\s*class\s+(?:\([^)]*\)\s*=>\s*)?([A-Z][A-Za-z0-9_']*)\b`)
	haskellInstancePattern      = regexp.MustCompile(`^\s*instance\s+(?:\([^)]*\)\s*=>\s*)?(.+?)\s+where\b`)
	haskellTypeSignaturePattern = regexp.MustCompile(`^\s*([a-z_][A-Za-z0-9_']*)\s*::`)
	haskellVariablePattern      = regexp.MustCompile(`^\s*([a-z][A-Za-z0-9_']*)\s*(?:$|=)`)
	haskellCallTokenPattern     = regexp.MustCompile(`(?:[A-Z][A-Za-z0-9_']*\.)+[a-z_][A-Za-z0-9_']*|[a-z_][A-Za-z0-9_']*`)
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
	explicitExports := make(map[string]struct{})
	seenFunctions := make(map[string]struct{})
	functionItems := make(map[string]map[string]any)
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	inWhereBlock := false
	currentClass := ""
	currentClassIndent := 0
	currentInstance := ""
	currentInstanceIndent := 0
	currentFunction := ""
	currentFunctionContext := ""
	currentFunctionIndent := 0
	currentFunctionParams := map[string]struct{}{}
	var currentFunctionItem map[string]any

	for index := 0; index < len(lines); index++ {
		rawLine := lines[index]
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		indent := haskellLeadingIndent(rawLine)

		if currentClass != "" && indent <= currentClassIndent {
			currentClass = ""
		}
		if currentInstance != "" && indent <= currentInstanceIndent {
			currentInstance = ""
		}
		if currentFunction != "" && indent <= currentFunctionIndent {
			currentFunction = ""
			currentFunctionContext = ""
			currentFunctionParams = map[string]struct{}{}
			currentFunctionItem = nil
		}

		if strings.HasPrefix(trimmed, "module ") {
			header, endIndex := haskellCollectModuleHeader(lines, index)
			index = endIndex
			if matches := haskellModulePattern.FindStringSubmatch(header); len(matches) == 2 {
				for name := range haskellParseModuleExports(header) {
					explicitExports[name] = struct{}{}
				}
				shared.AppendBucket(payload, "modules", map[string]any{
					"name":        matches[1],
					"line_number": lineNumber,
					"end_line":    endIndex + 1,
					"lang":        "haskell",
				})
			}
			continue
		}
		if name, alias, ok := haskellParseImport(trimmed); ok {
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"lang":        "haskell",
			}
			if alias != "" {
				item["alias"] = alias
			}
			shared.AppendBucket(payload, "imports", item)
		}
		if inWhereBlock {
			if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") {
				inWhereBlock = false
			} else {
				if currentFunctionItem != nil {
					currentFunctionItem["end_line"] = lineNumber
				}
				if matches := haskellVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
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
				if currentFunction != "" {
					haskellAppendFunctionCalls(
						payload,
						trimmed,
						lineNumber,
						currentFunction,
						currentFunctionContext,
						currentFunctionParams,
						seenCalls,
					)
				}
				continue
			}
		}
		if matches := haskellTypeDeclarationRegex.FindStringSubmatch(trimmed); len(matches) == 3 {
			item := map[string]any{
				"name":          matches[2],
				"line_number":   lineNumber,
				"end_line":      lineNumber,
				"lang":          "haskell",
				"semantic_kind": matches[1],
			}
			if haskellIsExplicitExport(explicitExports, matches[2]) {
				item["dead_code_root_kinds"] = []string{"haskell.exported_type"}
			}
			shared.AppendBucket(payload, "classes", item)
			continue
		}
		if matches := haskellClassPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			currentClass = matches[1]
			currentClassIndent = indent
			item := map[string]any{
				"name":          matches[1],
				"line_number":   lineNumber,
				"end_line":      lineNumber,
				"lang":          "haskell",
				"semantic_kind": "typeclass",
			}
			if haskellIsExplicitExport(explicitExports, matches[1]) {
				item["dead_code_root_kinds"] = []string{"haskell.exported_type"}
			}
			shared.AppendBucket(payload, "classes", item)
			continue
		}
		if matches := haskellInstancePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			currentInstance = strings.Join(strings.Fields(matches[1]), " ")
			currentInstanceIndent = indent
			continue
		}
		if currentFunction != "" && indent > currentFunctionIndent && !haskellFunctionPattern.MatchString(trimmed) {
			if currentFunctionItem != nil {
				currentFunctionItem["end_line"] = lineNumber
			}
			haskellAppendExpressionCalls(
				payload,
				trimmed,
				lineNumber,
				currentFunction,
				currentFunctionContext,
				currentFunctionParams,
				seenCalls,
			)
		}
		if currentClass != "" {
			if matches := haskellTypeSignaturePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
				name := matches[1]
				key := haskellFunctionKey(currentClass, name)
				if _, ok := seenFunctions[key]; !ok {
					seenFunctions[key] = struct{}{}
					item := map[string]any{
						"name":                 name,
						"line_number":          lineNumber,
						"end_line":             lineNumber,
						"lang":                 "haskell",
						"class_context":        currentClass,
						"decorators":           []string{},
						"dead_code_root_kinds": []string{"haskell.typeclass_method"},
					}
					functionItems[key] = item
					shared.AppendBucket(payload, "functions", item)
				}
				continue
			}
		}
		if matches := haskellFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			if haskellIsKeyword(name) {
				if currentFunction != "" && indent > currentFunctionIndent {
					if currentFunctionItem != nil {
						currentFunctionItem["end_line"] = lineNumber
					}
					haskellAppendFunctionCalls(
						payload,
						trimmed,
						lineNumber,
						currentFunction,
						currentFunctionContext,
						currentFunctionParams,
						seenCalls,
					)
				}
				continue
			}
			context, rootKinds := haskellFunctionContextAndRoots(name, currentClass, currentInstance, explicitExports)
			key := haskellFunctionKey(context, name)
			currentFunction = name
			currentFunctionContext = context
			currentFunctionIndent = indent
			currentFunctionParams = haskellFunctionParameters(trimmed[:strings.Index(trimmed, "=")])
			currentFunctionItem = functionItems[key]
			if _, ok := seenFunctions[key]; !ok {
				seenFunctions[key] = struct{}{}
				item := map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "haskell",
					"decorators":  []string{},
				}
				if context != "" {
					item["class_context"] = context
				}
				if len(rootKinds) > 0 {
					item["dead_code_root_kinds"] = rootKinds
				}
				if options.IndexSource {
					item["source"] = rawLine
				}
				functionItems[key] = item
				currentFunctionItem = item
				shared.AppendBucket(payload, "functions", item)
			} else if currentFunctionItem != nil {
				currentFunctionItem["end_line"] = lineNumber
			}
			haskellAppendFunctionCalls(payload, trimmed, lineNumber, name, context, currentFunctionParams, seenCalls)
		}
		if strings.HasSuffix(trimmed, "where") || trimmed == "where" {
			inWhereBlock = true
			continue
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
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
