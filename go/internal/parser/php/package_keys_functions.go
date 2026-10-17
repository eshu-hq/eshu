// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/php/composer"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file stamps the #8280 PHP function keys. A namespaced free function
// that composer loads through autoload.files carries export_name
// function:<FQN>: PHP keeps functions and classes in separate tables, so a bare
// <FQN> could collide with a class of the same name. A consumer keys a bare
// `f(` bound by a `use function` clause.

// phpFunctionExportPrefix starts the export name of a free function.
const phpFunctionExportPrefix = "function:"

// phpFunctionDeclaration is one free function eligible for a producer key.
type phpFunctionDeclaration struct {
	name string
	item map[string]any
}

// recordFunction stages a free function for the producer key when it is
// declared outside any function, method, closure, or type: a function declared
// inside another function exists only after the outer one runs.
func (k *phpPackageKeys) recordFunction(node *tree_sitter.Node, parents *phpParentLookup, item map[string]any) {
	if k == nil || node == nil {
		return
	}
	for current := parents.parent(node); current != nil; current = parents.parent(current) {
		switch current.Kind() {
		case "function_definition", "method_declaration", "anonymous_function", "arrow_function",
			"class_declaration", "interface_declaration", "trait_declaration", "enum_declaration", "anonymous_class":
			return
		}
	}
	name, _ := item["name"].(string)
	k.functions = append(k.functions, phpFunctionDeclaration{name: name, item: item})
}

// recordFunctionUse binds the local name of one `use function` clause to its
// fully qualified name. A local name bound to two different functions is
// blocked.
func (k *phpPackageKeys) recordFunctionUse(fqn string, alias string) {
	if k == nil {
		return
	}
	fqn = strings.Trim(strings.TrimSpace(fqn), `\`)
	local := strings.TrimSpace(alias)
	if local == "" {
		local = shared.LastPathSegment(fqn, `\`)
	}
	if !phpIsQualifiedName(fqn) || !strings.Contains(fqn, `\`) || !phpIsPlainName(local) {
		return
	}
	if existing, ok := k.functionTargets[local]; ok && existing != fqn {
		k.blockedFunctions[local] = struct{}{}
		return
	}
	k.functionTargets[local] = fqn
}

// stampFunctionProducers sets the free-function producer keys. namespace is
// the file's single namespace ("" for the global one).
func (k *phpPackageKeys) stampFunctionProducers(namespace string) {
	for _, function := range k.functions {
		if !phpIsPlainName(function.name) {
			continue
		}
		fqn := phpQualify(namespace, function.name)
		packageName := composer.ProducerFunctionPackage(k.repoRoot, k.path, fqn)
		if packageName == "" {
			continue
		}
		function.item["package_id"] = packageName
		function.item["export_name"] = phpFunctionExportPrefix + fqn
	}
}

// functionKey returns the package_export_symbol for a bare call of name, or ""
// when no `use function` clause binds name or no composer.lock names the
// package that provides it. An unbound bare name is never keyed: PHP resolves
// it in the current namespace, then the global one.
func (k *phpPackageKeys) functionKey(name string) string {
	if k == nil {
		return ""
	}
	if _, blocked := k.blockedFunctions[name]; blocked {
		return ""
	}
	fqn := k.functionTargets[name]
	if fqn == "" {
		return ""
	}
	packageName := k.lockedPackage(fqn)
	if packageName == "" {
		return ""
	}
	return "package:" + packageName + "#" + phpFunctionExportPrefix + fqn
}

// phpUseClauseIsFunction reports whether a use clause carries its own
// `function` keyword, as in the mixed group `use A\{B, function c}`.
func phpUseClauseIsFunction(clause *tree_sitter.Node) bool {
	for index := uint(0); index < clause.ChildCount(); index++ {
		child := clause.Child(index)
		if child != nil && !child.IsNamed() && child.Kind() == "function" {
			return true
		}
	}
	return false
}
