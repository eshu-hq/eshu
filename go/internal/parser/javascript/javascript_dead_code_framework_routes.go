package javascript

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	javaScriptKoaRouteMethods = map[string]struct{}{
		"get":     {},
		"post":    {},
		"put":     {},
		"patch":   {},
		"delete":  {},
		"head":    {},
		"options": {},
		"all":     {},
	}
	javaScriptFastifyRouteMethods = map[string]struct{}{
		"get":     {},
		"post":    {},
		"put":     {},
		"patch":   {},
		"delete":  {},
		"head":    {},
		"options": {},
		"route":   {},
	}
)

func javaScriptFrameworkRegisteredDeadCodeRootKinds(
	root *tree_sitter.Node,
	source []byte,
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil {
		return registered
	}

	text := string(source)
	expressBases := javaScriptExpressRegistrationBases(root, source, text)
	koaBases := javaScriptKoaRegistrationBases(root, source, text)
	fastifyBases := javaScriptFastifyRegistrationBases(root, source, text)
	if len(expressBases) == 0 && len(koaBases) == 0 && len(fastifyBases) == 0 {
		return registered
	}

	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		base, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok {
			return
		}
		property = strings.ToLower(property)
		args := javaScriptCallArguments(node)
		switch {
		case javaScriptNameSetContains(expressBases, base) && property == "use":
			javaScriptRegisterHandlerArgs(
				registered,
				args,
				source,
				"javascript.express_middleware_registration",
			)
		case javaScriptNameSetContains(koaBases, base):
			if property == "use" {
				javaScriptRegisterHandlerArgs(
					registered,
					args,
					source,
					"javascript.koa_middleware_registration",
				)
				return
			}
			if _, ok := javaScriptKoaRouteMethods[property]; ok {
				javaScriptRegisterHandlerArgs(
					registered,
					javaScriptRouteHandlerArgs(args),
					source,
					"javascript.koa_route_registration",
				)
			}
		case javaScriptNameSetContains(fastifyBases, base):
			if property == "addhook" {
				javaScriptRegisterHandlerArgs(
					registered,
					javaScriptArgsAfter(args, 1),
					source,
					"javascript.fastify_hook_registration",
				)
				return
			}
			if property == "register" {
				javaScriptRegisterHandlerArgs(
					registered,
					javaScriptArgsAfter(args, 0),
					source,
					"javascript.fastify_plugin_registration",
				)
				return
			}
			if _, ok := javaScriptFastifyRouteMethods[property]; ok {
				routeArgs := javaScriptRouteHandlerArgs(args)
				if property == "route" {
					routeArgs = javaScriptFastifyRouteObjectHandlerArgs(args, source)
				}
				javaScriptRegisterHandlerArgs(
					registered,
					routeArgs,
					source,
					"javascript.fastify_route_registration",
				)
			}
		}
	})
	return registered
}

func javaScriptExpressRegistrationBases(root *tree_sitter.Node, source []byte, text string) map[string]struct{} {
	bases := make(map[string]struct{})
	if !javaScriptHasExpressImport(text) {
		return bases
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		name, value := javaScriptVariableDeclaratorNameValue(node, source)
		if name == "" || value == nil {
			return
		}
		callName := strings.ToLower(javaScriptCallFullName(value.ChildByFieldName("function"), source))
		if callName == "express" || callName == "express.router" {
			javaScriptAddName(bases, name)
		}
	})
	return bases
}

func javaScriptKoaRegistrationBases(root *tree_sitter.Node, source []byte, text string) map[string]struct{} {
	bases := make(map[string]struct{})
	if !javaScriptHasKoaRouterImport(text) {
		return bases
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		name, value := javaScriptVariableDeclaratorNameValue(node, source)
		if name == "" || value == nil || value.Kind() != "new_expression" {
			return
		}
		constructorName, constructorFullName := javaScriptNewExpressionConstructorName(value, source)
		if constructorName == "Router" || strings.Contains(strings.ToLower(constructorFullName), "router") {
			javaScriptAddName(bases, name)
		}
	})
	return bases
}

func javaScriptFastifyRegistrationBases(root *tree_sitter.Node, source []byte, text string) map[string]struct{} {
	bases := make(map[string]struct{})
	if !javaScriptHasFastifyImport(text) {
		return bases
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		name, value := javaScriptVariableDeclaratorNameValue(node, source)
		if name == "" || value == nil || value.Kind() != "call_expression" {
			return
		}
		callName := strings.ToLower(javaScriptCallFullName(value.ChildByFieldName("function"), source))
		if callName == "fastify" {
			javaScriptAddName(bases, name)
		}
	})
	return bases
}

func javaScriptVariableDeclaratorNameValue(node *tree_sitter.Node, source []byte) (string, *tree_sitter.Node) {
	if node == nil || node.Kind() != "variable_declarator" {
		return "", nil
	}
	name := javaScriptIdentifierName(node.ChildByFieldName("name"), source)
	return name, node.ChildByFieldName("value")
}

func javaScriptCallArguments(node *tree_sitter.Node) []tree_sitter.Node {
	if node == nil {
		return nil
	}
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return nil
	}
	cursor := argsNode.Walk()
	args := argsNode.NamedChildren(cursor)
	cursor.Close()
	return args
}

func javaScriptRouteHandlerArgs(args []tree_sitter.Node) []tree_sitter.Node {
	if len(args) <= 1 {
		return nil
	}
	return args[1:]
}

func javaScriptFastifyRouteObjectHandlerArgs(args []tree_sitter.Node, source []byte) []tree_sitter.Node {
	handlers := make([]tree_sitter.Node, 0, 1)
	for i := range args {
		if args[i].Kind() != "object" {
			continue
		}
		handlers = append(handlers, javaScriptObjectHandlerValues(&args[i], source)...)
	}
	if len(handlers) > 0 {
		return handlers
	}
	return javaScriptRouteHandlerArgs(args)
}

