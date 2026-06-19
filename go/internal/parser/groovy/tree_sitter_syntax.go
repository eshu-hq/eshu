package groovy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type groovySyntaxIndex struct {
	classes   []map[string]any
	functions []map[string]any
	imports   []map[string]any
	calls     []map[string]any
	seenCalls map[string]struct{}
}

func groovySourceAndSyntax(path string, parser *tree_sitter.Parser) ([]byte, groovySyntaxIndex, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, groovySyntaxIndex{}, err
	}
	if parser == nil {
		return nil, groovySyntaxIndex{}, fmt.Errorf("parse groovy tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, groovySyntaxIndex{}, fmt.Errorf("parse groovy tree: parser returned nil tree")
	}
	defer tree.Close()

	index := groovySyntaxIndex{seenCalls: make(map[string]struct{})}
	index.collect(tree.RootNode(), source, "")
	return source, index, nil
}

func (i *groovySyntaxIndex) collect(node *tree_sitter.Node, source []byte, classContext string) {
	if node == nil {
		return
	}

	nextClassContext := classContext
	switch node.Kind() {
	case "class_declaration":
		name := groovyNodeName(node, source)
		if name != "" {
			nextClassContext = name
			i.classes = append(i.classes, map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"end_line":    shared.NodeEndLine(node),
			})
		}
	case "method_declaration":
		name := groovyNodeName(node, source)
		body := node.ChildByFieldName("body")
		if name != "" && body == nil && classContext == "" && !groovyFunctionCallIgnored(name) {
			i.appendCall(name, "", "", shared.NodeLine(node))
		}
		if name != "" && body != nil {
			item := map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"end_line":    shared.NodeEndLine(node),
			}
			if classContext != "" {
				item["class_context"] = classContext
			}
			i.functions = append(i.functions, item)
		}
	case "import_declaration":
		name := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
		if name != "" {
			item := map[string]any{
				"name":        name,
				"line_number": shared.NodeLine(node),
				"lang":        "groovy",
			}
			if alias := strings.TrimSpace(shared.NodeText(node.ChildByFieldName("alias"), source)); alias != "" {
				item["alias"] = alias
			}
			i.imports = append(i.imports, item)
		}
	case "method_invocation":
		name, fullName, receiverType := groovyInvocationParts(node, source)
		if name != "" && !groovyFunctionCallIgnored(name) {
			i.appendCall(name, fullName, receiverType, shared.NodeLine(node))
		}
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		i.collect(&child, source, nextClassContext)
	}
}

func (i *groovySyntaxIndex) appendCall(name string, fullName string, receiverType string, lineNumber int) {
	key := name + "\x00" + fullName + "\x00" + fmt.Sprint(lineNumber)
	if _, ok := i.seenCalls[key]; ok {
		return
	}
	i.seenCalls[key] = struct{}{}
	call := map[string]any{
		"name":        name,
		"line_number": lineNumber,
		"lang":        "groovy",
	}
	if fullName != "" && fullName != name {
		call["full_name"] = fullName
	}
	if receiverType != "" {
		call["inferred_obj_type"] = receiverType
	}
	i.calls = append(i.calls, call)
	slices.SortFunc(i.calls, func(left, right map[string]any) int {
		if delta := intValue(left["line_number"]) - intValue(right["line_number"]); delta != 0 {
			return delta
		}
		leftName, _ := left["name"].(string)
		rightName, _ := right["name"].(string)
		return strings.Compare(leftName, rightName)
	})
}

func groovyNodeName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(shared.NodeText(node.ChildByFieldName("name"), source))
}

func groovyInvocationParts(node *tree_sitter.Node, source []byte) (string, string, string) {
	function := node.ChildByFieldName("function")
	if function != nil {
		name := groovyLastIdentifier(function, source)
		fullName := groovyQualifiedInvocationName(shared.NodeText(function, source))
		receiverType := groovyReceiverType(fullName)
		if receiverType == "" {
			fullName = ""
		}
		return name, fullName, receiverType
	}

	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if groovyInvocationNameChildIgnored(child.Kind()) {
			continue
		}
		if name := groovyLastIdentifier(&child, source); name != "" {
			return name, "", ""
		}
	}
	return "", "", ""
}

func groovyInvocationNameChildIgnored(kind string) bool {
	switch kind {
	case "argument_list", "block", "closure":
		return true
	default:
		return false
	}
}

func groovyLastIdentifier(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	name := ""
	if kind := node.Kind(); kind == "identifier" || kind == "quoted_identifier" {
		name = strings.Trim(shared.NodeText(node, source), "'\"")
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		child := child
		if childName := groovyLastIdentifier(&child, source); childName != "" {
			name = childName
		}
	}
	return strings.TrimSpace(name)
}

func groovyQualifiedInvocationName(value string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), "")
	if !strings.Contains(compact, ".") {
		return ""
	}
	parts := strings.Split(compact, ".")
	for _, part := range parts {
		if !groovyIdentifierName(part) {
			return ""
		}
	}
	return compact
}

func groovyReceiverType(fullName string) string {
	dot := strings.LastIndex(fullName, ".")
	if dot <= 0 {
		return ""
	}
	receiver := fullName[:dot]
	receiverParts := strings.Split(receiver, ".")
	if len(receiverParts) == 0 {
		return ""
	}
	name := receiverParts[len(receiverParts)-1]
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return ""
	}
	return name
}

func groovyIdentifierName(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func groovyFunctionCallIgnored(name string) bool {
	_, ignored := groovyFunctionCallIgnoredNames[name]
	return ignored
}
