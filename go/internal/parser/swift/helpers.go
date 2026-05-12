package swift

import (
	"regexp"
	"slices"
	"strings"
)

type swiftTypePattern struct {
	pattern *regexp.Regexp
	bucket  string
	kind    string
}

func swiftTypePatterns() []swiftTypePattern {
	return []swiftTypePattern{
		{pattern: classPattern, bucket: "classes", kind: "class"},
		{pattern: actorPattern, bucket: "classes", kind: "class"},
		{pattern: structPattern, bucket: "structs", kind: "struct"},
		{pattern: enumPattern, bucket: "enums", kind: "enum"},
		{pattern: protocolPattern, bucket: "protocols", kind: "protocol"},
	}
}

func parseInheritanceClause(matches []string, index int) []string {
	if len(matches) <= index {
		return nil
	}
	raw := strings.TrimSpace(matches[index])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	bases := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		bases = append(bases, trimmed)
	}
	if len(bases) == 0 {
		return nil
	}
	return bases
}

func extractParameters(source string) []string {
	start := strings.Index(source, "(")
	end := strings.LastIndex(source, ")")
	if start == -1 || end == -1 || end <= start+1 {
		return nil
	}
	signature := source[start+1 : end]
	rawParams := strings.Split(signature, ",")
	args := make([]string, 0, len(rawParams))
	for _, rawParam := range rawParams {
		param := strings.TrimSpace(rawParam)
		if param == "" {
			continue
		}
		beforeType := strings.SplitN(param, ":", 2)[0]
		tokens := strings.Fields(beforeType)
		if len(tokens) == 0 {
			continue
		}
		name := tokens[len(tokens)-1]
		if name == "_" && len(tokens) >= 2 {
			name = tokens[len(tokens)-2]
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "_" {
			continue
		}
		args = append(args, name)
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func extractCallArguments(source string, callName string) []string {
	index := strings.Index(source, callName)
	if index < 0 {
		return nil
	}
	open := strings.Index(source[index+len(callName):], "(")
	if open < 0 {
		return nil
	}
	open += index + len(callName)
	close := strings.LastIndex(source, ")")
	if close <= open {
		return nil
	}
	inside := strings.TrimSpace(source[open+1 : close])
	if inside == "" {
		return []string{}
	}
	parts := strings.Split(inside, ",")
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		arg := strings.TrimSpace(part)
		if arg != "" {
			args = append(args, arg)
		}
	}
	return args
}

func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}

func currentScopedName(stack []scopedContext, kinds ...string) string {
	for index := len(stack) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if stack[index].kind == kind {
				return stack[index].name
			}
		}
	}
	return ""
}

func currentScopedKind(stack []scopedContext, kinds ...string) string {
	for index := len(stack) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if stack[index].kind == kind {
				return stack[index].kind
			}
		}
	}
	return ""
}

func popCompletedScopes(stack []scopedContext, braceDepth int) []scopedContext {
	for len(stack) > 0 && braceDepth < stack[len(stack)-1].braceDepth {
		stack = stack[:len(stack)-1]
	}
	return stack
}

func swiftCodeLineAndAttributes(line string) (string, []string, bool) {
	codeLine := strings.TrimSpace(line)
	attributes := make([]string, 0, 2)
	for strings.HasPrefix(codeLine, "@") {
		name, remaining, ok := consumeSwiftAttribute(codeLine)
		if !ok {
			break
		}
		attributes = append(attributes, name)
		codeLine = strings.TrimSpace(remaining)
	}
	if len(attributes) == 0 {
		return codeLine, nil, false
	}
	if codeLine == "" || strings.HasPrefix(codeLine, "//") {
		return "", attributes, true
	}
	return codeLine, attributes, false
}

func consumeSwiftAttribute(line string) (string, string, bool) {
	if len(line) < 2 || line[0] != '@' || !isSwiftAttributeStart(line[1]) {
		return "", line, false
	}
	index := 2
	for index < len(line) && isSwiftAttributePart(line[index]) {
		index++
	}
	name := line[1:index]
	remaining := strings.TrimLeft(line[index:], " \t")
	if strings.HasPrefix(remaining, "(") {
		closeIndex, ok := swiftAttributeArgumentEnd(remaining)
		if !ok {
			return name, "", true
		}
		remaining = remaining[closeIndex:]
	}
	return name, remaining, true
}

func swiftAttributeArgumentEnd(source string) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	var quote byte
	for index := 0; index < len(source); index++ {
		character := source[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				inString = false
			}
			continue
		}
		switch character {
		case '"', '\'':
			inString = true
			quote = character
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index + 1, true
			}
		}
	}
	return len(source), false
}

