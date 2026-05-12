package php

import (
	"regexp"
	"strings"
)

var (
	phpClassMethodArrayPattern = regexp.MustCompile(`\[\s*\\?([A-Za-z_]\w*(?:\\[A-Za-z_]\w*)*)::class\s*,\s*['"]([A-Za-z_]\w*)['"]\s*\]`)
	phpWordPressHookPattern    = regexp.MustCompile(`\badd_(?:action|filter)\s*\([^,]+,\s*['"]([A-Za-z_]\w*)['"]`)
	phpSymfonyRoutePattern     = regexp.MustCompile(`^#\[\s*(?:\\?Route|\\?Symfony\\Component\\Routing\\(?:Annotation|Attribute)\\Route)\s*\(`)
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

type phpDeadCodeFunctionFact struct {
	item        map[string]any
	name        string
	contextName string
	contextKind string
	lineNumber  int
	parameters  []string
	rawLine     string
}

func newPHPDeadCodeFacts() phpDeadCodeFacts {
	return phpDeadCodeFacts{
		typeKinds:                 map[string]string{},
		typeBases:                 map[string][]string{},
		interfaceMethods:          map[string]map[phpMethodKey]struct{}{},
		routeMethodTargets:        map[string]map[string]struct{}{},
		symfonyRouteAttributeLine: map[int]struct{}{},
		wordpressFunctionTargets:  map[string]struct{}{},
	}
}

func recordPHPDeadCodeType(facts phpDeadCodeFacts, kind string, name string, bases []string) {
	if name == "" {
		return
	}
	facts.typeKinds[name] = kind
	if len(bases) > 0 {
		facts.typeBases[name] = dedupePHPNonEmptyStrings(append(facts.typeBases[name], bases...))
	}
}

func recordPHPDeadCodeTraitUses(facts phpDeadCodeFacts, className string, bases []string) {
	if className == "" || len(bases) == 0 {
		return
	}
	facts.typeBases[className] = dedupePHPNonEmptyStrings(append(facts.typeBases[className], bases...))
}

func observePHPDeadCodeStatement(line string, facts phpDeadCodeFacts, routeAttributePending *bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	if phpSymfonyRoutePattern.MatchString(trimmed) {
		*routeAttributePending = true
	}
	collectPHPLiteralRouteTargets(trimmed, facts)
	collectPHPWordPressHookTargets(trimmed, facts)
}

func recordPHPDeadCodeFunction(
	facts phpDeadCodeFacts,
	name string,
	contextName string,
	contextKind string,
	lineNumber int,
	parameters []string,
	routeAttributePending *bool,
) {
	methodKey := phpMethodKey{name: name, arity: len(parameters)}
	if contextKind == "interface_declaration" {
		if facts.interfaceMethods[contextName] == nil {
			facts.interfaceMethods[contextName] = map[phpMethodKey]struct{}{}
		}
		facts.interfaceMethods[contextName][methodKey] = struct{}{}
	}
	if *routeAttributePending {
		facts.symfonyRouteAttributeLine[lineNumber] = struct{}{}
		*routeAttributePending = false
	}
}

func collectPHPLiteralRouteTargets(line string, facts phpDeadCodeFacts) {
	for _, match := range phpClassMethodArrayPattern.FindAllStringSubmatchIndex(line, -1) {
		if len(match) != 6 || phpIndexInQuotedString(line, match[0]) {
			continue
		}
		className := normalizePHPTypeName(line[match[2]:match[3]])
		methodName := strings.TrimSpace(line[match[4]:match[5]])
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
	for _, match := range phpWordPressHookPattern.FindAllStringSubmatchIndex(line, -1) {
		if len(match) != 4 || phpIndexInQuotedString(line, match[0]) {
			continue
		}
		if functionName := strings.TrimSpace(line[match[2]:match[3]]); functionName != "" {
			facts.wordpressFunctionTargets[functionName] = struct{}{}
		}
	}
}

func phpExecutableLineWithoutComments(rawLine string, inBlockComment *bool) string {
	var builder strings.Builder
	var quote rune
	escaped := false
	for index, r := range rawLine {
		if *inBlockComment {
			if r == '/' && index > 0 && rawLine[index-1] == '*' {
				*inBlockComment = false
			}
			continue
		}
		if quote != 0 {
			builder.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
			builder.WriteRune(r)
			continue
		}
		if r == '/' && index+1 < len(rawLine) {
			switch rawLine[index+1] {
			case '/':
				return strings.TrimSpace(builder.String())
			case '*':
				*inBlockComment = true
				continue
			}
		}
		if r == '#' && (index+1 >= len(rawLine) || rawLine[index+1] != '[') {
			return strings.TrimSpace(builder.String())
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

func phpIndexInQuotedString(line string, target int) bool {
	var quote rune
	escaped := false
	for index, r := range line {
		if index >= target {
			return quote != 0
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			quote = r
		}
	}
	return quote != 0
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
		if phpIsMagicMethod(name) {
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
		if phpInterfaceHasMethod(base, methodKey, facts, map[string]struct{}{}) {
			return true
		}
	}
	return false
}

func phpInterfaceHasMethod(interfaceName string, methodKey phpMethodKey, facts phpDeadCodeFacts, seen map[string]struct{}) bool {
	if _, ok := seen[interfaceName]; ok {
		return false
	}
	seen[interfaceName] = struct{}{}
	if _, ok := facts.interfaceMethods[interfaceName][methodKey]; ok {
		return true
	}
	for _, base := range facts.typeBases[interfaceName] {
		if facts.typeKinds[base] == "interface_declaration" && phpInterfaceHasMethod(base, methodKey, facts, seen) {
			return true
		}
	}
	return false
}

func phpIsControllerAction(contextName string, name string, rawLine string, routeBacked bool) bool {
	if !strings.HasSuffix(contextName, "Controller") || strings.HasPrefix(name, "__") {
		return false
	}
	if !routeBacked {
		return false
	}
	return phpMethodLineIsPublic(rawLine)
}

func phpIsMagicMethod(name string) bool {
	switch strings.ToLower(name) {
	case "__construct", "__destruct", "__call", "__callstatic", "__get", "__set",
		"__isset", "__unset", "__sleep", "__wakeup", "__serialize", "__unserialize",
		"__tostring", "__invoke", "__set_state", "__clone", "__debuginfo":
		return true
	default:
		return false
	}
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
