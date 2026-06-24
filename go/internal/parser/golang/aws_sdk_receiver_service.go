// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// awsSDKServiceImportPrefixes are the AWS SDK v2 and v1 import-path prefixes
// that namespace one service client package per directory. A matching import's
// last path segment is the canonical service name (e.g. "s3", "dynamodb").
var awsSDKServiceImportPrefixes = []string{
	"github.com/aws/aws-sdk-go-v2/service/",
	"github.com/aws/aws-sdk-go/service/",
}

// awsSDKConstructorNames are the AWS SDK service-client constructor functions
// recognized for in-file dataflow. v2 clients use NewFromConfig; both v1 and v2
// expose New.
var awsSDKConstructorNames = map[string]struct{}{
	"NewFromConfig": {},
	"New":           {},
}

// goAWSSDKServiceAliases maps each in-file import alias to the AWS SDK service
// name it constructs clients for. Only imports whose path matches an AWS SDK
// service import prefix are included, so a non-SDK package named "s3" never
// produces a binding. The service name is the import path's last segment, which
// stays correct under import aliasing because the alias is the map key.
func goAWSSDKServiceAliases(importAliases map[string][]string) map[string]string {
	serviceAliases := make(map[string]string)
	for importPath, aliases := range importAliases {
		service := awsSDKServiceFromImportPath(importPath)
		if service == "" {
			continue
		}
		for _, alias := range aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			serviceAliases[alias] = service
		}
	}
	return serviceAliases
}

// awsSDKServiceFromImportPath returns the AWS SDK service name for an import
// path, or "" when the path is not an AWS SDK service package. The path must
// match an AWS SDK service prefix and carry exactly one segment after it, so
// transitive sub-packages (for example ".../service/s3/types") do not bind.
func awsSDKServiceFromImportPath(importPath string) string {
	importPath = strings.TrimSpace(importPath)
	for _, prefix := range awsSDKServiceImportPrefixes {
		if !strings.HasPrefix(importPath, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(importPath, prefix)
		if remainder == "" || strings.Contains(remainder, "/") {
			return ""
		}
		return remainder
	}
	return ""
}

// goAWSSDKReceiverBindings records, for in-file dataflow, each local variable
// assigned from an AWS SDK service-client constructor call. Bindings reuse the
// receiver-binding scope model so a variable is only consulted within the
// lexical scope where it was constructed, mirroring goLocalReceiverBindings.
// The service name is stored in the binding's typeName field.
func goAWSSDKReceiverBindings(
	root *tree_sitter.Node,
	source []byte,
	serviceAliases map[string]string,
	lookup *goParentLookup,
) []goLocalReceiverBinding {
	bindings := make([]goLocalReceiverBinding, 0)
	if len(serviceAliases) == 0 {
		return bindings
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "short_var_declaration", "assignment_statement":
			bindings = append(bindings, goAWSSDKReceiverBindingsFromAssignment(node, source, serviceAliases, lookup)...)
		}
	})
	return bindings
}

func goAWSSDKReceiverBindingsFromAssignment(
	node *tree_sitter.Node,
	source []byte,
	serviceAliases map[string]string,
	lookup *goParentLookup,
) []goLocalReceiverBinding {
	names := goAssignableIdentifierNodes(node.ChildByFieldName("left"), source)
	values := goExpressionNodes(node.ChildByFieldName("right"))
	if len(names) == 0 || len(values) == 0 {
		return nil
	}
	count := len(names)
	if len(values) < count {
		count = len(values)
	}
	bindings := make([]goLocalReceiverBinding, 0, count)
	for i := 0; i < count; i++ {
		service := goAWSSDKServiceFromConstructorCall(values[i], source, serviceAliases)
		if service == "" {
			continue
		}
		binding := goNewLocalReceiverBinding(node, names[i], service, true, source, lookup)
		if binding.variable != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// goAWSSDKServiceFromConstructorCall returns the AWS SDK service constructed by
// an expression of the form <alias>.New(...) or <alias>.NewFromConfig(...) when
// <alias> is a known AWS SDK service import alias, or "" otherwise.
func goAWSSDKServiceFromConstructorCall(
	node *tree_sitter.Node,
	source []byte,
	serviceAliases map[string]string,
) string {
	node = goUnwrapSingleExpression(node)
	if node == nil || node.Kind() != "call_expression" {
		return ""
	}
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return ""
	}
	operand := functionNode.ChildByFieldName("operand")
	method := functionNode.ChildByFieldName("field")
	if operand == nil || method == nil || operand.Kind() != "identifier" {
		return ""
	}
	if _, ok := awsSDKConstructorNames[strings.TrimSpace(nodeText(method, source))]; !ok {
		return ""
	}
	alias := strings.TrimSpace(nodeText(operand, source))
	return serviceAliases[alias]
}

// goInferredReceiverSDKService returns the AWS SDK service bound to a receiver
// variable at a call site. It reuses goConcreteInferredReceiverType so the
// result is correlation-truthful: a receiver resolves only when a single
// service is provably bound within the narrowest in-scope assignment, and an
// ambiguous reassignment to more than one service yields "" rather than a
// guess.
func goInferredReceiverSDKService(
	receiver string,
	callLine int,
	bindings []goLocalReceiverBinding,
) string {
	return goConcreteInferredReceiverType(receiver, callLine, bindings)
}
