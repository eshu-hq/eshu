package swift

import (
	"slices"
	"strings"
)

// swiftSemanticFacts carries the whole-file knowledge dead-code root
// classification needs: which methods each protocol declares, which types each
// type conforms to, and which function names are referenced as Vapor route
// handlers. The syntax index populates it from the AST before row emission.
type swiftSemanticFacts struct {
	protocolMethods    map[string]map[string]struct{}
	typeConformances   map[string]map[string]struct{}
	vaporRouteHandlers map[string]struct{}
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

func appendSwiftRootKind(rootKinds []string, rootKind string) []string {
	if slices.Contains(rootKinds, rootKind) {
		return rootKinds
	}
	return append(rootKinds, rootKind)
}
