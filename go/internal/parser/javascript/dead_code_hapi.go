// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// javaScriptIsHapiHandlerFile reports whether the current source file sits
// under a Hapi OpenAPI handler directory declared in this repository.
func javaScriptIsHapiHandlerFile(repoRoot string, path string, siblingParser *javaScriptSiblingParser) bool {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	relativePath, ok := relativeSlashPath(repoRoot, path)
	if !ok || !strings.Contains(relativePath, "/handlers/") {
		return false
	}
	for _, handlerDir := range javaScriptHapiHandlerDirs(repoRoot, path, siblingParser) {
		if path == handlerDir || strings.HasPrefix(path, handlerDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func javaScriptHapiHandlerDirs(repoRoot string, path string, siblingParser *javaScriptSiblingParser) []string {
	serviceRoots := []string{repoRoot}
	if packageRoot, ok := nearestJavaScriptPackageRoot(repoRoot, path); ok {
		serviceRoots = appendUniqueString(serviceRoots, packageRoot)
	}

	candidates := []string{}
	for _, serviceRoot := range serviceRoots {
		candidates = append(
			candidates,
			filepath.Join(serviceRoot, "server", "init", "plugins", "spec.js"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "spec.ts"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "specs.js"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "specs.ts"),
		)
	}

	dirs := []string{}
	for _, candidate := range candidates {
		root, source, ok := siblingParser.rootForFile(candidate)
		if !ok {
			continue
		}
		if !javaScriptLooksLikeHapiSpecsPlugin(string(source)) {
			continue
		}
		for _, relative := range javaScriptHapiHandlerSpecDirs(root, source) {
			resolved := filepath.Clean(filepath.Join(filepath.Dir(candidate), relative))
			dirs = appendUniqueString(dirs, resolved)
		}
	}
	return dirs
}

// javaScriptHapiHandlerSpecDirs walks a parsed spec plugin for
// handlers: path.resolve|join(__dirname, "dir") pairs and returns each relative
// handler directory in source order. The handler value must be a path.resolve or
// path.join call whose first argument is __dirname and whose second argument is a
// string literal, matching the prior regex contract via the AST.
func javaScriptHapiHandlerSpecDirs(root *tree_sitter.Node, source []byte) []string {
	relatives := make([]string, 0)
	if root == nil {
		return relatives
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "pair" {
			return
		}
		if javaScriptHapiPairKey(node, source) != "handlers" {
			return
		}
		valueNode := node.ChildByFieldName("value")
		if relative, ok := javaScriptDirnamePathArgument(valueNode, source); ok {
			relatives = append(relatives, relative)
		}
	})
	return relatives
}

// javaScriptDirnamePathArgument returns the string-literal second argument of a
// path.resolve|join(__dirname, "dir") call, with ok=false otherwise.
func javaScriptDirnamePathArgument(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "call_expression" {
		return "", false
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "member_expression" {
		return "", false
	}
	objectNode := functionNode.ChildByFieldName("object")
	propertyNode := functionNode.ChildByFieldName("property")
	if objectNode == nil || strings.TrimSpace(nodeText(objectNode, source)) != "path" {
		return "", false
	}
	switch strings.TrimSpace(nodeText(propertyNode, source)) {
	case "resolve", "join":
	default:
		return "", false
	}
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return "", false
	}
	cursor := argsNode.Walk()
	args := argsNode.NamedChildren(cursor)
	cursor.Close()
	if len(args) != 2 {
		return "", false
	}
	if args[0].Kind() != "identifier" || strings.TrimSpace(nodeText(&args[0], source)) != "__dirname" {
		return "", false
	}
	if args[1].Kind() != "string" {
		return "", false
	}
	value := jsStringLiteralValue(&args[1], source)
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func javaScriptLooksLikeHapiSpecsPlugin(source string) bool {
	normalized := strings.ToLower(source)
	return strings.Contains(normalized, "openapi")
}