func isSwiftAttributeStart(character byte) bool {
	return character == '_' ||
		(character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z')
}

func isSwiftAttributePart(character byte) bool {
	return isSwiftAttributeStart(character) ||
		(character >= '0' && character <= '9') ||
		character == '.'
}

func collectSwiftSemanticFacts(lines []string) swiftSemanticFacts {
	facts := swiftSemanticFacts{
		protocolMethods:    make(map[string]map[string]struct{}),
		typeConformances:   make(map[string]map[string]struct{}),
		vaporRouteHandlers: make(map[string]struct{}),
	}
	braceDepth := 0
	stack := make([]scopedContext, 0)
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}
		codeLine, _, onlyAttributes := swiftCodeLineAndAttributes(trimmed)
		if onlyAttributes {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}
		for _, match := range vaporRoutePattern.FindAllStringSubmatch(codeLine, -1) {
			if len(match) == 2 {
				facts.vaporRouteHandlers[match[1]] = struct{}{}
			}
		}
		for _, typed := range swiftTypePatterns() {
			if matches := typed.pattern.FindStringSubmatch(codeLine); len(matches) >= 2 {
				name := matches[1]
				facts.typeConformances[name] = swiftStringSet(parseInheritanceClause(matches, 2))
				stack = append(stack, scopedContext{
					kind:       typed.kind,
					name:       name,
					braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				})
				break
			}
		}
		if currentScopedKind(stack, "protocol") == "protocol" {
			if matches := functionPattern.FindStringSubmatch(codeLine); len(matches) == 2 {
				protocolName := currentScopedName(stack, "protocol")
				if facts.protocolMethods[protocolName] == nil {
					facts.protocolMethods[protocolName] = make(map[string]struct{})
				}
				facts.protocolMethods[protocolName][matches[1]] = struct{}{}
			}
		}
		braceDepth += braceDelta(rawLine)
		stack = popCompletedScopes(stack, braceDepth)
	}
	return facts
}

func swiftTypeDeadCodeRootKinds(kind string, bases []string, attributes []string) []string {
	rootKinds := make([]string, 0, 2)
	if kind == "protocol" {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.protocol_type")
	}
	if swiftHasAttribute(attributes, "main") {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.main_type")
	}
	for _, base := range bases {
		switch swiftShortTypeName(base) {
		case "App":
			rootKinds = appendSwiftRootKind(rootKinds, "swift.swiftui_app_type")
		case "UIApplicationDelegate":
			rootKinds = appendSwiftRootKind(rootKinds, "swift.ui_application_delegate_type")
		}
	}
	return rootKinds
}

func swiftVariableDeadCodeRootKinds(name string, varType string, contextName string, facts swiftSemanticFacts) []string {
	if name == "body" && strings.Contains(varType, "Scene") && swiftConformsTo(facts, contextName, "App") {
		return []string{"swift.swiftui_body"}
	}
	return nil
}

func swiftFunctionDeadCodeRootKinds(
	name string,
	source string,
	classContext string,
	scopeKind string,
	attributes []string,
	facts swiftSemanticFacts,
) []string {
	rootKinds := make([]string, 0, 3)
	if classContext == "" && name == "main" {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.main_function")
	}
	if name == "init" && classContext != "" && scopeKind != "protocol" {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.constructor")
	}
	if scopeKind == "protocol" {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.protocol_method")
	}
	if strings.Contains(source, "override func") {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.override_method")
	}
	if swiftImplementsProtocolMethod(facts, classContext, name) {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.protocol_implementation_method")
	}
	if swiftConformsTo(facts, classContext, "UIApplicationDelegate") && name == "application" {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.ui_application_delegate_method")
	}
	if _, ok := facts.vaporRouteHandlers[name]; ok {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.vapor_route_handler")
	}
	if swiftConformsTo(facts, classContext, "XCTestCase") && strings.HasPrefix(name, "test") {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.xctest_method")
	}
	if swiftHasAttribute(attributes, "Test") {
		rootKinds = appendSwiftRootKind(rootKinds, "swift.swift_testing_method")
	}
	return rootKinds
}

func swiftImplementsProtocolMethod(facts swiftSemanticFacts, typeName string, methodName string) bool {
	for protocolName := range facts.typeConformances[typeName] {
		methods := facts.protocolMethods[protocolName]
		if _, ok := methods[methodName]; ok {
			return true
		}
	}
	return false
}

func swiftConformsTo(facts swiftSemanticFacts, typeName string, candidates ...string) bool {
	conformances := facts.typeConformances[typeName]
	for _, candidate := range candidates {
		if _, ok := conformances[candidate]; ok {
			return true
		}
	}
	return false
}

func swiftStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		shortName := swiftShortTypeName(value)
		if shortName != "" {
			set[shortName] = struct{}{}
		}
	}
	return set
}

func swiftShortTypeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, "?")
	if index := strings.Index(name, "<"); index >= 0 {
		name = name[:index]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	return strings.TrimSpace(name)
}

func swiftHasAttribute(attributes []string, name string) bool {
	return slices.Contains(attributes, name)
}

func appendSwiftRootKind(rootKinds []string, rootKind string) []string {
	if slices.Contains(rootKinds, rootKind) {
		return rootKinds
	}
	return append(rootKinds, rootKind)
}
