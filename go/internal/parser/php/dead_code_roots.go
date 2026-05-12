package php

import (
	"regexp"
	"strings"
)

var (
	phpClassMethodArrayPattern = regexp.MustCompile(`\[\s*\\?([A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::class\s*,\s*['"]([A-Za-z_]\w*)['"]\s*\]`)
	phpWordPressHookPattern    = regexp.MustCompile(`\badd_(?:action|filter)\s*\([^,]+,\s*['"]([A-Za-z_]\w*)['"]`)
)

type phpDeadCodeFacts struct {
	typeKinds                 map[string]string
	typeBases                 map[string][]string
	interfaceMethods          map[string]map[phpMethodKey]struct{}
	routeMethodTargets        map[string]map[string]struct{}
	symfonyRouteAttributeLine map[int]struct{}
	wordpressFunctionTargets  map[string]struct{}
}

type phpMethodKey struct {
	name  string
	arity int
}

func collectPHPDeadCodeFacts(lines []string) phpDeadCodeFacts {
	facts := phpDeadCodeFacts{
		typeKinds:                 map[string]string{},
		typeBases:                 map[string][]string{},
		interfaceMethods:          map[string]map[phpMethodKey]struct{}{},
		routeMethodTargets:        map[string]map[string]struct{}{},
		symfonyRouteAttributeLine: map[int]struct{}{},
		wordpressFunctionTargets:  map[string]struct{}{},
	}
	braceDepth := 0
	stack := make([]phpScopedContext, 0)
	var pendingType *phpScopedContext
	var pendingFunction *phpScopedContext
	routeAttributePending := false
	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if pendingType != nil && strings.Contains(rawLine, "{") {
			stack = append(stack, phpScopedContext{
				kind:       pendingType.kind,
				name:       pendingType.name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				lineNumber: pendingType.lineNumber,
			})
			pendingType = nil
		}
		if pendingFunction != nil && strings.Contains(rawLine, "{") {
			stack = append(stack, phpScopedContext{
				kind:       pendingFunction.kind,
				name:       pendingFunction.name,
				braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				lineNumber: pendingFunction.lineNumber,
			})
			pendingFunction = nil
		}
		if matches := phpTypePattern.FindStringSubmatch(trimmed); len(matches) == 4 {
			name := strings.TrimSpace(matches[2])
			kind := phpTypeKindForDeclaration(matches[1])
			facts.typeKinds[name] = kind
			if bases := parsePHPBases(matches[1], matches[3]); len(bases) > 0 {
				facts.typeBases[name] = bases
			}
			context := phpScopedContext{kind: kind, name: name, lineNumber: lineNumber}
			if strings.Contains(rawLine, "{") {
				context.braceDepth = braceDepth + max(1, strings.Count(rawLine, "{"))
				stack = append(stack, context)
			} else {
				pendingType = &context
			}
		} else if contextName, contextKind, _ := currentPHPTypeContext(stack); contextKind == "class_declaration" {
			if bases := parsePHPClassTraitUses(trimmed); len(bases) > 0 {
				facts.typeBases[contextName] = dedupePHPNonEmptyStrings(append(facts.typeBases[contextName], bases...))
			}
		}
		if strings.HasPrefix(trimmed, "#[") && strings.Contains(trimmed, "Route(") {
			routeAttributePending = true
		}
		collectPHPLiteralRouteTargets(trimmed, facts)
		collectPHPWordPressHookTargets(trimmed, facts)
		if matches := phpFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			name := strings.TrimSpace(matches[1])
			contextName, contextKind, _ := currentPHPTypeContext(stack)
			methodKey := phpMethodKey{name: name, arity: len(extractPHPParameters(lines, index, rawLine))}
			if contextKind == "interface_declaration" {
				if facts.interfaceMethods[contextName] == nil {
					facts.interfaceMethods[contextName] = map[phpMethodKey]struct{}{}
				}
				facts.interfaceMethods[contextName][methodKey] = struct{}{}
			}
			if routeAttributePending {
				facts.symfonyRouteAttributeLine[lineNumber] = struct{}{}
				routeAttributePending = false
			}
			functionKind := "function_definition"
			if contextKind != "" {
				functionKind = "method_declaration"
			}
			if strings.Contains(rawLine, "{") {
				stack = append(stack, phpScopedContext{kind: functionKind, name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")), lineNumber: lineNumber})
			} else if !strings.Contains(rawLine, ";") {
				pendingFunction = &phpScopedContext{kind: functionKind, name: name, lineNumber: lineNumber}
			}
		}
		braceDepth += braceDelta(rawLine)
		stack = popPHPCompletedScopes(stack, braceDepth)
	}
	return facts
}