func javaScriptArgsAfter(args []tree_sitter.Node, index int) []tree_sitter.Node {
	if len(args) <= index {
		return nil
	}
	return args[index:]
}

func javaScriptRegisterHandlerArgs(
	registered map[string][]string,
	args []tree_sitter.Node,
	source []byte,
	rootKind string,
) {
	for i := range args {
		if javaScriptArgumentIsStringLiteral(&args[i]) {
			continue
		}
		for _, handlerName := range javaScriptExpressHandlerNames(&args[i], source) {
			key := strings.ToLower(handlerName)
			registered[key] = appendUniqueString(registered[key], rootKind)
		}
	}
}

func javaScriptObjectHandlerValues(objectNode *tree_sitter.Node, source []byte) []tree_sitter.Node {
	if objectNode == nil || objectNode.Kind() != "object" {
		return nil
	}
	handlers := make([]tree_sitter.Node, 0, 1)
	cursor := objectNode.Walk()
	children := objectNode.NamedChildren(cursor)
	cursor.Close()
	for i := range children {
		child := children[i]
		if child.Kind() != "pair" {
			continue
		}
		key := strings.Trim(strings.TrimSpace(nodeText(child.ChildByFieldName("key"), source)), `"'`)
		if key != "handler" {
			continue
		}
		valueNode := child.ChildByFieldName("value")
		if valueNode != nil {
			handlers = append(handlers, *valueNode)
		}
	}
	return handlers
}

func javaScriptArgumentIsStringLiteral(node *tree_sitter.Node) bool {
	return node != nil && (node.Kind() == "string" || node.Kind() == "template_string")
}

func javaScriptHasKoaRouterImport(source string) bool {
	return strings.Contains(source, `from "@koa/router"`) ||
		strings.Contains(source, `from '@koa/router'`) ||
		strings.Contains(source, `require("@koa/router")`) ||
		strings.Contains(source, `require('@koa/router')`) ||
		strings.Contains(source, `from "koa-router"`) ||
		strings.Contains(source, `from 'koa-router'`) ||
		strings.Contains(source, `require("koa-router")`) ||
		strings.Contains(source, `require('koa-router')`)
}

func javaScriptHasFastifyImport(source string) bool {
	return strings.Contains(source, `from "fastify"`) ||
		strings.Contains(source, `from 'fastify'`) ||
		strings.Contains(source, `require("fastify")`) ||
		strings.Contains(source, `require('fastify')`)
}

func javaScriptNameSetContains(names map[string]struct{}, name string) bool {
	_, ok := names[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func javaScriptAddName(names map[string]struct{}, name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" {
		names[name] = struct{}{}
	}
}

func javaScriptIsNestJSControllerMethod(node *tree_sitter.Node, source []byte) bool {
	if node == nil || node.Kind() != "method_definition" || !javaScriptHasNestJSCommonImport(string(source)) {
		return false
	}
	if !javaScriptDecoratorsInclude(javaScriptNestJSRouteDecorators(node, source), javaScriptNestJSRouteDecoratorNames()) {
		return false
	}
	classNode := javaScriptEnclosingClassNode(node)
	return javaScriptDecoratorsInclude(javaScriptNestJSRouteDecorators(classNode, source), map[string]struct{}{"controller": {}})
}

func javaScriptHasNestJSCommonImport(source string) bool {
	return strings.Contains(source, `from "@nestjs/common"`) ||
		strings.Contains(source, `from '@nestjs/common'`) ||
		strings.Contains(source, `require("@nestjs/common")`) ||
		strings.Contains(source, `require('@nestjs/common')`)
}

func javaScriptNestJSRouteDecoratorNames() map[string]struct{} {
	return map[string]struct{}{
		"all":     {},
		"delete":  {},
		"get":     {},
		"head":    {},
		"options": {},
		"patch":   {},
		"post":    {},
		"put":     {},
	}
}

func javaScriptDecoratorsInclude(decorators []string, allowed map[string]struct{}) bool {
	for _, decorator := range decorators {
		name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(decorator), "@"))
		if index := strings.Index(name, "("); index >= 0 {
			name = name[:index]
		}
		if _, ok := allowed[name]; ok {
			return true
		}
	}
	return false
}

func javaScriptNestJSRouteDecorators(node *tree_sitter.Node, source []byte) []string {
	decorators := javaScriptDecorators(node, source)
	if len(decorators) > 0 || node == nil {
		return decorators
	}
	if node.Parent() != nil && node.Parent().Kind() == "decorated_definition" {
		decorators = javaScriptDecorators(node.Parent(), source)
	}
	if len(decorators) > 0 {
		return decorators
	}
	return javaScriptContiguousLeadingDecorators(node, source)
}

func javaScriptContiguousLeadingDecorators(node *tree_sitter.Node, source []byte) []string {
	if node == nil || node.StartByte() == 0 {
		return nil
	}
	prefix := string(source[:node.StartByte()])
	lineEnd := strings.LastIndex(prefix, "\n")
	decorators := []string{}
	for lineEnd >= 0 {
		lineStart := strings.LastIndex(prefix[:lineEnd], "\n")
		rawLine := prefix[lineStart+1 : lineEnd]
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			lineEnd = lineStart
			continue
		}
		if !strings.HasPrefix(trimmed, "@") {
			break
		}
		decorators = append([]string{trimmed}, decorators...)
		lineEnd = lineStart
	}
	return decorators
}

func javaScriptEnclosingClassNode(node *tree_sitter.Node) *tree_sitter.Node {
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "class_declaration", "abstract_class_declaration":
			return current
		case "program":
			return nil
		}
	}
	return nil
}
