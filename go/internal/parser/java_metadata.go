package parser

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type javaMethodReferenceIndex struct {
	namesByClass map[string]map[string]struct{}
}

func buildJavaMethodReferenceIndex(
	root *tree_sitter.Node,
	source []byte,
	inference *javaCallInferenceIndex,
) *javaMethodReferenceIndex {
	index := &javaMethodReferenceIndex{namesByClass: map[string]map[string]struct{}{}}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_reference" {
			return
		}
		name, receiver := javaMethodReferenceParts(nodeText(node, source))
		if name == "" {
			return
		}
		receiver = strings.TrimSpace(receiver)
		classContext := nearestNamedAncestor(node, source, "class_declaration", "record_declaration")
		if classContext == "" {
			return
		}
		if receiver != "this" {
			receiverType := javaVisibleNameType(node, receiver, source, inference)
			if receiverType == "" {
				return
			}
			classContext = receiverType
		}
		if _, ok := index.namesByClass[classContext]; !ok {
			index.namesByClass[classContext] = map[string]struct{}{}
		}
		index.namesByClass[classContext][name] = struct{}{}
	})
	return index
}

func (i *javaMethodReferenceIndex) hasTarget(classContext string, name string) bool {
	if i == nil || classContext == "" || name == "" {
		return false
	}
	_, ok := i.namesByClass[classContext][name]
	return ok
}

func javaDecorators(node *tree_sitter.Node, source []byte, name string) []string {
	raw := nodeText(node, source)
	var decorators []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if javaSignatureLineContainsName(line, name) {
			break
		}
		if strings.HasPrefix(line, "@") {
			decorators = append(decorators, line)
		}
	}
	return decorators
}

func javaSignatureLineContainsName(line string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	return strings.Contains(line, name+"(") || strings.Contains(line, name+"<") || strings.Contains(line, name+" ")
}

func javaParameterTypes(node *tree_sitter.Node, source []byte) []string {
	parametersNode := node.ChildByFieldName("parameters")
	var parameterTypes []string
	walkDirectNamed(parametersNode, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "formal_parameter", "spread_parameter":
			typeName := javaDeclaredTypeName(child, source)
			if typeName == "" {
				return
			}
			parameterTypes = append(parameterTypes, typeName)
		}
	})
	return parameterTypes
}

func javaArgumentCount(node *tree_sitter.Node) int {
	argumentsNode := node.ChildByFieldName("arguments")
	count := 0
	walkDirectNamed(argumentsNode, func(child *tree_sitter.Node) {
		count++
	})
	return count
}

func javaCallArgumentTypes(
	node *tree_sitter.Node,
	source []byte,
	inference *javaCallInferenceIndex,
) []string {
	if node == nil || node.Kind() != "method_invocation" {
		return nil
	}
	argumentsNode := node.ChildByFieldName("arguments")
	var argumentTypes []string
	walkDirectNamed(argumentsNode, func(child *tree_sitter.Node) {
		argumentTypes = append(argumentTypes, javaExpressionTypeName(child, source, inference))
	})
	if !hasNonEmptyString(argumentTypes) {
		return nil
	}
	return argumentTypes
}

func javaExpressionTypeName(node *tree_sitter.Node, source []byte, inference *javaCallInferenceIndex) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "object_creation_expression":
		return javaObjectCreationTypeName(node, source)
	case "identifier":
		return javaVisibleNameType(node, strings.TrimSpace(nodeText(node, source)), source, inference)
	case "method_invocation":
		if !javaCallIsUnqualifiedMethodInvocation(node) || inference == nil {
			return ""
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		return inference.methodReturnType(javaEnclosingClassNode(node), name)
	case "field_access":
		raw := strings.TrimSpace(nodeText(node, source))
		if fieldName := strings.TrimPrefix(raw, "this."); fieldName != raw {
			return javaVisibleNameType(node, fieldName, source, inference)
		}
	}
	return ""
}

func javaVisibleNameType(
	node *tree_sitter.Node,
	name string,
	source []byte,
	inference *javaCallInferenceIndex,
) string {
	name = strings.TrimSpace(name)
	if node == nil || name == "" {
		return ""
	}
	callLine := nodeLine(node)
	if inference != nil {
		if typeName := inference.variableTypeBefore(javaCallInferenceScope(node), name, callLine+1); typeName != "" {
			return typeName
		}
		return inference.fieldTypeBefore(javaEnclosingClassNode(node), name, callLine+1)
	}
	if typeName := javaVariableTypeBefore(javaCallInferenceScope(node), name, source, callLine+1); typeName != "" {
		return typeName
	}
	return javaFieldTypeBefore(javaEnclosingClassNode(node), name, source, callLine+1)
}

func hasNonEmptyString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
