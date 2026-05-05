package parser

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	phpNamespacePattern          = regexp.MustCompile(`^\s*namespace\s+([^;]+);`)
	phpUsePattern                = regexp.MustCompile(`^\s*use\s+([^;]+);`)
	phpTypePattern               = regexp.MustCompile(`^\s*(?:abstract\s+|final\s+)?(class|interface|trait)\s+([A-Za-z_]\w*)(.*)$`)
	phpFunctionPattern           = regexp.MustCompile(`^\s*(?:public\s+|protected\s+|private\s+|static\s+|abstract\s+|final\s+|readonly\s+)*function\s+([A-Za-z_]\w*)\s*\(`)
	phpFunctionReturnPattern     = regexp.MustCompile(`\)\s*:\s*([^{;]+)`)
	phpVariablePattern           = regexp.MustCompile(`\$[A-Za-z_]\w*`)
	phpTypedVariablePattern      = regexp.MustCompile(`(?:(?:public|protected|private|readonly|static)\s+)*([?A-Za-z_\\][\w\\|?]*)\s+\$[A-Za-z_]\w*`)
	phpStaticPropertyCallPattern = regexp.MustCompile(`((?:[A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::\$[A-Za-z_]\w*(?:->\w+(?:\([^()]*\))?)*->\w+)\s*\(`)
	phpMethodCallPattern         = regexp.MustCompile(`((?:\((?:\$[A-Za-z_]\w*(?:->\w+(?:\([^()]*\))?)*|(?:[A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::[A-Za-z_]\w*\(\)|new\s+[A-Za-z_\\]\w*(?:\\[A-Za-z_]\w*)*\(\))\)|\$[A-Za-z_]\w*(?:->\w+(?:\([^()]*\))?)*|(?:[A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::[A-Za-z_]\w*\(\)|new\s+[A-Za-z_\\]\w*(?:\\[A-Za-z_]\w*)*\(\))(?:->\w+(?:\([^()]*\))?)*->\w+)\s*\(`)
	phpFunctionChainPattern      = regexp.MustCompile(`((?:\((?:[A-Za-z_]\w*\(\))\)|[A-Za-z_]\w*\(\))(?:->\w+(?:\([^()]*\))?)*->\w+)\s*\(`)
	phpStaticCallPattern         = regexp.MustCompile(`\b([A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::([A-Za-z_]\w*)\s*\(`)
	phpNewCallPattern            = regexp.MustCompile(`\bnew\s+([A-Za-z_\\]\w*(?:\\[A-Za-z_]\w*)*)\s*\(`)
	phpFunctionCallPattern       = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
	phpVariableTypePattern       = regexp.MustCompile(`\$\w+\s*=\s*new\s+([A-Za-z_\\]\w*(?:\\[A-Za-z_]\w*)*)\s*\(`)
)

type phpScopedContext struct {
	kind       string
	name       string
	braceDepth int
	lineNumber int
}

func currentPHPScopedName(stack []phpScopedContext, kinds ...string) string {
	for index := len(stack) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if stack[index].kind == kind {
				return stack[index].name
			}
		}
	}
	return ""
}

func popPHPCompletedScopes(stack []phpScopedContext, braceDepth int) []phpScopedContext {
	for len(stack) > 0 && braceDepth < stack[len(stack)-1].braceDepth {
		stack = stack[:len(stack)-1]
	}
	return stack
}

