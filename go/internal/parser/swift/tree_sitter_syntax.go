// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// swiftSourceAndTree reads a Swift file and parses it with the caller-owned
// tree-sitter parser, returning the source bytes and the parsed tree. The caller
// owns the returned tree and must Close it.
func swiftSourceAndTree(path string, parser *tree_sitter.Parser) ([]byte, *tree_sitter.Tree, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, nil, err
	}
	if parser == nil {
		return nil, nil, fmt.Errorf("parse swift tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, nil, fmt.Errorf("parse swift tree: parser returned nil tree")
	}
	return source, tree, nil
}

// collectSwiftSemanticFacts walks the AST once to gather the same-file evidence
// dead-code classification needs: type conformances, protocol method
// requirements, and Vapor route handler names. The Vapor `use:` route hint has no
// dedicated symbol node, so it is read as framework evidence from call argument
// labels rather than producing a call or symbol row.
//
// Before the walk-collapse fix (issue #4841, epic #4831), conformance/method
// collection, the Vapor import check, the route-receiver scan, and the
// value_argument handler scan each ran their own full-tree traversal (two via
// shared.WalkNamed, one via manual recursion). None of them consumes another's
// output while collecting — collectSwiftFileFacts only needs the fully
// populated route-receiver map once it is done, to resolve group-nested
// receivers in swiftVaporRouteEntries afterward — so all four now run in one
// combined recursive walk, and only the Vapor-gated route-entries pass (which
// genuinely depends on that completed map) still runs as its own traversal.
func collectSwiftSemanticFacts(root *tree_sitter.Node, source []byte) swiftSemanticFacts {
	facts := swiftSemanticFacts{
		protocolMethods:    make(map[string]map[string]struct{}),
		typeConformances:   make(map[string]map[string]struct{}),
		vaporRouteHandlers: make(map[string]struct{}),
		vaporRouteEntries:  []map[string]string{},
	}
	state := &swiftPrePassState{routeReceivers: make(map[string]swiftVaporRouteReceiver)}
	collectSwiftFileFacts(root, source, "", "", &facts, state)
	if state.hasVaporImport {
		facts.vaporRouteEntries = append(facts.vaporRouteEntries, swiftVaporRouteEntries(root, source, state.routeReceivers)...)
	}
	return facts
}

// swiftPrePassState carries the Vapor-import flag and the Vapor
// route-receiver candidates collectSwiftFileFacts accumulates alongside the
// type-conformance and protocol-method facts. It is threaded by pointer
// since both fields are written during the single combined walk and only
// read afterward, once the walk (and therefore the route-receiver map) is
// complete.
type swiftPrePassState struct {
	hasVaporImport bool
	routeReceivers map[string]swiftVaporRouteReceiver
}

// collectSwiftFileFacts records each nominal type's conformance set, each
// protocol's declared method names, the Vapor import flag, Vapor
// route-receiver candidates, and Vapor `use:` handler names in one combined
// recursive walk, descending with the enclosing type context so protocol
// requirements attribute to their protocol.
func collectSwiftFileFacts(
	node *tree_sitter.Node,
	source []byte,
	currentType string,
	currentKind string,
	facts *swiftSemanticFacts,
	state *swiftPrePassState,
) {
	if node == nil {
		return
	}
	nextType := currentType
	nextKind := currentKind

	switch node.Kind() {
	case "class_declaration":
		if nameNode := swiftFirstChildOfKind(node, "type_identifier"); nameNode != nil {
			name := strings.TrimSpace(shared.NodeText(nameNode, source))
			keyword := swiftDeclarationKeyword(node, nameNode.StartByte(), source)
			if bucket, kind := swiftTypeBucketKind(keyword); bucket != "" && name != "" {
				nextType = name
				nextKind = kind
				facts.typeConformances[name] = swiftStringSet(swiftInheritanceBases(node, source))
			}
		} else if extended := swiftExtensionTypeName(node, source); extended != "" {
			nextType = extended
			nextKind = "extension"
		}
	case "protocol_declaration":
		if nameNode := swiftFirstChildOfKind(node, "type_identifier"); nameNode != nil {
			name := strings.TrimSpace(shared.NodeText(nameNode, source))
			if name != "" {
				nextType = name
				nextKind = "protocol"
				facts.typeConformances[name] = swiftStringSet(swiftInheritanceBases(node, source))
			}
		}
	case "function_declaration", "protocol_function_declaration":
		if currentKind == "protocol" && currentType != "" {
			if nameNode := swiftFirstChildOfKind(node, "simple_identifier"); nameNode != nil {
				name := strings.TrimSpace(shared.NodeText(nameNode, source))
				if name != "" {
					if facts.protocolMethods[currentType] == nil {
						facts.protocolMethods[currentType] = make(map[string]struct{})
					}
					facts.protocolMethods[currentType][name] = struct{}{}
				}
			}
		}
	case "import_declaration":
		if !state.hasVaporImport {
			if identifier := swiftFirstChildOfKind(node, "identifier"); identifier != nil {
				if strings.TrimSpace(shared.NodeText(identifier, source)) == "Vapor" {
					state.hasVaporImport = true
				}
			}
		}
	case "parameter":
		if name := swiftParameterName(node, source); name != "" {
			switch swiftVaporReceiverTypeName(node, source) {
			case "Application", "RoutesBuilder":
				state.routeReceivers[name] = swiftVaporRouteReceiver{}
			}
		}
	case "property_declaration":
		pattern := swiftFirstChildOfKind(node, "pattern", "simple_identifier")
		if name := swiftPatternName(pattern, source); name != "" {
			switch swiftVaporReceiverTypeName(node, source) {
			case "Application", "RoutesBuilder":
				state.routeReceivers[name] = swiftVaporRouteReceiver{}
			}
		}
	case "value_argument":
		collectSwiftVaporRouteHandler(node, source, facts)
	}

	for _, child := range swiftNamedChildren(node) {
		child := child
		collectSwiftFileFacts(&child, source, nextType, nextKind, facts, state)
	}
}

