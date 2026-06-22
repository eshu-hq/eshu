package swift

import (
	"slices"
	"strings"
)

// swiftTypeDeadCodeRootKinds returns the dead-code root kinds a nominal type
// declaration roots: protocol types, @main entrypoints, and SwiftUI/UIKit
// application types inferred from conformances and attributes.
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

// swiftVariableDeadCodeRootKinds roots a SwiftUI `App` scene body so the view
// hierarchy it builds is not reported as unreachable.
func swiftVariableDeadCodeRootKinds(name string, varType string, contextName string, facts swiftSemanticFacts) []string {
	if name == "body" && strings.Contains(varType, "Scene") && swiftConformsTo(facts, contextName, "App") {
		return []string{"swift.swiftui_body"}
	}
	return nil
}

// swiftFunctionDeadCodeRootKinds returns the dead-code root kinds a function or
// initializer roots: program entrypoints, constructors, protocol requirements and
// same-file implementations, overrides, UIKit delegate callbacks, Vapor route
// handlers, and XCTest/Swift Testing methods.
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

// swiftImplementsProtocolMethod reports whether typeName conforms to a same-file
// protocol that declares methodName, so the concrete implementation roots.
func swiftImplementsProtocolMethod(facts swiftSemanticFacts, typeName string, methodName string) bool {
	for protocolName := range facts.typeConformances[typeName] {
		methods := facts.protocolMethods[protocolName]
		if _, ok := methods[methodName]; ok {
			return true
		}
	}
	return false
}

// swiftConformsTo reports whether typeName conforms to any of the candidate base
// names according to same-file conformance evidence.
func swiftConformsTo(facts swiftSemanticFacts, typeName string, candidates ...string) bool {
	conformances := facts.typeConformances[typeName]
	for _, candidate := range candidates {
		if _, ok := conformances[candidate]; ok {
			return true
		}
	}
	return false
}

// swiftStringSet returns a set of short type names from raw conformance text,
// dropping optionals, generic arguments, and module qualifiers.
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

// swiftShortTypeName reduces a type reference to its leaf identifier by trimming a
// trailing optional marker, generic argument clause, and module qualifier.
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

// swiftHasAttribute reports whether the attribute name is present in the
// declaration's leading attribute list.
func swiftHasAttribute(attributes []string, name string) bool {
	return slices.Contains(attributes, name)
}

// appendSwiftRootKind appends a dead-code root kind once, preserving order and
// avoiding duplicate entries when several rules match the same declaration.
func appendSwiftRootKind(rootKinds []string, rootKind string) []string {
	if slices.Contains(rootKinds, rootKind) {
		return rootKinds
	}
	return append(rootKinds, rootKind)
}