func (e *Engine) parsePHP(path string, isDependency bool, options Options) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "php", isDependency)
	payload["traits"] = []map[string]any{}
	payload["interfaces"] = []map[string]any{}

	lines := strings.Split(string(source), "\n")
	namespace := ""
	braceDepth := 0
	stack := make([]phpScopedContext, 0)
	var pendingFunction *phpScopedContext
	var pendingAnonymousClass *phpScopedContext
	var pendingTraitAdaptation *phpScopedContext
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})
	classPropertyTypes := make(map[string]map[string]string)
	classParentTypes := make(map[string]string)
	localVariableTypes := make(map[string]map[string]string)
	methodReturnTypes := make(map[string]map[string]string)
	functionReturnTypes := make(map[string]string)
	importAliases := make(map[string]string)

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if pendingFunction != nil && strings.Contains(rawLine, "{") {
			stack = append(stack, phpScopedContext{
				kind:       pendingFunction.kind,
				name:       pendingFunction.name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				lineNumber: pendingFunction.lineNumber,
			})
			pendingFunction = nil
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			braceDepth += braceDelta(rawLine)
			stack = popPHPCompletedScopes(stack, braceDepth)
			continue
		}

		if matches := phpNamespacePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			namespace = strings.TrimSpace(matches[1])
		}

		if matches := phpUsePattern.FindStringSubmatch(trimmed); len(matches) == 2 && currentPHPScopedName(stack, "class_declaration", "interface_declaration", "trait_declaration") == "" {
			for _, spec := range parsePHPImports(matches[1]) {
				if spec.importType == "use" && spec.alias != "" {
					importAliases[spec.alias] = normalizePHPTypeName(spec.name)
				}
				appendBucket(payload, "imports", map[string]any{
					"name":             spec.name,
					"full_import_name": trimmed,
					"line_number":      lineNumber,
					"alias":            spec.alias,
					"import_type":      spec.importType,
					"context":          []any{nil, nil},
					"lang":             "php",
					"is_dependency":    false,
				})
			}
		} else if contextName, contextKind, _ := currentPHPContext(stack); contextKind == "class_declaration" {
			if bases := parsePHPClassTraitUses(trimmed); len(bases) > 0 {
				appendPHPClassBases(payload, contextName, bases)
			}
			if strings.Contains(trimmed, "{") && strings.Contains(trimmed, "use ") {
				pendingTraitAdaptation = &phpScopedContext{
					kind:       "trait_adaptation",
					name:       contextName,
					braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
					lineNumber: lineNumber,
				}
			}
		}

		if anonymousTail, ok := parsePHPAnonymousClass(trimmed); ok {
			name := phpAnonymousClassName(lineNumber)
			bases := parsePHPBases("class", anonymousTail)
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "php",
			}
			if len(bases) > 0 {
				item["bases"] = bases
			}
			if strings.Contains(anonymousTail, "extends") && len(bases) > 0 {
				classParentTypes[name] = normalizePHPImportedTypeName(bases[0], importAliases)
			}
			appendBucket(payload, "classes", item)
			pendingAnonymousClass = &phpScopedContext{
				kind:       "class_declaration",
				name:       name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				lineNumber: lineNumber,
			}
		}

		if matches := phpTypePattern.FindStringSubmatch(trimmed); len(matches) == 4 {
			name := matches[2]
			bases := parsePHPBases(matches[1], matches[3])
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "php",
			}
			if len(bases) > 0 {
				item["bases"] = bases
			}
			switch matches[1] {
			case "class":
				if strings.Contains(matches[3], "extends") && len(bases) > 0 {
					classParentTypes[name] = normalizePHPImportedTypeName(bases[0], importAliases)
				}
				appendBucket(payload, "classes", item)
				stack = append(stack, phpScopedContext{kind: "class_declaration", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")), lineNumber: lineNumber})
			case "interface":
				appendBucket(payload, "interfaces", item)
				stack = append(stack, phpScopedContext{kind: "interface_declaration", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")), lineNumber: lineNumber})
			case "trait":
				appendBucket(payload, "traits", item)
				stack = append(stack, phpScopedContext{kind: "trait_declaration", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")), lineNumber: lineNumber})
			}
		}

		if pendingTraitAdaptation != nil && braceDepth >= pendingTraitAdaptation.braceDepth {
			if adaptations := parsePHPClassTraitAdaptations(trimmed); len(adaptations) > 0 {
				appendPHPClassTraitAdaptations(payload, pendingTraitAdaptation.name, adaptations)
			}
		}

		if matches := phpFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := matches[1]
			functionKind := "function_definition"
			if currentPHPScopedName(stack, "class_declaration", "interface_declaration", "trait_declaration") != "" {
				functionKind = "method_declaration"
			}
			returnType := extractPHPReturnType(lines, index, rawLine)
			item := map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "php",
				"decorators":  []string{},
				"parameters":  extractPHPParameters(lines, index, rawLine),
			}
			if classContext := currentPHPScopedName(stack, "class_declaration", "interface_declaration", "trait_declaration"); classContext != "" {
				item["class_context"] = classContext
				if semanticKind := phpSemanticKindForMethod(name); semanticKind != "" {
					item["semantic_kind"] = semanticKind
				}
				if returnType != "" {
					if _, ok := methodReturnTypes[classContext]; !ok {
						methodReturnTypes[classContext] = make(map[string]string)
					}
					methodReturnTypes[classContext][name] = returnType
				}
			} else if returnType != "" {
				functionReturnTypes[name] = returnType
			}
			if returnType != "" {
				item["return_type"] = returnType
			}
			if options.IndexSource {
				item["source"] = collectPHPBlockSource(lines, index)
			}
			appendBucket(payload, "functions", item)
			if strings.Contains(rawLine, "{") {
				stack = append(stack, phpScopedContext{kind: functionKind, name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")), lineNumber: lineNumber})
			} else {
				pendingFunction = &phpScopedContext{kind: functionKind, name: name, lineNumber: lineNumber}
			}
		}

		contextName, contextKind, contextLine := currentPHPContext(stack)
		currentClassContext := currentPHPScopedName(stack, "class_declaration", "interface_declaration", "trait_declaration")
		functionScopeKey := currentPHPFunctionScopeKey(stack)
		if functionScopeKey != "" {
			if _, ok := localVariableTypes[functionScopeKey]; !ok {
				localVariableTypes[functionScopeKey] = make(map[string]string)
			}
		}

		for _, variable := range phpVariablePattern.FindAllString(rawLine, -1) {
			if variable == "$this" {
				continue
			}
			variableType := inferPHPVariableType(
				rawLine,
				variable,
				lineNumber,
				currentClassContext,
				classParentTypes,
				classPropertyTypes,
				localVariableTypes[functionScopeKey],
				methodReturnTypes,
				functionReturnTypes,
				importAliases,
			)
			if contextKind == "class_declaration" {
				if _, ok := classPropertyTypes[contextName]; !ok {
					classPropertyTypes[contextName] = make(map[string]string)
				}
				classPropertyTypes[contextName][strings.TrimPrefix(variable, "$")] = variableType
			}
			if functionScopeKey != "" && variableType != "" && variableType != "mixed" {
				localVariableTypes[functionScopeKey][strings.TrimPrefix(variable, "$")] = variableType
			}
			if _, ok := seenVariables[variable]; ok {
				continue
			}
			seenVariables[variable] = struct{}{}
			item := map[string]any{
				"name":        variable,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "php",
				"type":        variableType,
			}
			if contextName != "" {
				item["context"] = contextName
			}
			switch contextKind {
			case "class_declaration", "interface_declaration", "trait_declaration":
				item["class_context"] = contextName
			default:
				item["class_context"] = nil
			}
			appendBucket(payload, "variables", item)
		}

		normalizedTrimmed := strings.ReplaceAll(trimmed, "?->", "->")
		normalizedRawLine := strings.ReplaceAll(rawLine, "?->", "->")
		for _, match := range phpStaticPropertyCallPattern.FindAllStringSubmatch(normalizedTrimmed, -1) {
			if len(match) != 2 {
				continue
			}
			callName := lastPathSegment(match[1], "->")
			fullName := normalizePHPMethodCall(match[1])
			inferredObjType := inferPHPMethodReceiverType(
				match[1],
				currentClassContext,
				classParentTypes,
				classPropertyTypes,
				localVariableTypes[functionScopeKey],
				methodReturnTypes,
				functionReturnTypes,
				importAliases,
			)
			appendUniquePHPCall(payload, seenCalls, callName, fullName, lineNumber, extractPHPCallArgs(lines, index, normalizedRawLine, match[0]), contextName, contextKind, contextLine, inferredObjType)
		}
		for _, match := range phpMethodCallPattern.FindAllStringSubmatch(normalizedTrimmed, -1) {
			if len(match) != 2 {
				continue
			}
			callName := lastPathSegment(match[1], "->")
			fullName := normalizePHPMethodCall(match[1])
			inferredObjType := inferPHPMethodReceiverType(
				match[1],
				currentClassContext,
				classParentTypes,
				classPropertyTypes,
				localVariableTypes[functionScopeKey],
				methodReturnTypes,
				functionReturnTypes,
				importAliases,
			)
			appendUniquePHPCall(payload, seenCalls, callName, fullName, lineNumber, extractPHPCallArgs(lines, index, normalizedRawLine, match[0]), contextName, contextKind, contextLine, inferredObjType)
		}
		for _, matchIndexes := range phpFunctionChainPattern.FindAllStringSubmatchIndex(normalizedTrimmed, -1) {
			if len(matchIndexes) != 4 {
				continue
			}
			matchStart := matchIndexes[2]
			matchEnd := matchIndexes[3]
			if hasPHPReceiverChainPrefix(normalizedTrimmed, matchStart) {
				continue
			}
			match := normalizedTrimmed[matchStart:matchEnd]
			callName := lastPathSegment(match, "->")
			fullName := normalizePHPMethodCall(match)
			inferredObjType := inferPHPMethodReceiverType(
				match,
				currentClassContext,
				classParentTypes,
				classPropertyTypes,
				localVariableTypes[functionScopeKey],
				methodReturnTypes,
				functionReturnTypes,
				importAliases,
			)
			appendUniquePHPCall(payload, seenCalls, callName, fullName, lineNumber, extractPHPCallArgs(lines, index, normalizedRawLine, match), contextName, contextKind, contextLine, inferredObjType)
		}
		for _, match := range phpStaticCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 3 {
				continue
			}
			receiver := normalizePHPStaticReceiver(match[1], currentClassContext, classParentTypes, importAliases)
			if receiver == "" {
				continue
			}
			methodName := strings.TrimSpace(match[2])
			fullName := receiver + "." + methodName
			appendUniquePHPCall(payload, seenCalls, methodName, fullName, lineNumber, extractPHPCallArgs(lines, index, rawLine, match[0]), contextName, contextKind, contextLine, receiver)
		}
		for _, match := range phpNewCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			className := lastPathSegment(match[1], `\`)
			appendUniquePHPCall(payload, seenCalls, className, className, lineNumber, extractPHPCallArgs(lines, index, rawLine, match[0]), contextName, contextKind, contextLine, normalizePHPImportedTypeName(className, importAliases))
		}
		if !strings.Contains(trimmed, "->") && !strings.Contains(trimmed, "::") && !strings.Contains(trimmed, "new ") && !phpFunctionPattern.MatchString(trimmed) {
			for _, match := range phpFunctionCallPattern.FindAllStringSubmatch(trimmed, -1) {
				if len(match) != 2 {
					continue
				}
				name := match[1]
				switch name {
				case "function", "if", "foreach", "for", "switch", "echo", "require_once":
					continue
				}
				appendUniquePHPCall(payload, seenCalls, name, name, lineNumber, extractPHPCallArgs(lines, index, rawLine, match[0]), contextName, contextKind, contextLine, "")
			}
		}

		if pendingAnonymousClass != nil {
			stack = append(stack, *pendingAnonymousClass)
			pendingAnonymousClass = nil
		}

		braceDepth += braceDelta(rawLine)
		stack = popPHPCompletedScopes(stack, braceDepth)
		if pendingTraitAdaptation != nil && braceDepth < pendingTraitAdaptation.braceDepth {
			pendingTraitAdaptation = nil
		}
	}

	sortNamedBucket(payload, "functions")
	sortNamedBucket(payload, "classes")
	sortNamedBucket(payload, "traits")
	sortNamedBucket(payload, "interfaces")
	sortNamedBucket(payload, "variables")
	sortNamedBucket(payload, "imports")
	sortNamedBucket(payload, "function_calls")

	if namespace != "" {
		payload["namespace"] = namespace
	}

	return payload, nil
}

func (e *Engine) preScanPHP(path string) ([]string, error) {
	payload, err := e.parsePHP(path, false, Options{})
	if err != nil {
		return nil, err
	}
	names := collectBucketNames(payload, "functions", "classes", "traits", "interfaces")
	slices.Sort(names)
	return names, nil
}

func appendUniquePHPCall(
	payload map[string]any,
	seen map[string]struct{},
	name string,
	fullName string,
	lineNumber int,
	args []string,
	contextName string,
	contextKind string,
	contextLine int,
	inferredObjType string,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	seenKey := fullName + "#" + strconv.Itoa(lineNumber)
	if _, ok := seen[seenKey]; ok {
		return
	}
	seen[seenKey] = struct{}{}
	item := map[string]any{
		"name":          name,
		"full_name":     fullName,
		"line_number":   lineNumber,
		"args":          args,
		"context":       []any{contextName, contextKind, contextLine},
		"lang":          "php",
		"is_dependency": false,
	}
	if inferredObjType != "" {
		item["inferred_obj_type"] = inferredObjType
	} else {
		item["inferred_obj_type"] = nil
	}
	if contextKind == "class_declaration" || contextKind == "interface_declaration" || contextKind == "trait_declaration" {
		item["class_context"] = []any{contextName, contextLine}
	} else {
		item["class_context"] = []any{nil, nil}
	}
	appendBucket(payload, "function_calls", item)
}

func normalizePHPMethodCall(raw string) string {
	parts := strings.Split(normalizePHPParenthesizedReceiverExpression(raw), "->")
	if len(parts) <= 1 {
		return raw
	}
	return strings.Join(parts[:len(parts)-1], "->") + "." + parts[len(parts)-1]
}

func normalizePHPStaticReceiver(raw string, classContext string, classParentTypes map[string]string, importAliases map[string]string) string {
	receiver := strings.TrimSpace(raw)
	if receiver == "" {
		return ""
	}

	switch receiver {
	case "self", "static":
		if classContext != "" {
			return classContext
		}
	case "parent":
		if parentType := strings.TrimSpace(classParentTypes[classContext]); parentType != "" {
			return parentType
		}
		return ""
	}

	trimmed := strings.TrimPrefix(receiver, `\`)
	if importAliases != nil {
		if resolved := strings.TrimSpace(importAliases[trimmed]); resolved != "" {
			return normalizePHPImportedTypeName(resolved, importAliases)
		}
	}

	return trimmed
}

func hasPHPReceiverChainPrefix(raw string, start int) bool {
	if start < 2 || start > len(raw) {
		return false
	}
	prefix := raw[start-2 : start]
	return prefix == "->" || prefix == "::"
}