type swiftVaporRouteReceiver struct {
	pathSegments []string
}

func swiftVaporReceiverTypeName(node *tree_sitter.Node, source []byte) string {
	typeName := swiftTypeAnnotationText(node, source)
	if typeName == "" && node.Kind() == "parameter" {
		text := strings.TrimSpace(shared.NodeText(node, source))
		if _, after, ok := strings.Cut(text, ":"); ok {
			typeName = strings.TrimSpace(after)
		}
	}
	if index := strings.IndexAny(typeName, " =,"); index >= 0 {
		typeName = typeName[:index]
	}
	return swiftShortTypeName(typeName)
}

func collectSwiftVaporRouteHandler(node *tree_sitter.Node, source []byte, facts *swiftSemanticFacts) {
	label := swiftFirstChildOfKind(node, "value_argument_label")
	if label == nil {
		return
	}
	if strings.TrimSpace(shared.NodeText(label, source)) != "use" {
		return
	}
	for _, child := range swiftNamedChildren(node) {
		child := child
		if child.Kind() == "simple_identifier" {
			name := strings.TrimSpace(shared.NodeText(&child, source))
			if name != "" {
				facts.vaporRouteHandlers[name] = struct{}{}
			}
		}
	}
}

func swiftVaporRouteEntry(
	node *tree_sitter.Node,
	source []byte,
	routeReceivers map[string]swiftVaporRouteReceiver,
) map[string]string {
	receiver, callName := swiftCallTarget(node, source)
	if receiver == "" {
		return nil
	}
	receiverInfo, ok := routeReceivers[receiver]
	if !ok {
		return nil
	}
	httpMethod := swiftVaporHTTPMethod(callName)
	if httpMethod == "" {
		return nil
	}

	args := swiftCallArguments(node, source)
	handler, ok := swiftVaporUseHandler(args)
	if !ok {
		return nil
	}
	pathArgs := args
	if callName == "on" {
		method, rest, ok := swiftVaporOnMethodAndPathArgs(args)
		if !ok {
			return nil
		}
		httpMethod = method
		pathArgs = rest
	}
	segments, ok := swiftVaporPathSegments(pathArgs)
	if !ok {
		return nil
	}
	if len(receiverInfo.pathSegments) > 0 {
		prefixed := make([]string, 0, len(receiverInfo.pathSegments)+len(segments))
		prefixed = append(prefixed, receiverInfo.pathSegments...)
		prefixed = append(prefixed, segments...)
		segments = prefixed
	}
	return map[string]string{
		"method":  httpMethod,
		"path":    swiftVaporRoutePath(segments),
		"handler": handler,
	}
}

func swiftVaporRouteEntries(
	node *tree_sitter.Node,
	source []byte,
	routeReceivers map[string]swiftVaporRouteReceiver,
) []map[string]string {
	if node == nil {
		return nil
	}
	if node.Kind() == "call_expression" {
		if entries, ok := swiftVaporGroupRouteEntries(node, source, routeReceivers); ok {
			return entries
		}
		if entry := swiftVaporRouteEntry(node, source, routeReceivers); entry != nil {
			return []map[string]string{entry}
		}
	}

	var entries []map[string]string
	for _, child := range swiftNamedChildren(node) {
		child := child
		entries = append(entries, swiftVaporRouteEntries(&child, source, routeReceivers)...)
	}
	return entries
}