func phpTypeKindForDeclaration(kind string) string {
	switch kind {
	case "class":
		return "class_declaration"
	case "interface":
		return "interface_declaration"
	case "trait":
		return "trait_declaration"
	default:
		return ""
	}
}

func collectPHPLiteralRouteTargets(line string, facts phpDeadCodeFacts) {
	for _, match := range phpClassMethodArrayPattern.FindAllStringSubmatch(line, -1) {
		if len(match) != 3 {
			continue
		}
		className := normalizePHPTypeName(match[1])
		methodName := strings.TrimSpace(match[2])
		if className == "" || methodName == "" {
			continue
		}
		if facts.routeMethodTargets[className] == nil {
			facts.routeMethodTargets[className] = map[string]struct{}{}
		}
		facts.routeMethodTargets[className][methodName] = struct{}{}
	}
}

func collectPHPWordPressHookTargets(line string, facts phpDeadCodeFacts) {
	for _, match := range phpWordPressHookPattern.FindAllStringSubmatch(line, -1) {
		if len(match) != 2 {
			continue
		}
		if functionName := strings.TrimSpace(match[1]); functionName != "" {
			facts.wordpressFunctionTargets[functionName] = struct{}{}
		}
	}
}

func currentPHPTypeContext(stack []phpScopedContext) (string, string, int) {
	for index := len(stack) - 1; index >= 0; index-- {
		switch stack[index].kind {
		case "class_declaration", "interface_declaration", "trait_declaration":
			return stack[index].name, stack[index].kind, stack[index].lineNumber
		}
	}
	return "", "", 0
}

func phpDeadCodeRootKinds(
	name string,
	contextName string,
	contextKind string,
	lineNumber int,
	parameters []string,
	rawLine string,
	facts phpDeadCodeFacts,
) []string {
	var rootKinds []string
	methodKey := phpMethodKey{name: name, arity: len(parameters)}
	if contextKind == "" && name == "main" {
		rootKinds = append(rootKinds, "php.script_entrypoint")
	}
	if _, ok := facts.wordpressFunctionTargets[name]; ok && contextKind == "" {
		rootKinds = append(rootKinds, "php.wordpress_hook_callback")
	}
	if contextKind == "interface_declaration" {
		rootKinds = append(rootKinds, "php.interface_method")
	}
	if contextKind == "trait_declaration" {
		rootKinds = append(rootKinds, "php.trait_method")
	}
	if contextKind == "class_declaration" {
		_, routeBacked := facts.routeMethodTargets[contextName][name]
		_, attributeBacked := facts.symfonyRouteAttributeLine[lineNumber]
		if name == "__construct" {
			rootKinds = append(rootKinds, "php.constructor")
		}
		if strings.HasPrefix(name, "__") {
			rootKinds = append(rootKinds, "php.magic_method")
		}
		if phpClassImplementsInterfaceMethod(contextName, methodKey, facts) {
			rootKinds = append(rootKinds, "php.interface_implementation_method")
		}
		if phpIsControllerAction(contextName, name, rawLine, routeBacked || attributeBacked) {
			rootKinds = append(rootKinds, "php.framework_controller_action")
		}
		if routeBacked {
			rootKinds = append(rootKinds, "php.route_handler")
		}
		if attributeBacked {
			rootKinds = append(rootKinds, "php.symfony_route_attribute")
		}
	}
	return dedupePHPNonEmptyStrings(rootKinds)
}

func phpClassImplementsInterfaceMethod(className string, methodKey phpMethodKey, facts phpDeadCodeFacts) bool {
	for _, base := range facts.typeBases[className] {
		if facts.typeKinds[base] != "interface_declaration" {
			continue
		}
		if _, ok := facts.interfaceMethods[base][methodKey]; ok {
			return true
		}
	}
	return false
}

func phpIsControllerAction(contextName string, name string, rawLine string, routeBacked bool) bool {
	if !strings.HasSuffix(contextName, "Controller") || strings.HasPrefix(name, "__") {
		return false
	}
	if !routeBacked && !strings.HasSuffix(name, "Action") {
		return false
	}
	return phpMethodLineIsPublic(rawLine)
}

func phpMethodLineIsPublic(rawLine string) bool {
	fields := strings.Fields(strings.TrimSpace(rawLine))
	for _, field := range fields {
		switch field {
		case "private", "protected":
			return false
		case "public":
			return true
		case "function":
			return true
		}
	}
	return false
}
