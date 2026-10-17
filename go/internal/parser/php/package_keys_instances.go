// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// This file stamps the #8280 PHP instance keys on top of the #7601 ones. A
// public, concrete, non-magic instance method of a keyed class carries
// export_name <FQCN>-><method>, distinct from the static <FQCN>::<method>. A
// consumer keys `(new X())->m(` and `$v->m(` when $v is proven to hold an X
// (package_keys_scopes.go).

// phpInstanceMemberSeparator joins a class name and an instance method.
const phpInstanceMemberSeparator = "->"

// stampInstanceProducers sets the instance-method producer keys. keyedClasses
// maps a keyed class's short name to its package and FQCN.
func (k *phpPackageKeys) stampInstanceProducers(keyedClasses map[string][2]string) {
	for _, method := range k.publicInstanceMethods {
		keyed, ok := keyedClasses[method.className]
		name, _ := method.item["name"].(string)
		if !ok || !phpIsPlainName(name) {
			continue
		}
		method.item["package_id"] = keyed[0]
		method.item["export_name"] = keyed[1] + phpInstanceMemberSeparator + name
	}
}

// instanceKey returns the package_export_symbol for a member call node
// (`object->method(`), or "" when the receiver is not proven to be an
// instance of a keyable class: the object must be `(new X(...))`, or a
// variable that its scope writes exactly once, by `new X(...)`, and otherwise
// reads only as the object of a member call or property access.
func (k *phpPackageKeys) instanceKey(state *phpParseState, node *tree_sitter.Node, objectNode *tree_sitter.Node, method string) string {
	if k == nil || node.Kind() != "member_call_expression" || !phpIsPlainName(method) || !k.hasLocks() {
		return ""
	}
	var creation *tree_sitter.Node
	switch objectNode.Kind() {
	case "parenthesized_expression":
		if objectNode.NamedChildCount() == 1 {
			if inner := objectNode.NamedChild(0); inner != nil && inner.Kind() == "object_creation_expression" {
				creation = inner
			}
		}
	case "variable_name":
		name := shared.NodeText(objectNode, state.source)
		if _, candidate := k.newAssignedVariables(state)[name]; !candidate {
			return ""
		}
		root := phpVariableScopeRoot(node, state.parents)
		scope := k.scopeSummaries(state)[root.Id()]
		if scope == nil {
			return ""
		}
		use := scope.vars[name]
		if scope.blocked || use == nil || use.newWrites != 1 || use.other != 0 {
			return ""
		}
		// A function scope starts empty, so its one write is the only value the
		// variable can hold. A script's variables can come from the file that
		// includes it and can be rewritten by any code through `global` or
		// $GLOBALS, so there the write must run before the call on every path
		// and nothing that could run other code may sit between them.
		if root.Kind() == "program" && (!phpWriteDominates(use.assignment, node, state.parents) ||
			!phpScriptRegionInert(state, use.assignment, node, name)) {
			return ""
		}
		creation = use.creation
	}
	if creation == nil {
		return ""
	}
	fqcn := k.classTarget(phpObjectCreationTypeNode(creation), state.source)
	if fqcn == "" {
		return ""
	}
	packageName := k.lockedPackage(fqcn)
	if packageName == "" {
		return ""
	}
	return "package:" + packageName + "#" + fqcn + phpInstanceMemberSeparator + method
}

// phpDeclaredAtTopLevel reports whether a declaration is a direct child of
// the file or of a braced namespace body.
func phpDeclaredAtTopLevel(node *tree_sitter.Node, parents *phpParentLookup) bool {
	parent := parents.parent(node)
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "program":
		return true
	case "compound_statement":
		namespace := parents.parent(parent)
		return namespace != nil && namespace.Kind() == "namespace_definition"
	}
	return false
}

// phpMethodIsInstanceKeyable reports whether a non-static method can carry
// an instance key: it has a body (an abstract method has none) and is not a
// magic method, which PHP invokes implicitly rather than by a named member
// call.
func phpMethodIsInstanceKeyable(node *tree_sitter.Node, name string) bool {
	return !strings.HasPrefix(name, "__") && node.ChildByFieldName("body") != nil
}
