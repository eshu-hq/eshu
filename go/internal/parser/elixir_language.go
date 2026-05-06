package parser

import (
	"regexp"
	"slices"
	"strings"
)

var (
	elixirModulePattern    = regexp.MustCompile(`^\s*(defmodule|defprotocol|defimpl)\s+(.+)$`)
	elixirFunctionPattern  = regexp.MustCompile(`^\s*(def|defp|defmacro|defmacrop|defdelegate|defguard|defguardp)\s+(.+)$`)
	elixirImportPattern    = regexp.MustCompile(`^\s*(use|import|alias|require)\s+(.+)$`)
	elixirAttributePattern = regexp.MustCompile(`^\s*(@[a-z_]\w*)\s+(.+)$`)
	elixirScopedCall       = regexp.MustCompile(`([A-Z][A-Za-z0-9_.]+)\.([a-z_]\w*[!?]?)\(`)
	elixirCallPattern      = regexp.MustCompile(`\b([a-z_]\w*[!?]?)\(`)
)

func (e *Engine) parseElixir(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "elixir", isDependency)
	payload["modules"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}
	lines := strings.Split(string(source), "\n")
	seenCalls := make(map[string]struct{})
	scopes := make([]elixirScope, 0)
	lastMeaningfulLine := ""

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}

		if trimmed == "end" {
			var popped elixirScope
			scopes, popped = popElixirScope(scopes)
			if options.IndexSource && popped.item != nil {
				popped.item["end_line"] = lineNumber
				popped.item["source"] = strings.Join(lines[popped.lineNumber-1:lineNumber], "\n")
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if keyword, name, tail, ok := parseElixirModuleLine(trimmed); ok {
			item := map[string]any{
				"name":          name,
				"line_number":   lineNumber,
				"end_line":      lineNumber,
				"lang":          "elixir",
				"is_dependency": isDependency,
				"type":          keyword,
				"module_kind":   elixirModuleKind(keyword),
			}
			if keyword == "defimpl" {
				item["protocol"] = name
				if implementedFor := parseElixirDefImplTarget(tail); implementedFor != "" {
					item["implemented_for"] = implementedFor
				}
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			if keyword == "defprotocol" {
				appendBucket(payload, "protocols", item)
			} else {
				appendBucket(payload, "modules", item)
			}
			if elixirLineOpensBlock(keyword, trimmed) {
				scopes = append(scopes, elixirScope{
					kind:       "module",
					name:       name,
					lineNumber: lineNumber,
					item:       item,
				})
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if keyword, name, args, openBlock, ok := parseElixirFunctionLine(trimmed); ok {
			item := map[string]any{
				"name":          name,
				"line_number":   lineNumber,
				"end_line":      lineNumber,
				"args":          args,
				"lang":          "elixir",
				"is_dependency": isDependency,
				"visibility":    "public",
				"type":          keyword,
				"decorators":    []string{},
				"semantic_kind": elixirFunctionSemanticKind(keyword),
			}
			if strings.HasSuffix(keyword, "p") {
				item["visibility"] = "private"
			}
			if moduleName, moduleLine := currentElixirModule(scopes); moduleName != "" {
				item["context"] = []any{moduleName, "module", moduleLine}
				item["context_type"] = "module"
				item["class_context"] = moduleName
			}
			if options.IndexSource {
				item["source"] = rawLine
				if docstring := elixirDocstringFromPreviousLine(lastMeaningfulLine); docstring != "" {
					item["docstring"] = docstring
				}
			}
			appendBucket(payload, "functions", item)
			if keyword == "defguard" || keyword == "defguardp" {
				guardExpression := trimmed
				if whenIndex := strings.Index(guardExpression, " when "); whenIndex >= 0 {
					guardExpression = guardExpression[whenIndex+len(" when "):]
				}
				currentContextName, currentContextType, currentContextLine := currentElixirContext(scopes)
				currentModuleName, _ := currentElixirModule(scopes)
				for _, match := range elixirCallPattern.FindAllStringSubmatchIndex(guardExpression, -1) {
					if len(match) < 4 {
						continue
					}
					name := guardExpression[match[2]:match[3]]
					if name == item["name"] {
						continue
					}
					args := elixirCallArgs(guardExpression, match[1]-1)
					appendUniqueElixirCall(
						payload,
						seenCalls,
						name,
						name,
						"",
						args,
						lineNumber,
						currentContextName,
						currentContextType,
						currentContextLine,
						currentModuleName,
						isDependency,
					)
				}
			}
			if openBlock {
				scopes = append(scopes, elixirScope{
					kind:       "function",
					name:       name,
					lineNumber: lineNumber,
					item:       item,
				})
			} else if options.IndexSource {
				item["source"] = rawLine
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if keyword, paths, ok := parseElixirImportLine(trimmed); ok {
			for _, path := range paths {
				aliasName := any(nil)
				if keyword == "alias" && len(path) > 0 {
					aliasName = lastAliasSegment(path)
				}
				appendBucket(payload, "imports", map[string]any{
					"name":             path,
					"full_import_name": keyword + " " + path,
					"line_number":      lineNumber,
					"alias":            aliasName,
					"lang":             "elixir",
					"is_dependency":    isDependency,
					"import_type":      keyword,
				})
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if matches := elixirAttributePattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			attributeName := strings.TrimSpace(matches[1])
			if attributeName != "@doc" && attributeName != "@moduledoc" {
				item := map[string]any{
					"name":           attributeName,
					"line_number":    lineNumber,
					"end_line":       lineNumber,
					"lang":           "elixir",
					"is_dependency":  isDependency,
					"value":          strings.TrimSpace(matches[2]),
					"attribute_kind": "module_attribute",
				}
				if moduleName, moduleLine := currentElixirModule(scopes); moduleName != "" {
					item["context"] = []any{moduleName, "module", moduleLine}
					item["context_type"] = "module"
					item["class_context"] = moduleName
				}
				if options.IndexSource {
					item["source"] = rawLine
				}
				appendBucket(payload, "variables", item)
				lastMeaningfulLine = trimmed
				continue
			}
		}

		if strings.HasPrefix(trimmed, "#") || isElixirDefinitionLine(trimmed) {
			lastMeaningfulLine = trimmed
			continue
		}

		currentContextName, currentContextType, currentContextLine := currentElixirContext(scopes)
		currentModuleName, _ := currentElixirModule(scopes)

		for _, match := range elixirScopedCall.FindAllStringSubmatchIndex(trimmed, -1) {
			if len(match) < 6 {
				continue
			}
			receiver := trimmed[match[2]:match[3]]
			name := trimmed[match[4]:match[5]]
			args := elixirCallArgs(trimmed, match[1]-1)
			appendUniqueElixirCall(
				payload,
				seenCalls,
				name,
				receiver+"."+name,
				receiver,
				args,
				lineNumber,
				currentContextName,
				currentContextType,
				currentContextLine,
				currentModuleName,
				isDependency,
			)
		}
		for _, match := range elixirCallPattern.FindAllStringSubmatchIndex(trimmed, -1) {
			if len(match) < 4 {
				continue
			}
			if match[0] > 0 && trimmed[match[0]-1] == '.' {
				continue
			}
			name := trimmed[match[2]:match[3]]
			switch name {
			case "def", "defp", "do", "fn":
				continue
			}
			args := elixirCallArgs(trimmed, match[1]-1)
			appendUniqueElixirCall(
				payload,
				seenCalls,
				name,
				name,
				"",
				args,
				lineNumber,
				currentContextName,
				currentContextType,
				currentContextLine,
				currentModuleName,
				isDependency,
			)
		}

		lastMeaningfulLine = trimmed
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "modules")
	sortNamedBucket(payload, "protocols")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")
	return payload, nil
}

func (e *Engine) preScanElixir(path string) ([]string, error) {
	payload, err := e.parseElixir(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "modules", "protocols")
	slices.Sort(names)
	return names, nil
}

type elixirScope struct {
	kind       string
	name       string
	lineNumber int
	item       map[string]any
}