func swiftVaporGroupRouteEntries(
	node *tree_sitter.Node,
	source []byte,
	routeReceivers map[string]swiftVaporRouteReceiver,
) ([]map[string]string, bool) {
	receiver, callName := swiftCallTarget(node, source)
	if receiver == "" || callName != "group" {
		return nil, false
	}
	parent, ok := routeReceivers[receiver]
	if !ok {
		return nil, false
	}
	alias := swiftVaporGroupClosureReceiver(node, source)
	if alias == "" {
		return nil, false
	}
	segments, ok := swiftVaporPathSegments(swiftCallArguments(node, source))
	if !ok {
		return nil, true
	}
	pathSegments := make([]string, 0, len(parent.pathSegments)+len(segments))
	pathSegments = append(pathSegments, parent.pathSegments...)
	pathSegments = append(pathSegments, segments...)

	groupReceivers := make(map[string]swiftVaporRouteReceiver, len(routeReceivers)+1)
	for receiver, receiverInfo := range routeReceivers {
		groupReceivers[receiver] = receiverInfo
	}
	groupReceivers[alias] = swiftVaporRouteReceiver{pathSegments: pathSegments}

	var entries []map[string]string
	for _, child := range swiftNamedChildren(node) {
		child := child
		entries = append(entries, swiftVaporRouteEntries(&child, source, groupReceivers)...)
	}
	return entries, true
}

func swiftVaporGroupClosureReceiver(node *tree_sitter.Node, source []byte) string {
	lambda := swiftFirstDescendantOfKind(node, "lambda_literal")
	functionType := swiftFirstDescendantOfKind(lambda, "lambda_function_type")
	if functionType == nil {
		return ""
	}
	identifier := swiftFirstDescendantOfKind(functionType, "simple_identifier")
	if identifier == nil {
		return ""
	}
	name := strings.TrimSpace(shared.NodeText(identifier, source))
	if !swiftSimpleIdentifier(name) {
		return ""
	}
	return name
}

func swiftFirstDescendantOfKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	var found *tree_sitter.Node
	shared.WalkNamed(node, func(candidate *tree_sitter.Node) {
		if found != nil || candidate.Kind() != kind {
			return
		}
		found = shared.CloneNode(candidate)
	})
	return found
}

func swiftVaporHTTPMethod(callName string) string {
	switch callName {
	case "get", "post", "put", "patch", "delete", "options", "head":
		return strings.ToUpper(callName)
	case "on":
		return "ON"
	default:
		return ""
	}
}

func swiftVaporUseHandler(args []string) (string, bool) {
	for _, arg := range args {
		label, value, ok := strings.Cut(arg, ":")
		if !ok || strings.TrimSpace(label) != "use" {
			continue
		}
		handler := strings.TrimSpace(value)
		if swiftSimpleIdentifier(handler) {
			return handler, true
		}
		return "", false
	}
	return "", false
}

func swiftVaporOnMethodAndPathArgs(args []string) (string, []string, bool) {
	if len(args) == 0 {
		return "", nil, false
	}
	method := swiftVaporMethodToken(args[0])
	if method == "" {
		return "", nil, false
	}
	return method, args[1:], true
}

func swiftVaporMethodToken(arg string) string {
	arg = strings.TrimSpace(arg)
	arg = strings.TrimPrefix(arg, ".")
	arg = strings.TrimPrefix(arg, "HTTPMethod.")
	arg = strings.TrimPrefix(arg, "HTTPMethod(")
	arg = strings.TrimSuffix(arg, ")")
	if !swiftSimpleIdentifier(arg) {
		return ""
	}
	return strings.ToUpper(arg)
}

func swiftVaporPathSegments(args []string) ([]string, bool) {
	segments := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(strings.TrimSpace(arg), "use:") {
			break
		}
		segment, ok := swiftExactStringArgument(arg)
		if !ok {
			return nil, false
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return nil, false
	}
	return segments, true
}

func swiftExactStringArgument(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(arg, "\"") || strings.Contains(arg, `\(`) {
		return "", false
	}
	segment, err := strconv.Unquote(arg)
	if err != nil {
		return "", false
	}
	return segment, true
}

func swiftVaporRoutePath(segments []string) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		for _, part := range strings.Split(segment, "/") {
			part = strings.Trim(part, "/")
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, ":") && len(part) > 1 {
				part = "{" + strings.TrimPrefix(part, ":") + "}"
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func swiftSimpleIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
			continue
		}
		if index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

// swiftExtensionTypeName returns the extended type name for an `extension`
// declaration. The Swift grammar models `extension Foo { ... }` as a
// class_declaration whose extended type is a direct user_type child rather than a
// type_identifier name field. The leading `extension` keyword must be present in
// the text before the type so true class/struct/enum declarations are not misread
// as extensions.
func swiftExtensionTypeName(node *tree_sitter.Node, source []byte) string {
	userType := swiftFirstChildOfKind(node, "user_type")
	if userType == nil {
		return ""
	}
	prefix := string(source[node.StartByte():userType.StartByte()])
	if !swiftTextHasToken(prefix, "extension") {
		return ""
	}
	return swiftShortTypeName(strings.TrimSpace(shared.NodeText(userType, source)))
}
