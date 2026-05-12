package elixir

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	elixirModulePattern    = regexp.MustCompile(`^\s*(defmodule|defprotocol|defimpl)\s+(.+)$`)
	elixirFunctionPattern  = regexp.MustCompile(`^\s*(def|defp|defmacro|defmacrop|defdelegate|defguard|defguardp)\s+(.+)$`)
	elixirImportPattern    = regexp.MustCompile(`^\s*(use|import|alias|require)\s+(.+)$`)
	elixirAttributePattern = regexp.MustCompile(`^\s*(@[a-z_]\w*)\s+(.+)$`)
	elixirScopedCall       = regexp.MustCompile(`([A-Z][A-Za-z0-9_.]+)\.([a-z_]\w*[!?]?)\(`)
	elixirCallPattern      = regexp.MustCompile(`\b([a-z_]\w*[!?]?)\(`)
)

// Parse extracts Elixir modules, functions, imports, attributes, and calls.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "elixir", isDependency)
	payload["modules"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}
	lines := strings.Split(string(source), "\n")
	seenCalls := make(map[string]struct{})
	scopes := make([]scope, 0)
	lastMeaningfulLine := ""
	deadCodeFacts := newElixirDeadCodeFacts()
	pendingImpl := false

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}

		if trimmed == "end" {
			var popped scope
			scopes, popped = popScope(scopes)
			if options.IndexSource && popped.item != nil {
				popped.item["end_line"] = lineNumber
				popped.item["source"] = strings.Join(lines[popped.lineNumber-1:lineNumber], "\n")
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if keyword, name, tail, ok := parseModuleLine(trimmed); ok {
			item := map[string]any{
				"name":          name,
				"line_number":   lineNumber,
				"end_line":      lineNumber,
				"lang":          "elixir",
				"is_dependency": isDependency,
				"type":          keyword,
				"module_kind":   moduleKind(keyword),
			}
			if keyword == "defimpl" {
				item["protocol"] = name
				if implementedFor := parseDefImplTarget(tail); implementedFor != "" {
					item["implemented_for"] = implementedFor
				}
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			if keyword == "defprotocol" {
				shared.AppendBucket(payload, "protocols", item)
			} else {
				shared.AppendBucket(payload, "modules", item)
			}
			recordElixirModule(deadCodeFacts, name)
			if lineOpensBlock(keyword, trimmed) {
				scopes = append(scopes, scope{
					kind:       "module",
					name:       name,
					lineNumber: lineNumber,
					item:       item,
				})
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if keyword, name, args, openBlock, ok := parseFunctionLine(trimmed); ok {
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
				"semantic_kind": functionSemanticKind(keyword),
			}
			if strings.HasSuffix(keyword, "p") {
				item["visibility"] = "private"
			}
			if moduleName, moduleLine := currentModule(scopes); moduleName != "" {
				item["context"] = []any{moduleName, "module", moduleLine}
				item["context_type"] = "module"
				item["class_context"] = moduleName
				if rootKinds := elixirFunctionDeadCodeRootKinds(
					keyword,
					name,
					args,
					moduleName,
					elixirCurrentModuleKind(scopes),
					pendingImpl,
					deadCodeFacts,
				); len(rootKinds) > 0 {
					item["dead_code_root_kinds"] = rootKinds
				}
			} else if rootKinds := elixirFunctionDeadCodeRootKinds(
				keyword,
				name,
				args,
				"",
				"",
				pendingImpl,
				deadCodeFacts,
			); len(rootKinds) > 0 {
				item["dead_code_root_kinds"] = rootKinds
			}
			pendingImpl = false
			if options.IndexSource {
				item["source"] = rawLine
				if docstring := docstringFromPreviousLine(lastMeaningfulLine); docstring != "" {
					item["docstring"] = docstring
				}
			}
			markElixirObservedExactnessBlockersOnItem(item, trimmed)
			shared.AppendBucket(payload, "functions", item)
			if keyword == "defguard" || keyword == "defguardp" {
				appendGuardCalls(payload, seenCalls, trimmed, lineNumber, scopes, item["name"], isDependency)
			}
			if openBlock {
				scopes = append(scopes, scope{
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

		if keyword, paths, ok := parseImportLine(trimmed); ok {
			for _, path := range paths {
				aliasName := any(nil)
				if keyword == "alias" && len(path) > 0 {
					aliasName = lastAliasSegment(path)
				}
				shared.AppendBucket(payload, "imports", map[string]any{
					"name":             path,
					"full_import_name": keyword + " " + path,
					"line_number":      lineNumber,
					"alias":            aliasName,
					"lang":             "elixir",
					"is_dependency":    isDependency,
					"import_type":      keyword,
				})
			}
			if moduleName, _ := currentModule(scopes); moduleName != "" {
				recordElixirUse(deadCodeFacts, moduleName, trimmed, keyword, paths)
			}
			lastMeaningfulLine = trimmed
			continue
		}

		if matches := elixirAttributePattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			if matches[1] == "@impl" {
				pendingImpl = true
			}
			if matches[1] == "@behaviour" {
				if moduleName, _ := currentModule(scopes); moduleName != "" {
					recordElixirBehaviour(deadCodeFacts, moduleName, matches[2])
				}
			}
			if appendAttribute(payload, matches, rawLine, lineNumber, scopes, isDependency, options) {
				lastMeaningfulLine = trimmed
				continue
			}
		}

		if strings.HasPrefix(trimmed, "#") || isDefinitionLine(trimmed) {
			lastMeaningfulLine = trimmed
			continue
		}

		markElixirObservedExactnessBlockers(scopes, trimmed)
		appendLineCalls(payload, seenCalls, trimmed, lineNumber, scopes, isDependency)
		lastMeaningfulLine = trimmed
	}

	for _, bucket := range []string{"functions", "modules", "protocols", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}
	return payload, nil
}

// PreScan returns Elixir names used by the collector import-map pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "modules", "protocols")
	slices.Sort(names)
	return names, nil
}

func appendGuardCalls(
	payload map[string]any,
	seenCalls map[string]struct{},
	trimmed string,
	lineNumber int,
	scopes []scope,
	itemName any,
	isDependency bool,
) {
	guardExpression := trimmed
	if whenIndex := strings.Index(guardExpression, " when "); whenIndex >= 0 {
		guardExpression = guardExpression[whenIndex+len(" when "):]
	}
	currentContextName, currentContextType, currentContextLine := currentContext(scopes)
	currentModuleName, _ := currentModule(scopes)
	for _, match := range elixirCallPattern.FindAllStringSubmatchIndex(guardExpression, -1) {
		if len(match) < 4 {
			continue
		}
		name := guardExpression[match[2]:match[3]]
		if name == itemName {
			continue
		}
		args := callArgs(guardExpression, match[1]-1)
		appendUniqueCall(
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
